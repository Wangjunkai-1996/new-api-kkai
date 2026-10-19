package kkaimigrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func assertTenantOnlineMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	prepareTenantMigrationFixture(t, db)
	var baseline []AppliedMigration
	require.NoError(t, db.Order("version").Find(&baseline).Error)
	plan, err := PlanTenantOnlineExpansion(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "pristine", plan.State)
	_, err = ApplyTenantOnlineExpansion(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasTable(&tenantOnlineReceipt{}))
	assert.Error(t, CheckTenantOnlineExpansion(ctx, db))
	_, err = ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineExpansion(ctx, db))
	require.NoError(t, checkThroughVersion(ctx, db, 11, 11, 11), "the old runtime ledger remains valid")
	var preserved []AppliedMigration
	require.NoError(t, db.Order("version").Find(&preserved).Error)
	assert.Equal(t, baseline, preserved)
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	// Simulate the old application's registration SQL during the bridge period.
	require.NoError(t, db.Exec("INSERT INTO users(id,username,email) VALUES(102,'new-main','new@example.test')").Error)
	var user tenantV13User
	require.NoError(t, db.First(&user, 102).Error)
	assert.Zero(t, user.TenantID)
	assert.Nil(t, user.NormalizedUsername)
	_, err = ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	status, err := ObserveTenantOnlineExpansion(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "expanded", status.State)
	assert.Equal(t, status.TotalSteps, status.CompletedSteps)
	_, err = Apply(ctx, db, Options{})
	assert.ErrorIs(t, err, ErrUnsafeMigration, "normal startup must not finalize the contract")
	dry, err := FinalizeTenantOnlineSchema(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	assert.EqualValues(t, 14, dry.Pending[0].Version)
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
	require.NoError(t, CheckRequired(ctx, db))
	assert.Error(t, checkThroughVersion(ctx, db, 11, 11, 11), "the old binary is not a post-contract rollback")
	var versions []int64
	require.NoError(t, db.Model(&AppliedMigration{}).Order("version").Pluck("version", &versions).Error)
	assert.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 14}, versions)
	require.NoError(t, db.Where("version<=11").Order("version").Find(&preserved).Error)
	assert.Equal(t, baseline, preserved)
	canonical, email := "new-main", "new@example.test"
	for _, tenant := range []int64{1, 2} {
		require.NoError(t, db.Create(&tenantV13User{ID: 200 + tenant, TenantID: tenant, Username: "new-main", Email: email, NormalizedUsername: &canonical, NormalizedEmail: &email}).Error)
	}
	assert.Error(t, db.Create(&tenantV13User{ID: 203, TenantID: 1, Username: " NEW-MAIN ", NormalizedUsername: &canonical}).Error)
	assertTenantOnlineAggregateStorage(t, db)
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	require.NoError(t, err, "finalize is idempotent after tenant data exists")
}

func assertTenantOnlineAggregateStorage(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Multiple valid tariff denominators can exceed a single quote's 128-byte
	// amount boundary. Both aggregate fields must survive a database round trip.
	aggregate := tenantOnlineExactAggregate("1/" + strings.Repeat("9", 200) + "7")
	wallet := tenantOnlineResellerWallet{TenantID: 1, ExactProfitCarry: aggregate}
	require.NoError(t, db.Create(&wallet).Error)
	var storedWallet tenantOnlineResellerWallet
	require.NoError(t, db.First(&storedWallet, wallet.ID).Error)
	assert.Equal(t, aggregate, storedWallet.ExactProfitCarry)
	reversal := tenantOnlineCustomerPaymentReversal{TenantID: 1, IdempotencyKey: "aggregate-refund", PaymentLotID: 1, TopUpID: 1, ReversedProfitMinorExact: aggregate}
	require.NoError(t, db.Create(&reversal).Error)
	var storedReversal tenantOnlineCustomerPaymentReversal
	require.NoError(t, db.First(&storedReversal, reversal.ID).Error)
	assert.Equal(t, aggregate, storedReversal.ReversedProfitMinorExact)
}

func TestTenantOnlineSQLiteExpansionAndContract(t *testing.T) {
	assertTenantOnlineMigration(t, newMigrationTestDB(t))
}

func TestTenantOnlineRefusesUnownedPartialSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0").Error)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	assert.ErrorContains(t, err, "partial tenant schema")
	assert.False(t, db.Migrator().HasTable(&tenantOnlineReceipt{}))
}

func TestTenantOnlineResumesOnlyOwnedMatchingDDL(t *testing.T) {
	for _, correct := range []bool{true, false} {
		t.Run(map[bool]string{true: "owned-column", false: "wrong-column-shape"}[correct], func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			require.NoError(t, db.Migrator().CreateTable(&tenantOnlineReceipt{}))
			require.NoError(t, db.Create(tenantOnlineRootReceipt(db)).Error)
			step := tenantOnlineSteps()[0]
			receipt := tenantOnlineRootReceipt(db)
			receipt.Step, receipt.Checksum, receipt.State, receipt.CompletedAt = step.key, step.checksum(), "started", 0
			require.NoError(t, db.Create(receipt).Error)
			statement := "ALTER TABLE users ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0"
			if !correct {
				statement = "ALTER TABLE users ADD COLUMN tenant_id TEXT"
			}
			require.NoError(t, db.Exec(statement).Error)
			_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
			if correct {
				require.NoError(t, err)
				require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))
			} else {
				assert.ErrorIs(t, err, ErrSchemaNotReady)
				assert.False(t, db.Migrator().HasColumn("tokens", "tenant_id"), "failure must stop later steps")
			}
		})
	}
}

