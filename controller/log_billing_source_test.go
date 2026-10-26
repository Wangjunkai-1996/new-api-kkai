package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogBillingSourceListAndStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
	})
	for i, entry := range []model.Log{
		{UserId: 7, Type: model.LogTypeConsume, Other: `{"billing_source":"membership"}`, Quota: 10, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeConsume, Other: `{"billing_source":"wallet","membership_account_id":1}`, Quota: 20, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeConsume, Other: "", Quota: 30, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeConsume, Other: `{"billing_source":"subscription"}`, Quota: 40, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeConsume, Other: `{"billing_source":"membership"}`, Quota: 50, ModelName: "wanted"},
		{UserId: 8, Type: model.LogTypeConsume, Other: `{"billing_source":"membership"}`, Quota: 60, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeRefund, Other: `{"billing_source":"membership"}`, Quota: -10, ModelName: "wanted"},
		{UserId: 7, Type: model.LogTypeConsume, Other: `{"billing_source":"membership"}`, Quota: 70, ModelName: "different"},
	} {
		entry.CreatedAt = common.GetTimestamp()
		entry.RequestId = fmt.Sprint(i + 1)
		require.NoError(t, db.Create(&entry).Error)
	}
	for _, test := range []struct {
		name    string
		list    gin.HandlerFunc
		stats   gin.HandlerFunc
		source  string
		page    int
		total   int
		request string
		quota   int
	}{
		{"self member page 2", GetUserLogs, GetLogsSelfStat, "membership", 2, 2, "1", 60},
		{"admin member page 2", GetAllLogs, GetLogsStat, "membership", 2, 3, "5", 120},
		{"self wallet", GetUserLogs, GetLogsSelfStat, "wallet", 1, 2, "3", 50},
		{"admin wallet", GetAllLogs, GetLogsStat, "wallet", 1, 2, "3", 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, stats := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/log?billing_source=%s&model_name=wanted&page_size=1&p=%d", test.source, test.page), nil)
				ctx.Set("id", 7)
				ctx.Set("role", common.RoleAdminUser)
				if stats {
					test.stats(ctx)
				} else {
					test.list(ctx)
				}
				var response struct {
					Success bool `json:"success"`
					Data    struct {
						BillingSource string      `json:"billing_source"`
						Total         int         `json:"total"`
						Items         []model.Log `json:"items"`
						Quota         int         `json:"quota"`
						RPM           int         `json:"rpm"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.True(t, response.Success, recorder.Body.String())
				assert.Equal(t, test.source, response.Data.BillingSource)
				if stats {
					assert.Equal(t, test.quota, response.Data.Quota)
					assert.Equal(t, test.total, response.Data.RPM)
				} else {
					assert.Equal(t, test.total, response.Data.Total)
					require.Len(t, response.Data.Items, 1)
					assert.Equal(t, test.request, response.Data.Items[0].RequestId)
				}
			}
		})
	}
	for _, handler := range []gin.HandlerFunc{GetUserLogs, GetAllLogs, GetLogsSelfStat, GetLogsStat} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log?billing_source=invalid", nil)
		ctx.Set("id", 7)
		handler(ctx)
		assert.JSONEq(t, `{"success":false,"message":"invalid billing_source"}`, recorder.Body.String())
	}
}
