package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumeLogPersistsDashboardCacheTokensWithoutDoubleCounting(t *testing.T) {
	if DB.Dialector.Name() != "postgres" {
		t.Skip("rc49 dashboard regression uses PostgreSQL")
	}
	truncateTables(t)
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = true
	CacheQuotaDataLock.Lock()
	previousCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = previousExport
		CacheQuotaDataLock.Lock()
		CacheQuotaData = previousCache
		CacheQuotaDataLock.Unlock()
	})
	user := createReserveTestUser(t, 100)
	setTestCreditEpoch(t, map[string]int64{"quota_data": 0, "logs": 0})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	ctx.Set("username", user.Username)
	for index := 0; index < 2; index++ {
		other := NewLogOther()
		other.SetPublic("cache_tokens", 11)
		other.SetPublic("cache_creation_tokens", 20)
		other.SetPublic("cache_creation_tokens_5m", 7)
		other.SetPublic("cache_creation_tokens_1h", 13)
		RecordConsumeLog(ctx, user.Id, RecordConsumeLogParams{ModelName: "cache-dashboard", Quota: 1, Other: other})
		require.NoError(t, SaveQuotaDataCache())
	}
	rows, err := GetQuotaDataByUserId(user.Id, 0, common.GetTimestamp()+3600)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 22, rows[0].CacheReadTokens)
	assert.EqualValues(t, 40, rows[0].CacheCreationTokens, "5m and 1h counters already belong to the total")
	assert.Equal(t, 2, rows[0].Count)
}