func TestTenantOnlineExpansionRejectsPrematureTenantDataAndReceiptTampering(t *testing.T) {
	for _, change := range []string{"UPDATE users SET tenant_id=1", "UPDATE users SET normalized_username='legacy'", "UPDATE kkai_tenant_online_steps SET checksum='tampered' WHERE step='source'"} {
		t.Run(change, func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
			require.NoError(t, err)
			require.NoError(t, db.Exec(change).Error)
			assert.Error(t, CheckTenantOnlineExpansion(context.Background(), db))
			_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{})
			assert.Error(t, err)
			keys, err := tenantGlobalUsernameUniqueKeys(db)
			require.NoError(t, err)
			assert.NotEmpty(t, keys)
			var count int64
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version=14").Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestTenantOnlineRejectsPartialSQLiteIdentityIndex(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP INDEX idx_users_tenant_normalized_username").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_users_tenant_normalized_username ON users(tenant_id,normalized_username) WHERE tenant_id=0").Error)
	assert.ErrorContains(t, CheckTenantOnlineExpansion(context.Background(), db), "partial")
}

func TestTenantOnlineRejectsCurrencyWithoutLegacyWriteDefault(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("ALTER TABLE top_ups DROP COLUMN currency").Error)
	require.NoError(t, db.Exec("ALTER TABLE top_ups ADD COLUMN currency VARCHAR(8) NOT NULL").Error)
	assert.ErrorContains(t, CheckTenantOnlineExpansion(context.Background(), db), "top_ups.currency has an invalid default")
}

func TestTenantOnlineOwnedContractResumesAfterLegacyKeyRemoval(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	receipt := tenantOnlineRootReceipt(db)
	receipt.Step, receipt.Checksum, receipt.State, receipt.CompletedAt = "contract", tenantOnlineHash("contract:"+tenantOnlineSchemaSpec()), "started", 0
	require.NoError(t, db.Create(receipt).Error)
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	require.NoError(t, tenantOnlineDropLegacyKeys(db, keys))
	assert.Error(t, CheckTenantOnlineExpansion(context.Background(), db))
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineSchema(context.Background(), db))
}

func TestTenantOnlineEmptyReceiptBootstrapResumesOnlyPristineSource(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Migrator().CreateTable(&tenantOnlineReceipt{}))
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))
}

func TestTenantOnlineRuntimeRequiresCompleteExpansion(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	ctx := context.Background()
	assert.Error(t, CheckRequired(ctx, db), "a bare schema-11 ledger does not provide tenant columns")
	_, err := ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	if RuntimeMinVersion == MembershipSchemaVersion {
		require.NoError(t, CheckRequired(ctx, db), "bridge accepts only a fully validated expansion")
	} else {
		assert.Error(t, CheckRequired(ctx, db), "feature runtime requires the completed contract")
	}
	require.NoError(t, db.Exec("DROP INDEX idx_tokens_tenant_id").Error)
	assert.Error(t, CheckRequired(ctx, db))
}

func TestTenantOnlineLedgerRejectsHistoricalBranches(t *testing.T) {
	for _, historical := range []int64{TenantSchemaVersion, TenantCompatibilitySchemaVersion} {
		t.Run(map[int64]string{12: "v12", 13: "v13"}[historical], func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
			require.NoError(t, err)
			_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{})
			require.NoError(t, err)
			entry := planItems([]migration{migrationSet()[historical-1]})[0]
			require.NoError(t, db.Create(&entry).Error)
			assert.ErrorIs(t, CheckTenantOnlineSchema(context.Background(), db), ErrMigrationHole)
		})
	}
}

func TestTenantOnlineAuthorizationFailureStopsBeforeBootstrapAndAdditiveDDL(t *testing.T) {
	for _, stopAt := range []int{1, 2, 3, 4} {
		t.Run(map[int]string{1: "bootstrap", 2: "bootstrap-after-plan", 3: "first-step-receipt", 4: "first-column"}[stopAt], func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			denied := errors.New("controller proof expired")
			calls := 0
			_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error {
				calls++
				if calls >= stopAt {
					return denied
				}
				return nil
			}})
			assert.ErrorIs(t, err, denied)
			assert.Equal(t, stopAt, calls)
			assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
			assert.Equal(t, stopAt >= 3, db.Migrator().HasTable(&tenantOnlineReceipt{}))
			require.NoError(t, checkThroughVersion(context.Background(), db, 11, 11, 11))
		})
	}
}

func TestTenantOnlineContractRevalidatesAuthorizationBeforeDestructiveDDLAndReceiptRepair(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	ctx := context.Background()
	_, err := ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	denied := errors.New("controller proof expired after the preflight scans")
	calls := 0
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error {
		calls++
		if calls >= 2 {
			return denied
		}
		return nil
	}})
	assert.ErrorIs(t, err, denied)
	assert.Equal(t, 2, calls)
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	assert.NotEmpty(t, keys, "expiry must preserve the old application's unique key")
	require.NoError(t, checkThroughVersion(ctx, db, 11, 11, 11))
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	require.NoError(t, err, "a fresh proof can resume the owned contract receipt")
	require.NoError(t, db.Model(&tenantOnlineReceipt{}).Where("step=?", "contract").Update("state", "started").Error)
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	assert.ErrorIs(t, err, denied)
	var receipt tenantOnlineReceipt
	require.NoError(t, db.Where("step=?", "contract").First(&receipt).Error)
	assert.Equal(t, "started", receipt.State, "even post-ledger receipt repair requires current authorization")
}
