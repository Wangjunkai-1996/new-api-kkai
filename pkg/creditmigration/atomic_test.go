package creditmigration

import (
	"context"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func atomicMigrationFixture(t *testing.T) (*gorm.DB, Plan, PricingPlan, MaintenanceContract) {
	t.Helper()
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE options (key TEXT PRIMARY KEY, value TEXT NOT NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,quota,used_quota,aff_quota,aff_history,setting) VALUES (1,665000000,0,133000000,0,'')`).Error)
	inventory := pricingInventoryFixture()
	target, err := BuildTargetPricingConfig(inventory.Source)
	require.NoError(t, err)
	before, after := pricingOptionValues(inventory.Source), pricingOptionValues(target)
	before["ModelRatio"], after["ModelRatio"] = `{"claude-sonnet":10.5}`, `{"claude-sonnet":1.5}`
	before["billing_setting.billing_expr"], after["billing_setting.billing_expr"] = `{"gpt-task":"p*21+c*105"}`, `{"gpt-task":"p * 3 + c * 15"}`
	keys := make([]string, 0, len(before))
	for key := range before {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		old := before[key]
		inventory.Options = append(inventory.Options, OptionChange{Key: key, Before: &old, After: after[key]})
		require.NoError(t, db.Exec("INSERT INTO options (key,value) VALUES (?,?)", key, old).Error)
	}
	pricing, err := BuildPricingPlan(inventory)
	require.NoError(t, err)
	require.NoError(t, ValidatePricingPlan(pricing))
	plan, err := BuildPlan(context.Background(), db, inventory.MigrationID, DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	contract := MaintenanceContract{MigrationID: plan.MigrationID, Offline: true, AllowApply: true, MaintenanceLocked: true, WritesStopped: true, BackupReadable: true, LogDatabaseMode: "main"}
	return db, plan, pricing, contract
}

func TestAtomicCurrencyCutoverPreservesHistoryAndRestoresOptions(t *testing.T) {
	db, _, pricing, contract := atomicMigrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE checkins (id INTEGER PRIMARY KEY, quota_awarded BIGINT); INSERT INTO checkins VALUES (7,1000)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE redemptions (id INTEGER PRIMARY KEY, quota BIGINT, status INTEGER); INSERT INTO redemptions VALUES (1,1000,1),(2,1000,2),(3,1000,3)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE top_ups (id INTEGER PRIMARY KEY, status TEXT, amount BIGINT); INSERT INTO top_ups VALUES (9,'success',1330)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, quota BIGINT); INSERT INTO logs VALUES (11,1400)`).Error)
	plan, err := BuildPlan(context.Background(), db, contract.MigrationID, DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	assert.ErrorIs(t, Apply(context.Background(), db, plan, contract), ErrUnsupportedData)
	require.NoError(t, Apply(context.Background(), db, plan, contract, pricing))
	require.NoError(t, Apply(context.Background(), db, plan, contract, pricing))
	require.NoError(t, Verify(context.Background(), db, plan))
	var user struct {
		Quota    int64
		AffQuota int64
	}
	require.NoError(t, db.Table("users").Where("id = 1").Take(&user).Error)
	assert.Equal(t, int64(49875000), user.Quota) // 1330 old credits -> $99.75.
	assert.Equal(t, int64(9975000), user.AffQuota)
	var redemptions []struct {
		ID    int64
		Quota int64
	}
	require.NoError(t, db.Table("redemptions").Order("id").Find(&redemptions).Error)
	assert.Equal(t, int64(75), redemptions[0].Quota)
	assert.Equal(t, int64(75), redemptions[1].Quota)
	assert.Equal(t, int64(1000), redemptions[2].Quota)
	var checkin, log int64
	require.NoError(t, db.Table("checkins").Select("quota_awarded").Scan(&checkin).Error)
	require.NoError(t, db.Table("logs").Select("quota").Scan(&log).Error)
	assert.Equal(t, int64(1000), checkin)
	assert.Equal(t, int64(1400), log)
	var epochOption optionValue
	require.NoError(t, db.Table("options").Where("key = ?", common.CreditEpochOption).Take(&epochOption).Error)
	epoch, err := common.ParseCreditEpochCutover(epochOption.Value)
	require.NoError(t, err)
	assert.Equal(t, plan.PlanHash, epoch.PlanHash)
	assert.Equal(t, pricing.PlanHash, epoch.PricingPlanHash)
	assert.Equal(t, "hold", epoch.LegacyTopUpPolicy)
	assert.Equal(t, []int64{3}, epoch.LegacyUsedRedemptionIDs)
	assert.Equal(t, int64(9), epoch.LegacyMaxIDs["top_ups"])
	assert.Equal(t, int64(11), epoch.LegacyMaxIDs["logs"])
	require.NoError(t, Restore(context.Background(), db, plan.MigrationID, DefaultReceiptTable, contract))
	require.NoError(t, checkOptions(db, pricing.Options, false))
	require.NoError(t, db.Table("users").Where("id = 1").Take(&user).Error)
	assert.Equal(t, int64(665000000), user.Quota)
	var count int64
	require.NoError(t, db.Table("options").Where("key = ?", common.CreditEpochOption).Count(&count).Error)
	assert.Zero(t, count)
}

