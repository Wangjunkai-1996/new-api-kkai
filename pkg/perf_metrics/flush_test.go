package perfmetrics

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newMetricFlushTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	resetKKAIGroupSignalState(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	previousDB := model.DB
	model.DB = db
	clearBuckets := func() {
		hotBuckets.Range(func(key, _ any) bool {
			hotBuckets.Delete(key)
			return true
		})
	}
	clearBuckets()
	t.Cleanup(func() {
		model.DB = previousDB
		clearBuckets()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestFlushLocalBucketsPersistsCurrentBucketIncrementsOnce(t *testing.T) {
	db := newMetricFlushTestDB(t)
	sample := Sample{Model: "flush-model", Group: "default", Success: true, LatencyMs: 200, HasTtft: true, TtftMs: 20, OutputTokens: 10, GenerationMs: 100}
	Record(sample)
	require.NoError(t, FlushLocalBuckets(context.Background()))
	require.NoError(t, FlushLocalBuckets(context.Background()))

	// Another serving process contributes to the same persisted bucket.
	hotBuckets.Range(func(key, _ any) bool {
		hotBuckets.Delete(key)
		return true
	})
	Record(sample)
	require.NoError(t, FlushLocalBuckets(context.Background()))
	require.NoError(t, FlushLocalBuckets(context.Background()))

	var rows []model.PerfMetric
	require.NoError(t, db.Find(&rows).Error)
	var total model.PerfMetric
	for _, row := range rows {
		total.RequestCount += row.RequestCount
		total.SuccessCount += row.SuccessCount
		total.TotalLatencyMs += row.TotalLatencyMs
		total.TtftCount += row.TtftCount
		total.TtftSumMs += row.TtftSumMs
		total.OutputTokens += row.OutputTokens
		total.GenerationMs += row.GenerationMs
	}
	assert.EqualValues(t, 2, total.RequestCount)
	assert.EqualValues(t, 2, total.SuccessCount)
	assert.EqualValues(t, 400, total.TotalLatencyMs)
	assert.EqualValues(t, 2, total.TtftCount)
	assert.EqualValues(t, 40, total.TtftSumMs)
	assert.EqualValues(t, 20, total.OutputTokens)
	assert.EqualValues(t, 200, total.GenerationMs)
}

func TestFlushLocalBucketsRetainsFailedAndCanceledWrites(t *testing.T) {
	db := newMetricFlushTestDB(t)
	sample := Sample{Model: "retry-model", Group: "default", Success: true, LatencyMs: 100}
	Record(sample)
	injected := errors.New("injected metric write failure")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:metric-write", func(tx *gorm.DB) {
		tx.AddError(injected)
	}))
	require.ErrorIs(t, FlushLocalBuckets(context.Background()), injected)
	require.NoError(t, db.Callback().Create().Remove("test:metric-write"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, FlushLocalBuckets(ctx), context.Canceled)
	Record(sample)
	require.NoError(t, FlushLocalBuckets(context.Background()))
	require.NoError(t, FlushLocalBuckets(context.Background()))
	var total int64
	require.NoError(t, db.Model(&model.PerfMetric{}).Select("SUM(request_count)").Scan(&total).Error)
	assert.EqualValues(t, 2, total)
}

func TestMetricMaintenanceDoesNotFlushServingSamples(t *testing.T) {
	db := newMetricFlushTestDB(t)
	Record(Sample{Model: "local-model", Group: "default", Success: true})
	require.NoError(t, RunMaintenance(context.Background()))
	var count int64
	require.NoError(t, db.Model(&model.PerfMetric{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, FlushLocalBuckets(context.Background()))
	require.NoError(t, db.Model(&model.PerfMetric{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestFlushLocalBucketsHonorsDatabaseContext(t *testing.T) {
	db := newMetricFlushTestDB(t)
	Record(Sample{Model: "context-model", Group: "default", Success: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:cancel-metric-write", func(tx *gorm.DB) {
		assert.Same(t, ctx, tx.Statement.Context)
		cancel()
	}))
	require.ErrorIs(t, FlushLocalBuckets(ctx), context.Canceled)
	require.NoError(t, db.Callback().Create().Remove("test:cancel-metric-write"))
	require.NoError(t, FlushLocalBuckets(context.Background()))
	var rows []model.PerfMetric
	require.NoError(t, db.Where("model_name = ?", "context-model").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 1, rows[0].RequestCount)
}
