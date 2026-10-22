package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPerfMetricsExcludesInactiveGroupsAndIncludesNamedAutoProfiles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	previousDB := model.DB
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousProfiles := setting.AutoGroupProfiles2JsonString()
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousProfiles))
		require.NoError(t, sqlDB.Close())
	})
	model.DB = db
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["default"]}`))
	hour := time.Now().Truncate(time.Hour).Add(-time.Hour).Unix()
	for _, row := range []model.PerfMetric{
		{ModelName: "visibility-model", Group: "default", BucketTs: hour, RequestCount: 100, SuccessCount: 100},
		{ModelName: "visibility-model", Group: "auto2", BucketTs: hour, RequestCount: 1},
		{ModelName: "visibility-model", Group: "inactive", BucketTs: hour, RequestCount: 100},
	} {
		require.NoError(t, model.UpsertPerfMetric(context.Background(), &row))
	}

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?model=visibility-model", nil)
	GetPerfMetrics(c)
	require.Equal(t, http.StatusOK, writer.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Summary     *perfmetrics.Summary      `json:"summary"`
			Groups      []perfmetrics.GroupResult `json:"groups"`
			WindowStart int64                     `json:"window_start"`
			WindowEnd   int64                     `json:"window_end"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(writer.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.NotNil(t, payload.Data.Summary)
	assert.Equal(t, 99.01, payload.Data.Summary.SuccessRate)
	assert.Less(t, payload.Data.WindowStart, payload.Data.WindowEnd)
	require.Len(t, payload.Data.Groups, 2)
	assert.Equal(t, "auto2", payload.Data.Groups[0].Group)
	assert.NotContains(t, writer.Body.String(), "inactive")
	assert.NotContains(t, writer.Body.String(), "request_count")
}

func TestTenantPerfMetricsFollowSaleableModelGroups(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	db := setupModelListControllerTestDB(t)
	previousModels := ratio_setting.ModelRatio2JSONString()
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousUsable := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		common.SetDatabaseTypes(previousMainType, previousLogType)
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModels))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.TenantPriceVersion{}))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"perf-tenant-a":1,"perf-tenant-b":1,"perf-private":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":2,"private":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "public-supply", Group: "default,premium,private", Models: "perf-tenant-a,perf-tenant-b,perf-private"}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1, Group: "default", Model: "perf-tenant-a", Enabled: true},
		{ChannelId: 1, Group: "premium", Model: "perf-tenant-b", Enabled: true},
		{ChannelId: 1, Group: "private", Model: "perf-private", Enabled: true},
	}).Error)
	model.InvalidatePricingCache()
	hour := time.Now().Truncate(time.Hour).Add(-time.Hour).Unix()
	for _, row := range []model.PerfMetric{
		{ModelName: "perf-tenant-a", Group: "default", BucketTs: hour, RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 2000, OutputTokens: 200, GenerationMs: 2000},
		{ModelName: "perf-tenant-b", Group: "premium", BucketTs: hour, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 2000, OutputTokens: 50, GenerationMs: 1000},
		{ModelName: "perf-tenant-a", Group: "premium", BucketTs: hour, RequestCount: 100, TotalLatencyMs: 900000},
		{ModelName: "perf-tenant-b", Group: "default", BucketTs: hour, RequestCount: 100, TotalLatencyMs: 900000},
		{ModelName: "perf-private", Group: "default", BucketTs: hour, RequestCount: 100},
		{ModelName: "perf-private", Group: "private", BucketTs: hour, RequestCount: 100},
	} {
		require.NoError(t, model.UpsertPerfMetric(context.Background(), &row))
	}

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/summary", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	c.Set("group", "default") // Dashboard authentication supplies the account group.
	GetPerfMetricsSummary(c)
	var summary struct {
		Success bool                         `json:"success"`
		Data    perfmetrics.SummaryAllResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(writer.Body.Bytes(), &summary))
	require.True(t, summary.Success, writer.Body.String())
	require.NotNil(t, summary.Data.Summary)
	assert.Equal(t, perfmetrics.Summary{SuccessRate: 100, AvgLatencyMs: 1333, AvgTps: 83.33}, *summary.Data.Summary)
	require.Len(t, summary.Data.Models, 2)
	assert.NotContains(t, writer.Body.String(), "perf-private")
	assert.Equal(t, "no-store", writer.Header().Get("Cache-Control"))

	for _, test := range []struct {
		query     string
		wantGroup string
	}{
		{"model=perf-tenant-a", "default"},
		{"model=perf-tenant-b", "premium"},
		{"model=perf-tenant-a&group=premium", ""},
		{"model=perf-private", ""},
	} {
		writer = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?"+test.query, nil)
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		c.Set("group", "default")
		GetPerfMetrics(c)
		var detail struct {
			Success bool                    `json:"success"`
			Data    perfmetrics.QueryResult `json:"data"`
		}
		require.NoError(t, common.Unmarshal(writer.Body.Bytes(), &detail))
		require.True(t, detail.Success, writer.Body.String())
		if test.wantGroup != "" {
			require.NotNil(t, detail.Data.Summary)
			assert.Equal(t, 100.0, detail.Data.Summary.SuccessRate)
			require.Len(t, detail.Data.Groups, 1)
			assert.Equal(t, test.wantGroup, detail.Data.Groups[0].Group)
		} else {
			assert.Nil(t, detail.Data.Summary)
			assert.Empty(t, detail.Data.Groups)
		}
	}
}