func TestReceiptFailureRollsBackWalletOptionsAndEpochTogether(t *testing.T) {
	db, plan, pricing, contract := atomicMigrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_receipt BEFORE INSERT ON kkai_credit_migration_receipts BEGIN SELECT RAISE(ABORT,'receipt unavailable'); END`).Error)
	require.ErrorContains(t, Apply(context.Background(), db, plan, contract, pricing), "receipt unavailable")
	require.NoError(t, checkEntries(db, plan.Entries, false))
	require.NoError(t, checkOptions(db, pricing.Options, false))
	var count int64
	require.NoError(t, db.Table("options").Where("key = ?", common.CreditEpochOption).Count(&count).Error)
	assert.Zero(t, count)
}

func TestAtomicMigrationRejectsConfigDriftAndUnreviewedWalletEdits(t *testing.T) {
	db, plan, pricing, contract := atomicMigrationFixture(t)
	require.NoError(t, db.Exec("UPDATE options SET value='0.08' WHERE key='Price'").Error)
	assert.ErrorIs(t, Apply(context.Background(), db, plan, contract, pricing), ErrPlanDrift)
	require.NoError(t, checkEntries(db, plan.Entries, false))
	require.NoError(t, db.Exec("UPDATE options SET value='0.075' WHERE key='Price'").Error)
	plan.Entries[0].After["quota"] = "1000000000"
	plan.PlanHash = hashPlan(plan)
	assert.ErrorIs(t, Apply(context.Background(), db, plan, contract, pricing), ErrPlanDrift)
}

func TestTerminalHistoryDoesNotHideUnfinishedSettlement(t *testing.T) {
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE top_ups(id INTEGER PRIMARY KEY,status TEXT); INSERT INTO top_ups VALUES(1,'success'),(2,'pending')`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE tasks(id INTEGER PRIMARY KEY,status TEXT,private_data TEXT); INSERT INTO tasks VALUES(1,'SUCCESS','{"billing_state":"accepted"}')`).Error)
	plan, err := BuildPlan(context.Background(), db, "drain-test", DefaultReceiptTable)
	require.NoError(t, err)
	assert.Contains(t, plan.Blockers, "tasks has unfinished or unproven billing/accounting settlement")
	require.NoError(t, db.Exec(`UPDATE tasks SET private_data='{"billing_state":"completed","accounting_required":true,"accounting_state":"completed"}'`).Error)
	plan, err = BuildPlan(context.Background(), db, "drain-test", DefaultReceiptTable)
	require.NoError(t, err)
	assert.Empty(t, plan.Blockers)
}

func TestTaskHistoryPreservesSettledUnknownAndLegacyTerminalOnly(t *testing.T) {
	db := migrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE tasks(id INTEGER PRIMARY KEY,status TEXT,private_data TEXT)`).Error)
	for _, test := range []struct {
		status, private string
		blocked         bool
	}{
		{"SUCCESS", `{}`, false},
		{"FAILURE", `{}`, false},
		{"UNKNOWN", `{"billing_state":"completed","accounting_required":true,"accounting_state":"completed"}`, false},
		{"UNKNOWN", `{}`, true},
		{"SUCCESS", `{"billing_state":"accepted"}`, true},
		{"SUCCESS", `{"billing_state":"completed","accounting_required":true}`, true},
		{"IN_PROGRESS", `{"billing_state":"completed"}`, true},
	} {
		require.NoError(t, db.Exec("DELETE FROM tasks").Error)
		require.NoError(t, db.Exec("INSERT INTO tasks VALUES(1,?,?)", test.status, test.private).Error)
		blocker, err := historyBlocker(db, "tasks")
		require.NoError(t, err)
		assert.Equal(t, test.blocked, blocker != "", "%s %s", test.status, test.private)
	}
}

