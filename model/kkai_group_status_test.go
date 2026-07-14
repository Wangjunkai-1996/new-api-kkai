package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetKKAIPerfMetricBucketsAggregatesVisibleGroups(t *testing.T) {
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.SetLogDatabaseType(originalLogDatabaseType)
		initCol()
	})

	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	initCol()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&PerfMetric{}))
	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "gpt-a", Group: "default", BucketTs: 100, RequestCount: 3, SuccessCount: 2, TotalLatencyMs: 900, TtftSumMs: 300, TtftCount: 2},
		{ModelName: "gpt-b", Group: "default", BucketTs: 100, RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 500, TtftSumMs: 200, TtftCount: 2},
		{ModelName: "gpt-a", Group: "vip", BucketTs: 100, RequestCount: 7, SuccessCount: 7},
		{ModelName: "gpt-a", Group: "default", BucketTs: 200, RequestCount: 5, SuccessCount: 4},
	}).Error)

	buckets, err := GetKKAIPerfMetricBuckets(50, 150, []string{"default"})
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, "default", buckets[0].Group)
	assert.Equal(t, int64(100), buckets[0].BucketTs)
	assert.Equal(t, int64(5), buckets[0].RequestCount)
	assert.Equal(t, int64(4), buckets[0].SuccessCount)
	assert.Equal(t, int64(1400), buckets[0].TotalLatencyMs)
	assert.Equal(t, int64(500), buckets[0].TtftSumMs)
	assert.Equal(t, int64(4), buckets[0].TtftCount)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Close())
}
