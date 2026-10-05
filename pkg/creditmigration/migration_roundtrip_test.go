package creditmigration

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanJSONKeepsInt64AndApplyReplayIsSafe(t *testing.T) {
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`INSERT INTO users(id, quota, used_quota, aff_quota, aff_history, setting)
		VALUES (1, 9223372036854775807, 0, 0, 0, '')`).Error)
	plan, err := BuildPlan(context.Background(), db, "large-credit", DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	encoded, err := Marshal(plan)
	require.NoError(t, err)
	var decoded Plan
	require.NoError(t, Unmarshal(encoded, &decoded))
	require.Equal(t, plan.PlanHash, decoded.PlanHash)
	require.Equal(t, plan.Entries, decoded.Entries)
	contract := MaintenanceContract{MigrationID: decoded.MigrationID, Offline: true, AllowApply: true}
	require.NoError(t, Apply(context.Background(), db, decoded, contract))
	require.NoError(t, Apply(context.Background(), db, decoded, contract))
	var quota int64
	require.NoError(t, db.Table("users").Select("quota").Where("id = 1").Scan(&quota).Error)
	want, _ := Convert(math.MaxInt64)
	require.Equal(t, want, quota)
}

func TestSettingThresholdMirrorsRuntimeIntegerConversion(t *testing.T) {
	converted, changed, err := convertSetting(`{"quota_warning_threshold":400.5}`)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, `{"quota_warning_threshold":30}`, converted)
}
