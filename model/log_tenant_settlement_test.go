package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantLogSettlementFollowsLedgerAcrossSeparateDatabases(t *testing.T) {
	mainDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, mainDB.AutoMigrate(&TenantConsumption{}))
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		for _, db := range []*gorm.DB{mainDB, logDB} {
			sqlDB, closeErr := db.DB()
			require.NoError(t, closeErr)
			require.NoError(t, sqlDB.Close())
		}
	})

	stored := Log{TenantID: 1, UserId: 7, Type: LogTypeConsume, TokenId: 9, RequestId: "pending-request", Quota: 900, ChannelId: 12, Other: `{"expr_b64":"display","admin_info":{"actual_expr_b64":"actual"}}`}
	require.NoError(t, logDB.Create(&stored).Error)
	require.NoError(t, mainDB.Create(&TenantConsumption{TenantID: 1, CustomerID: 7, RequestID: stored.RequestId, Status: "settlement_pending", ReservedQuota: 100, ActualQuota: 900, LastError: "internal funds diagnosis"}).Error)

	logs, total, err := GetUserLogs(7, LogTypeConsume, 0, 0, "", "", 0, 20, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Equal(t, &TenantLogSettlement{Status: "settlement_pending", ReservedQuota: 100, ActualQuota: 900}, logs[0].TenantSettlement)
	assert.Equal(t, 900, logs[0].Quota)
	assert.Zero(t, logs[0].ChannelId)
	assert.NotContains(t, logs[0].Other, "internal funds diagnosis")
	assert.JSONEq(t, `{"expr_b64":"actual"}`, logs[0].Other)

	require.NoError(t, mainDB.Model(&TenantConsumption{}).Where("request_id = ?", stored.RequestId).Update("status", "settled").Error)
	logs, err = GetLogByTokenId(9)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "settled", logs[0].TenantSettlement.Status)
	assert.Equal(t, 900, logs[0].Quota)
	var unchanged Log
	require.NoError(t, logDB.First(&unchanged, stored.Id).Error)
	assert.Equal(t, stored.Quota, unchanged.Quota)
	assert.Equal(t, stored.Other, unchanged.Other)
	assert.Nil(t, unchanged.TenantSettlement)

	for _, test := range []struct {
		name string
		log  Log
	}{
		{"wrong_tenant", Log{TenantID: 2, UserId: 7, Type: LogTypeConsume, RequestId: stored.RequestId}},
		{"wrong_customer", Log{TenantID: 1, UserId: 8, Type: LogTypeConsume, RequestId: stored.RequestId}},
		{"platform", Log{TenantID: PlatformTenantID, UserId: 7, Type: LogTypeConsume, RequestId: stored.RequestId}},
		{"not_consume", Log{TenantID: 1, UserId: 7, Type: LogTypeError, RequestId: stored.RequestId}},
		{"historical_no_match", Log{TenantID: 1, UserId: 7, Type: LogTypeConsume, RequestId: "legacy"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, projectTenantLogSettlements([]*Log{&test.log}))
			assert.Nil(t, test.log.TenantSettlement)
		})
	}
}
