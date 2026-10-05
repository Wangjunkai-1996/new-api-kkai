package kkaimigrate

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRC41ArchitectureMigrationIsAdditiveAcrossDialects(t *testing.T) {
	item := rc41ArchitectureMigration()
	require.Equal(t, int64(9), item.Version)
	require.NoError(t, validateMigrationCatalog(migrationSet()))

	for _, dialect := range requiredMigrationDialects {
		t.Run(dialect, func(t *testing.T) {
			statements := item.Statements[dialect]
			require.NotEmpty(t, statements)
			allSQL := make([]string, 0, len(statements))
			for _, statement := range statements {
				require.NotContains(t, strings.ToUpper(statement.SQL), "DROP ")
				require.NotContains(t, strings.ToUpper(statement.SQL), "TRUNCATE ")
				allSQL = append(allSQL, statement.SQL)
			}
			joined := strings.Join(allSQL, "\n")
			for _, table := range []string{"user_access_tokens", "task_plugins", "login_encryption_keys", "audit_logs", "kkai_credit_migration_receipts"} {
				require.Contains(t, joined, "CREATE TABLE IF NOT EXISTS "+table)
			}
			require.Contains(t, joined, "CREATE UNIQUE INDEX idx_user_access_tokens_token_hash")
			require.Contains(t, joined, "CREATE UNIQUE INDEX uk_task_plugin_key_version")
			require.Contains(t, joined, "CREATE UNIQUE INDEX idx_login_encryption_keys_slot")
			if dialect == DialectMySQL {
				require.Contains(t, joined, "source LONGTEXT NOT NULL")
				require.Contains(t, joined, "icon LONGTEXT NOT NULL")
				for _, column := range []string{"before_image", "after_image", "options_before", "options_after"} {
					require.Contains(t, joined, column+" LONGTEXT NOT NULL")
				}
			} else {
				require.Contains(t, joined, "source TEXT NOT NULL")
				require.Contains(t, joined, "icon TEXT NOT NULL")
			}
		})
	}
}

func TestRC41ArchitectureSQLiteDDLExecutes(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	for _, statement := range rc41ArchitectureSchemaStatements[DialectSQLite] {
		require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement), statement.SQL)
	}
	for _, table := range []string{"user_access_tokens", "task_plugins", "login_encryption_keys", "audit_logs", "kkai_credit_migration_receipts"} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	for table, indexes := range map[string][]string{
		"user_access_tokens":    {"idx_user_access_tokens_user_id", "idx_user_access_tokens_token_hash", "idx_user_access_tokens_expires_at"},
		"task_plugins":          {"uk_task_plugin_key_version", "idx_task_plugins_active"},
		"login_encryption_keys": {"idx_login_encryption_keys_slot"},
	} {
		for _, index := range indexes {
			require.True(t, db.Migrator().HasIndex(table, index), index)
		}
	}
}

func TestRC41ExpandPreservesDataAndProvisionsAuthenticationOnce(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO users (id, telegram_id) VALUES (1, '123')").Error)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.False(t, db.Migrator().HasTable("login_encryption_keys"))
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	var key model.LoginEncryptionKey
	require.NoError(t, db.First(&key).Error)
	require.NotEmpty(t, key.PrivateKeyPEM)
	var deadline model.Option
	require.NoError(t, db.Where(map[string]any{"key": "LegacyAccessTokenRetireAt"}).Take(&deadline).Error)
	require.NotEmpty(t, deadline.Value)
	require.NoError(t, db.Transaction(backfillRC41Authentication))
	var sameKey model.LoginEncryptionKey
	require.NoError(t, db.First(&sameKey).Error)
	require.Equal(t, key, sameKey)
	var sameDeadline model.Option
	require.NoError(t, db.Where(map[string]any{"key": "LegacyAccessTokenRetireAt"}).Take(&sameDeadline).Error)
	require.Equal(t, deadline, sameDeadline)
	var telegram string
	require.NoError(t, db.Table("users").Select("telegram_id").Where("id = ?", 1).Scan(&telegram).Error)
	require.Equal(t, "123", telegram)
	var count int64
	require.NoError(t, db.Table("external_identity_claims").Count(&count).Error)
	require.EqualValues(t, 1, count)
	token := model.UserAccessToken{UserId: 1, TokenHash: "hash", CreatedAt: time.Now().Unix()}
	require.NoError(t, db.Create(&token).Error)
	event := model.AuditLog{EventId: "event-1", UserId: 1, Other: model.AuditOther{LoginMethod: "password"}}
	require.NoError(t, db.Create(&event).Error)
	var restored model.AuditLog
	require.NoError(t, db.First(&restored).Error)
	require.Equal(t, "password", restored.Other.LoginMethod)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, CheckRequired(context.Background(), db))
}

