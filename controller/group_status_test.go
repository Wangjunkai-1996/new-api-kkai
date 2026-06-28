package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type groupStatusAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Window        string                     `json:"window"`
		WindowMinutes int                        `json:"window_minutes"`
		WindowHours   int                        `json:"window_hours"`
		Groups        []service.GroupStatusEntry `json:"groups"`
	} `json:"data"`
}

func setupGroupStatusControllerTest(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	prepareGroupStatusControllerModelColumns(t)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Ability{}, &model.PerfMetric{}, &model.Log{}))
	require.NoError(t, db.AutoMigrate(&model.Channel{}))

	resetGroupStatusControllerSettings(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func prepareGroupStatusControllerModelColumns(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}

	common.IsMasterNode = originalIsMasterNode
	common.SQLitePath = originalSQLitePath
	common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	if hadSQLDSN {
		require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
	} else {
		require.NoError(t, os.Unsetenv("SQL_DSN"))
	}
}

func resetGroupStatusControllerSettings(t *testing.T) {
	t.Helper()

	userUsableGroups := setting.UserUsableGroups2JSONString()
	groupRatios := ratio_setting.GroupRatio2JSONString()
	groupSpecialUsableGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(userUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(groupSpecialUsableGroups)))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))
}

func TestGroupStatusControllerReturnsOnlyUserUsableGroupsAndNoSensitiveFields(t *testing.T) {
	db := setupGroupStatusControllerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "group-status-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-4o", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "gpt-4o", ChannelId: 2, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "default-channel", Key: "sk-default", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "vip-channel", Key: "sk-vip", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{
			ModelName:      "gpt-4o",
			Group:          "default",
			BucketTs:       time.Now().Unix(),
			RequestCount:   30,
			SuccessCount:   30,
			TotalLatencyMs: 30000,
			TtftSumMs:      9000,
			TtftCount:      30,
		},
		{
			ModelName:      "gpt-4o",
			Group:          "vip",
			BucketTs:       time.Now().Unix(),
			RequestCount:   30,
			SuccessCount:   30,
			TotalLatencyMs: 30000,
			TtftSumMs:      9000,
			TtftCount:      30,
		},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status/groups?hours=6", nil)
	ctx.Set("id", 1001)

	GetGroupStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response groupStatusAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, 6, response.Data.WindowHours)
	require.Equal(t, []string{"default", "vip"}, controllerGroupStatusNames(response.Data.Groups))
	require.Equal(t, service.GroupConfidenceExcellent, response.Data.Groups[0].ConfidenceStatus)
	require.NotEmpty(t, response.Data.Groups[0].ExperienceLabel)
	require.NotEmpty(t, response.Data.Groups[0].RecommendationLevel)
	require.NotEmpty(t, response.Data.Groups[0].DisplayMessage)

	body := recorder.Body.String()
	require.NotContains(t, body, "channel_id")
	require.NotContains(t, body, "channel_name")
	require.NotContains(t, body, "base_url")
	require.NotContains(t, body, "key")
}

func TestGroupStatusControllerAcceptsRealtimeWindow(t *testing.T) {
	db := setupGroupStatusControllerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1003,
		Username: "group-status-live-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Name:   "default-channel",
		Key:    "sk-default",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: 1,
		Enabled:   true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status/groups?window=now", nil)
	ctx.Set("id", 1003)

	GetGroupStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response groupStatusAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "now", response.Data.Window)
	require.Equal(t, 5, response.Data.WindowMinutes)
	require.Equal(t, 1, response.Data.WindowHours)
	require.Equal(t, []string{"default", "vip"}, controllerGroupStatusNames(response.Data.Groups))
	require.Equal(t, service.GroupConfidenceUnknown, response.Data.Groups[0].ConfidenceStatus)
}

func TestGroupStatusControllerHonorsSpecialUserUsableGroups(t *testing.T) {
	db := setupGroupStatusControllerTest(t)
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{
		"default": {
			"-:vip": "hide vip"
		}
	}`)))
	require.NoError(t, db.Create(&model.User{
		Id:       1002,
		Username: "limited-group-status-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-4o", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "gpt-4o", ChannelId: 2, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "default-channel", Key: "sk-default", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "vip-channel", Key: "sk-vip", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{
			ModelName:      "gpt-4o",
			Group:          "default",
			BucketTs:       time.Now().Unix(),
			RequestCount:   30,
			SuccessCount:   30,
			TotalLatencyMs: 30000,
			TtftSumMs:      9000,
			TtftCount:      30,
		},
		{
			ModelName:      "gpt-4o",
			Group:          "vip",
			BucketTs:       time.Now().Unix(),
			RequestCount:   30,
			SuccessCount:   30,
			TotalLatencyMs: 30000,
			TtftSumMs:      9000,
			TtftCount:      30,
		},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status/groups", nil)
	ctx.Set("id", 1002)

	GetGroupStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response groupStatusAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, []string{"default"}, controllerGroupStatusNames(response.Data.Groups))
}

func controllerGroupStatusNames(entries []service.GroupStatusEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Group)
	}
	return names
}