func TestDeadOutboxIsPreservedWhilePendingStillBlocks(t *testing.T) {
	db, _, pricing, contract := atomicMigrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE kkai_outbox(id INTEGER PRIMARY KEY,status TEXT,payload TEXT); INSERT INTO kkai_outbox VALUES(1,'dead','{"raw":1330}')`).Error)
	plan, err := BuildPlan(t.Context(), db, contract.MigrationID, DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	require.NoError(t, Apply(t.Context(), db, plan, contract, pricing))
	var row struct{ Status, Payload string }
	require.NoError(t, db.Table("kkai_outbox").Take(&row).Error)
	assert.Equal(t, "dead", row.Status)
	assert.Equal(t, `{"raw":1330}`, row.Payload)
	require.NoError(t, db.Exec("UPDATE kkai_outbox SET status='pending'").Error)
	blocker, err := historyBlocker(db, "kkai_outbox")
	require.NoError(t, err)
	assert.NotEmpty(t, blocker)
}

func TestPendingTopUpsRemainRawAndAreHeldIncludingMalformedOrders(t *testing.T) {
	db, _, pricing, contract := atomicMigrationFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE top_ups(id INTEGER PRIMARY KEY,status TEXT,payment_provider TEXT,amount BIGINT,money REAL); INSERT INTO top_ups VALUES(1,'pending','epay',1000,75),(2,'pending','stripe',10,10),(3,'pending','creem',500000,1),(4,'pending','waffo',10,10),(5,'pending','waffo_pancake',10,10)`).Error)
	plan, err := BuildPlan(context.Background(), db, contract.MigrationID, DefaultReceiptTable)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	require.NoError(t, Apply(context.Background(), db, plan, contract, pricing))
	var order struct {
		Amount int64
		Money  float64
		Status string
	}
	require.NoError(t, db.Table("top_ups").Where("id = 1").Take(&order).Error)
	assert.Equal(t, int64(1000), order.Amount)
	assert.Equal(t, 75.0, order.Money)
	assert.Equal(t, "pending", order.Status)
	require.NoError(t, Restore(context.Background(), db, plan.MigrationID, DefaultReceiptTable, contract))
	for _, sql := range []string{
		`UPDATE top_ups SET payment_provider='mystery' WHERE id=1`,
		`UPDATE top_ups SET payment_provider='epay',money=-1 WHERE id=1`,
		`UPDATE top_ups SET payment_provider='creem',money=1,amount=1 WHERE id=1`,
	} {
		require.NoError(t, db.Exec(sql).Error)
		inventory, err := InventoryDB(context.Background(), db, "pending-hold", DefaultReceiptTable)
		require.NoError(t, err)
		assert.Empty(t, inventory.Unsupported)
	}
}