func TestRC41ExpandPreservesLegacyMidjourneyBilling(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO midjourneys (id, mj_id, channel_id, quota, status)
VALUES (1, 'legacy-task', 43, 120, 'SUCCESS')`).Error)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{})
	require.NoError(t, err)

	var legacy model.Midjourney
	require.NoError(t, db.First(&legacy, 1).Error)
	assert.Equal(t, "legacy-task", legacy.MjId)
	assert.Equal(t, "SUCCESS", legacy.Status)
	assert.Equal(t, 120, legacy.Quota)
	assert.Zero(t, legacy.TokenId)
	assert.Zero(t, legacy.BillingChannelId)
	assert.Equal(t, 43, legacy.GetBillingChannelId())
	var untouched int64
	require.NoError(t, db.Table("midjourneys").Where("id = 1 AND token_id IS NULL AND billing_channel_id IS NULL").Count(&untouched).Error)
	assert.EqualValues(t, 1, untouched)

	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	legacy.TokenId = 9
	legacy.BillingChannelId = 51
	legacy.Quota = 150
	require.NoError(t, legacy.UpdateBillingState())
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	var restored model.Midjourney
	require.NoError(t, db.First(&restored, 1).Error)
	assert.Equal(t, legacy, restored)
	assert.Equal(t, 51, restored.GetBillingChannelId())
	require.NoError(t, CheckRequired(context.Background(), db))
}

func TestRC41AuthenticationWriteFailureDoesNotLogPrivateKey(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, rc41ArchitectureMigration()))
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_login_key BEFORE INSERT ON login_encryption_keys
BEGIN SELECT RAISE(ABORT, 'rejected login key'); END`).Error)
	var output bytes.Buffer
	db = db.Session(&gorm.Session{Logger: logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Info})})
	err = db.Transaction(backfillRC41Authentication)
	require.ErrorContains(t, err, "rejected login key")
	assert.NotContains(t, output.String(), "PRIVATE KEY")
	assert.NotContains(t, output.String(), "INSERT INTO `login_encryption_keys`")
}

func TestRC41CreditReceiptPreservesLargeImagesAndIdentity(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, rc41ArchitectureMigration()))
	snapshot := strings.Repeat("balance-image", 10000)
	row := map[string]any{"migration_id": "usd-credit-v1", "plan_hash": "plan", "pricing_plan_hash": "pricing", "state": "applied", "before_image": snapshot, "after_image": snapshot, "options_before": snapshot, "options_after": snapshot, "created_at": int64(1), "updated_at": int64(1)}
	require.NoError(t, db.Table("kkai_credit_migration_receipts").Create(row).Error)
	require.Error(t, db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Table("kkai_credit_migration_receipts").Create(row).Error)
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, rc41ArchitectureMigration()))
	var restored struct{ BeforeImage, AfterImage, OptionsBefore, OptionsAfter, PricingPlanHash string }
	require.NoError(t, db.Table("kkai_credit_migration_receipts").Where("migration_id = ?", "usd-credit-v1").Take(&restored).Error)
	assert.Equal(t, snapshot, restored.BeforeImage)
	assert.Equal(t, snapshot, restored.AfterImage)
	assert.Equal(t, snapshot, restored.OptionsBefore)
	assert.Equal(t, snapshot, restored.OptionsAfter)
	assert.Equal(t, "pricing", restored.PricingPlanHash)
}

func TestRC41LedgerEpochExpansionPreservesAuditValues(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO kkai_internal_balance_adjustments
 (id,operation_id,user_id,delta,reason,metadata,payload_sha256,balance_before,balance_after,created_at)
 VALUES (1,'legacy-rebate',2,1330,'rebate','{}','old-hash',100,1430,1)`).Error)
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	var row struct {
		Delta, BalanceBefore, BalanceAfter int64
		PayloadSHA256                      string
		WalletDelta                        *int64
		SourceEpoch                        *string
	}
	require.NoError(t, db.Table("kkai_internal_balance_adjustments").Where("id = ?", 1).Take(&row).Error)
	assert.EqualValues(t, 1330, row.Delta)
	assert.EqualValues(t, 100, row.BalanceBefore)
	assert.EqualValues(t, 1430, row.BalanceAfter)
	assert.Equal(t, "old-hash", row.PayloadSHA256)
	assert.Nil(t, row.WalletDelta)
	assert.Nil(t, row.SourceEpoch)
}
