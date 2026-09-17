package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributorAffinityPreservesAutoGroupCandidateIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousRedis := common.RedisEnabled
	previousProfiles := setting.AutoGroupProfiles2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	affinity := operation_setting.GetChannelAffinitySetting()
	previousAffinity := *affinity
	previousAffinity.Rules = append([]operation_setting.ChannelAffinityRule(nil), affinity.Rules...)
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.RedisEnabled = previousRedis
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		*affinity = previousAffinity
	})

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:auto2-affinity-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip","default"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto2":"Auto 2"}`))

	modelName := "gpt-auto2-affinity"
	priority := int64(10)
	weight := uint(100)
	channels := make([]model.Channel, 0, 2)
	for _, group := range []string{"vip", "default"} {
		channel := model.Channel{
			Type: constant.ChannelTypeOpenAI, Key: "test-key-" + group, Status: common.ChannelStatusEnabled,
			Name: group, Models: modelName, Group: group, Priority: &priority, Weight: &weight,
		}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
			Priority: &priority, Weight: weight,
		}).Error)
		channels = append(channels, channel)
	}
	require.NoError(t, model.SyncChannelCacheOnce())
	t.Cleanup(func() {
		// Rebuild an empty cache before the fixture database is closed.
		require.NoError(t, db.Where("1 = 1").Delete(&model.Ability{}).Error)
		require.NoError(t, db.Where("1 = 1").Delete(&model.Channel{}).Error)
		require.NoError(t, model.SyncChannelCacheOnce())
	})

	affinityValue := fmt.Sprintf("auto2-affinity-%d", time.Now().UnixNano())
	affinity.Enabled = true
	affinity.Rules = []operation_setting.ChannelAffinityRule{{
		Name: "auto2-index-test", ModelRegex: []string{"^" + modelName + "$"},
		PathRegex:         []string{"^/v1/chat/completions$"},
		KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Auto2-Affinity"}},
		IncludeUsingGroup: true, IncludeModelName: true, IncludeRuleName: true, TTLSeconds: 60,
	}}
	seedContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	seedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	seedContext.Request.Header.Set("X-Auto2-Affinity", affinityValue)
	_, found := service.GetPreferredChannelByAffinity(seedContext, modelName, "auto2")
	require.False(t, found)
	service.RecordChannelAffinity(seedContext, channels[1].Id)
	t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(seedContext) })

	var selectedID, selectedIndex, retryID int
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto2")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	}, Distribute(), func(c *gin.Context) {
		selectedID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		selectedIndex = common.GetContextKeyInt(c, constant.ContextKeyAutoGroupIndex)
		retried, selectedGroup, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
			Ctx: c, TokenGroup: "auto2", ModelName: modelName, RequestPath: c.Request.URL.Path,
			Retry: common.GetPointer(1),
		})
		require.NoError(t, err)
		require.NotNil(t, retried)
		retryID = retried.Id
		assert.Equal(t, "default", selectedGroup)
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+modelName+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Auto2-Affinity", affinityValue)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, channels[1].Id, selectedID)
	assert.Equal(t, 1, selectedIndex)
	assert.Equal(t, channels[1].Id, retryID, "retry without cross-group permission must stay in the affinity candidate")
}
