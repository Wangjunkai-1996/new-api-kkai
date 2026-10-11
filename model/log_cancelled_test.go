package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordCancelledLogUsesCancelledType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(common.RequestIdKey, "cancelled-log-test")
	RecordCancelledLog(ctx, 1, 67, "gpt-test", "token-test", "request cancelled", 2, 1, false, "default", NewLogOther())

	var stored Log
	require.NoError(t, db.Where("request_id = ?", "cancelled-log-test").First(&stored).Error)
	assert.Equal(t, LogTypeCancelled, stored.Type)
	assert.Equal(t, "request cancelled", stored.Content)
}
