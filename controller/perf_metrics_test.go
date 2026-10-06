package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
