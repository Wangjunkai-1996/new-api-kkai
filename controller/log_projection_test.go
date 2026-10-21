package controller

import (
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

func TestGetAllLogsProjectsDiagnosticsForOperatorRole(t *testing.T) {
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
	require.NoError(t, db.Create(&model.Log{Type: model.LogTypeConsume, Other: `{"admin_info":{"stream_status":{"status":"error","end_reason":"scanner_error","error_count":1,"end_error":"private diagnosis"}},"root_info":{"secret":"root-only"}}`}).Error)
	for _, test := range []struct {
		name string
		role int
		root bool
	}{
		{"admin", common.RoleAdminUser, false},
		{"root", common.RoleRootUser, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/?p=1&page_size=10", nil)
			ctx.Set("role", test.role)
			GetAllLogs(ctx)
			assert.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Items []model.Log `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success, recorder.Body.String())
			require.Len(t, response.Data.Items, 1)
			other, err := common.StrToMap(response.Data.Items[0].Other)
			require.NoError(t, err)
			assert.Contains(t, other, "admin_info")
			assert.Equal(t, map[string]any{"status": "error", "end_reason": "scanner_error", "error_count": float64(1)}, other["stream_status"])
			if test.root {
				assert.Contains(t, other, "root_info")
			} else {
				assert.NotContains(t, other, "root_info")
			}
		})
	}
}
