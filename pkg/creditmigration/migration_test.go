package creditmigration

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func migrationFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY, quota BIGINT NOT NULL, used_quota BIGINT NOT NULL,
		aff_quota BIGINT NOT NULL, aff_history BIGINT NOT NULL, setting TEXT NOT NULL
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE subscription_plans (
		id INTEGER PRIMARY KEY, total_amount BIGINT NOT NULL
	)`).Error)
	require.NoError(t, db.Exec(ReceiptTableSchema(DefaultReceiptTable, "sqlite")).Error)
	return db
}

func TestConvertHandlesSignedBoundaries(t *testing.T) {
	for _, tc := range []struct {
		old  int64
		want int64
	}{
		{old: 0, want: 0},
		{old: 6, want: 0},
		{old: 7, want: 1},
		{old: -7, want: -1},
	} {
		got, _ := Convert(tc.old)
		assert.Equal(t, tc.want, got)
	}
	positive, _ := Convert(int64(1<<63 - 1))
	negative, _ := Convert(-1 << 63)
	assert.Greater(t, positive, int64(0))
	assert.Less(t, negative, int64(0))
}

func TestInventoryDoesNotAssumeEveryTableHasQuota(t *testing.T) {
	db := migrationFixture(t)
	_, err := InventoryDB(context.Background(), db, "usd-credit-test", DefaultReceiptTable)
	require.NoError(t, err)
}

func TestBuildPlanBlocksFiniteQuotaThatWouldBecomeZero(t *testing.T) {
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE redemptions (id INTEGER PRIMARY KEY, quota BIGINT NOT NULL, status INTEGER NOT NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO redemptions(id, quota, status) VALUES (1, 6, 1)`).Error)
	plan, err := BuildPlan(context.Background(), db, "usd-credit-test", DefaultReceiptTable)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(plan.Blockers, "\n"), "redemptions.1.quota would round to finite zero")
}

func TestApplyVerifyAndRestoreUsesReceiptAndRejectsDrift(t *testing.T) {
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`INSERT INTO users(id, quota, used_quota, aff_quota, aff_history, setting)
		VALUES (1, 1000, 200, 80, 120, '{"quota_warning_threshold":400,"theme":"dark"}')`).Error)
	plan, err := BuildPlan(context.Background(), db, "usd-credit-test", DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	contract := MaintenanceContract{MigrationID: plan.MigrationID, Offline: true, AllowApply: true}
	require.NoError(t, Apply(context.Background(), db, plan, contract))
	require.NoError(t, Verify(context.Background(), db, plan))
	require.NoError(t, Apply(context.Background(), db, plan, contract))
	require.NoError(t, db.Table("users").Where("id = 1").Update("quota", 76).Error)
	assert.ErrorIs(t, Apply(context.Background(), db, plan, contract), ErrPlanDrift)
	require.NoError(t, db.Table("users").Where("id = 1").Update("quota", 75).Error)
	var quota int64
	var setting string
	require.NoError(t, db.Table("users").Select("quota").Where("id = 1").Scan(&quota).Error)
	require.NoError(t, db.Table("users").Select("setting").Where("id = 1").Scan(&setting).Error)
	assert.Equal(t, int64(75), quota)
	assert.Contains(t, setting, `"quota_warning_threshold":30`)
	assert.Contains(t, setting, `"theme":"dark"`)
	require.NoError(t, Restore(context.Background(), db, plan.MigrationID, DefaultReceiptTable, contract))
	require.NoError(t, db.Table("users").Select("quota").Where("id = 1").Scan(&quota).Error)
	assert.Equal(t, int64(1000), quota)
	assert.ErrorIs(t, Verify(context.Background(), db, plan), ErrPlanDrift)
}
