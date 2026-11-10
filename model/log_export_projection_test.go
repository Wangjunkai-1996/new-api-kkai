package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogExportPreservesCreditEpochTenantProjectionAndBillingSource(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&TenantConsumption{}))
	const requestID = "rc49-export-tenant"
	t.Cleanup(func() { DB.Where("request_id = ?", requestID).Delete(&TenantConsumption{}) })
	require.NoError(t, DB.Create(&TenantConsumption{TenantID: 42, CustomerID: 7, RequestID: requestID, Status: "settlement_pending", ReservedQuota: 70, ActualQuota: 90}).Error)
	logs := []Log{
		{Id: 100, UserId: 7, TenantID: 42, Type: LogTypeConsume, CreatedAt: 1, RequestId: requestID, Quota: 400, ChannelId: 9, Other: `{"billing_source":"wallet","expr_b64":"display","admin_info":{"actual_expr_b64":"actual","secret":"private"}}`},
		{Id: 101, UserId: 7, Type: LogTypeConsume, CreatedAt: 1, RequestId: "rc49-export-new", Quota: 400, Other: `{"billing_source":"wallet"}`},
		{Id: 102, UserId: 7, Type: LogTypeConsume, CreatedAt: 2, RequestId: "rc49-export-member", Quota: 999, Other: `{"billing_source":"membership"}`},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	setTestCreditEpoch(t, map[string]int64{"logs": 100})
	var exported []*Log
	require.NoError(t, StreamUserLogs(context.Background(), 7, LogFilter{BillingSource: "wallet"}, func(log *Log) error { exported = append(exported, log); return nil }))
	require.Len(t, exported, 2)
	assert.Equal(t, 1, exported[0].Id)
	assert.Equal(t, 400, exported[0].Quota, "new row must not be classified using its rewritten display ID")
	assert.Nil(t, exported[0].OriginalQuota)
	assert.Equal(t, 2, exported[1].Id)
	assert.Equal(t, 30, exported[1].Quota)
	require.NotNil(t, exported[1].OriginalQuota)
	assert.Equal(t, 400, *exported[1].OriginalQuota)
	assert.Equal(t, common.CreditEpochLegacy, exported[1].PricingEpoch)
	assert.Zero(t, exported[1].ChannelId)
	assert.NotContains(t, exported[1].Other, "private")
	assert.Contains(t, exported[1].Other, "actual")
	assert.Equal(t, &TenantLogSettlement{Status: "settlement_pending", ReservedQuota: 70, ActualQuota: 90}, exported[1].TenantSettlement)
}
