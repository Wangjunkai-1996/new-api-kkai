package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributorRejectsSpecifiedChannelWithoutMultiReferenceCapability(t *testing.T) {
	channels := setupImageStudioDistributorTest(t)
	selected := 0
	response := runImageStudioDistributorTestRequest(t, func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(channels.replicate.Id))
	}, func(c *gin.Context) {
		selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusNoContent)
	})

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Zero(t, selected)
	assert.Contains(t, response.Body.String(), service.ErrNoChannelSupportsImageReferences.Error())
}

func TestDistributorRejectsSpecifiedChannelWithoutRequestedOutputCapability(t *testing.T) {
	channels := setupImageStudioDistributorTest(t)
	selected := 0
	response := runImageStudioDistributorTestRequestWithCapabilities(t, 0, 2, func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(channels.azure.Id))
	}, func(c *gin.Context) {
		selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusNoContent)
	})

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Zero(t, selected)
	assert.Contains(t, response.Body.String(), service.ErrNoChannelSupportsImageOutputCount.Error())
	assert.Contains(t, response.Body.String(), string(types.ErrorCodeImageOutputCountUnavailable))
}

func TestDistributorSkipsIncompatibleAffinityWithoutClearingIt(t *testing.T) {
	channels := setupImageStudioDistributorTest(t)
	affinityValue := fmt.Sprintf("image-affinity-%d", time.Now().UnixNano())
	configureImageStudioAffinityTest(t, affinityValue, channels.replicate.Id)

	selected := 0
	response := runImageStudioDistributorTestRequest(t, func(c *gin.Context) {
		c.Request.Header.Set("X-Image-Affinity", affinityValue)
	}, func(c *gin.Context) {
		selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusInternalServerError)
	})

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, channels.openAI.Id, selected)

	verifyContext := newImageStudioAffinityContext(affinityValue)
	preferred, found := service.GetPreferredChannelByAffinity(
		verifyContext, "gpt-image-2", service.ImageStudioTokenGroup,
	)
	assert.True(t, found)
	assert.Equal(t, channels.replicate.Id, preferred)
	service.ClearCurrentChannelAffinityCache(verifyContext)
}

func TestDistributorSkipsOutputIncompatibleAffinityWithoutClearingIt(t *testing.T) {
	channels := setupImageStudioDistributorTest(t)
	affinityValue := fmt.Sprintf("image-output-affinity-%d", time.Now().UnixNano())
	configureImageStudioAffinityTest(t, affinityValue, channels.azure.Id)

	selected := 0
	response := runImageStudioDistributorTestRequestWithCapabilities(t, 0, 2, func(c *gin.Context) {
		c.Request.Header.Set("X-Image-Affinity", affinityValue)
	}, func(c *gin.Context) {
		selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusInternalServerError)
	})

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, channels.replicate.Id, selected)

	verifyContext := newImageStudioAffinityContext(affinityValue)
	preferred, found := service.GetPreferredChannelByAffinity(
		verifyContext, "gpt-image-2", service.ImageStudioTokenGroup,
	)
	assert.True(t, found)
	assert.Equal(t, channels.azure.Id, preferred)
	service.ClearCurrentChannelAffinityCache(verifyContext)
}

type imageStudioDistributorChannels struct {
	replicate model.Channel
	openAI    model.Channel
	azure     model.Channel
}

func setupImageStudioDistributorTest(t *testing.T) imageStudioDistributorChannels {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousRedisEnabled := common.RedisEnabled
	dsn := fmt.Sprintf("file:image-distributor-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.RedisEnabled = previousRedisEnabled
	})

	azure := seedImageStudioDistributorChannel(t, db, constant.ChannelTypeAzure, 30)
	replicate := seedImageStudioDistributorChannel(t, db, constant.ChannelTypeReplicate, 20)
	openAI := seedImageStudioDistributorChannel(t, db, constant.ChannelTypeOpenAI, 10)
	require.NoError(t, model.SyncChannelCacheOnce())
	return imageStudioDistributorChannels{replicate: replicate, openAI: openAI, azure: azure}
}

func seedImageStudioDistributorChannel(t *testing.T, db *gorm.DB, channelType int, priority int64) model.Channel {
	t.Helper()
	weight := uint(100)
	channel := model.Channel{
		Type: channelType, Key: fmt.Sprintf("key-%d", channelType), Status: common.ChannelStatusEnabled,
		Name: fmt.Sprintf("channel-%d", channelType), Models: "gpt-image-2", Group: service.ImageStudioTokenGroup,
		Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.ImageStudioTokenGroup, Model: "gpt-image-2", ChannelId: channel.Id,
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	return channel
}

func runImageStudioDistributorTestRequest(
	t *testing.T,
	setup func(*gin.Context),
	final func(*gin.Context),
) *httptest.ResponseRecorder {
	return runImageStudioDistributorTestRequestWithCapabilities(t, 2, 1, setup, final)
}

func runImageStudioDistributorTestRequestWithCapabilities(
	t *testing.T,
	referenceCount int,
	outputCount int,
	setup func(*gin.Context),
	final func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.POST("/v1/images/edits", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, service.ImageStudioTokenGroup)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		service.SetImageStudioReferenceCount(c, referenceCount)
		service.SetImageStudioRequestedOutputCount(c, outputCount)
		setup(c)
	}, Distribute(), final)
	body := []byte(`{"model":"gpt-image-2"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func configureImageStudioAffinityTest(t *testing.T, affinityValue string, channelID int) {
	t.Helper()
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)
	original := *setting
	original.Rules = append([]operation_setting.ChannelAffinityRule(nil), setting.Rules...)
	setting.Enabled = true
	setting.KeepOnChannelDisabled = false
	setting.Rules = []operation_setting.ChannelAffinityRule{{
		Name: "image-studio-test", ModelRegex: []string{"^gpt-image-2$"},
		PathRegex: []string{"^/v1/images/edits$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{{
			Type: "request_header", Key: "X-Image-Affinity",
		}},
		TTLSeconds: 60, IncludeUsingGroup: true, IncludeModelName: true, IncludeRuleName: true,
	}}
	t.Cleanup(func() { *setting = original })

	ctx := newImageStudioAffinityContext(affinityValue)
	_, found := service.GetPreferredChannelByAffinity(ctx, "gpt-image-2", service.ImageStudioTokenGroup)
	require.False(t, found)
	service.RecordChannelAffinity(ctx, channelID)
}

func newImageStudioAffinityContext(affinityValue string) *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	ctx.Request.Header.Set("X-Image-Affinity", affinityValue)
	return ctx
}
