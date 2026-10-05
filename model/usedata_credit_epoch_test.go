package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaDataCreditEpochKeepsSameHourBucketsSeparate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	old := QuotaData{Id: 1, UserID: 1, Username: "alice", ModelName: "model", CreatedAt: 3600, UseGroup: "default", Count: 1, Quota: 1000, TokenUsed: 40}
	require.NoError(t, db.Create(&old).Error)
	setTestCreditEpoch(t, map[string]int64{"quota_data": 1})
	fresh := old
	fresh.Id, fresh.Quota, fresh.TokenUsed = 0, 10, 5
	require.NoError(t, persistQuotaData(&fresh))
	fresh.Id, fresh.Quota, fresh.TokenUsed = 0, 15, 7
	require.NoError(t, persistQuotaData(&fresh))
	var rows []QuotaData
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, old, rows[0])
	assert.Equal(t, 25, rows[1].Quota)
	assert.Equal(t, 2, rows[1].Count)
	for _, read := range []func() ([]*QuotaData, error){
		func() ([]*QuotaData, error) { return GetQuotaDataByUsername("alice", 3600, 7200) },
		func() ([]*QuotaData, error) { return GetQuotaDataByUserId(1, 3600, 7200) },
		func() ([]*QuotaData, error) { return GetQuotaDataGroupByUser(3600, 7200) },
		func() ([]*QuotaData, error) { return GetAllQuotaDates(3600, 7200, "") },
	} {
		got, err := read()
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, 100, got[0].Quota)
		assert.Equal(t, 3, got[0].Count)
		assert.Equal(t, 52, got[0].TokenUsed)
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		got, err := GetFlowQuotaData(3600, 7200, "", 1, role)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, 100, got[0].Quota)
		assert.Equal(t, 3, got[0].Count)
		assert.Equal(t, 52, got[0].TokenUsed)
	}
}

func TestCreditEpochHistoricalSQLRoundsEachRowWithoutInt64Overflow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE logs(id INTEGER PRIMARY KEY,quota BIGINT NOT NULL)`).Error)
	values := []int64{0, 6, 7, 20, 39, 40, -6, -7, -20, -39, -40, math.MaxInt64, math.MinInt64}
	for i, value := range values {
		require.NoError(t, db.Exec("INSERT INTO logs VALUES(?,?)", i+1, value).Error)
	}
	setTestCreditEpoch(t, map[string]int64{"logs": int64(len(values))})
	expression, err := creditEpochQuotaExpression(db, "logs")
	require.NoError(t, err)
	var rows []struct {
		ID    int
		Quota int64
	}
	require.NoError(t, db.Table("logs").Select("id,"+expression+" AS quota").Order("id").Scan(&rows).Error)
	require.Len(t, rows, len(values))
	for i, row := range rows {
		assert.Equal(t, common.LegacyCreditToUSD(values[i]), row.Quota, "raw quota %d", values[i])
	}
	var sum int64
	require.NoError(t, db.Table("logs").Select("SUM("+expression+")").Where("id IN ?", []int{3, 4, 7}).Scan(&sum).Error)
	assert.Equal(t, int64(3), sum) // round(7*.075)+round(20*.075)+round(-6*.075).
}

func TestQuotaDataRejectsMissingCurrencyBoundary(t *testing.T) {
	setTestCreditEpoch(t, map[string]int64{"logs": 1})
	_, err := GetQuotaDataByUsername("alice", 0, 7200)
	assert.ErrorIs(t, err, common.ErrCreditEpochInvalid)
	assert.ErrorIs(t, persistQuotaData(&QuotaData{UserID: 1, Quota: 7}), common.ErrCreditEpochInvalid)
}
