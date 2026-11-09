package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipBillingRecoveryExpandPreservesPriorState(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id INTEGER NULL", "ALTER TABLE top_ups ADD COLUMN create_time INTEGER NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	reservation := model.MembershipBillingRecord{RequestID: "v20-reservation", Scope: model.MembershipBillingScopeRelay, UserID: 101, Source: model.MembershipWalletSource, Snapshot: "frozen", ReservedQuota: 43, State: "reserved", CreatedAt: 100}
	require.NoError(t, db.Create(&reservation).Error)

	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	denied := errors.New("controller proof expired")
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(ctx context.Context, locked *gorm.DB) error {
		_, nestedErr := ApplyMembershipBillingRecoveryExpand(ctx, locked, Options{Offline: true})
		assert.ErrorIs(t, nestedErr, ErrUnsafeMigration, "authorization runs while competing migrations are excluded")
		return denied
	}})
	require.ErrorIs(t, err, denied)
	checks := 0
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error {
		checks++
		if checks == 2 {
			return denied
		}
		return nil
	}})
	require.ErrorIs(t, err, denied, "authorization is checked again immediately before DDL")
	assert.Equal(t, 2, checks)
	plan, err := ApplyMembershipBillingRecoveryExpand(ctx, db, Options{DryRun: true, AuthorizeMutation: func(context.Context, *gorm.DB) error {
		t.Error("dry-run must not request mutation authorization")
		return denied
	}})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.Equal(t, MembershipBillingRecoverySchemaVersion, plan.Pending[0].Version)
	present, err := MembershipBillingRecoverySchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.False(t, present)
	assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)

	// A valid DDL prefix is resumable, but cannot enable recovery without a receipt.
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, migrationSet()[MembershipBillingRecoverySchemaVersion-1]))
	present, err = MembershipBillingRecoverySchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.True(t, present)
	assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)
	result, err := ApplyMembershipBillingRecoveryExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.Equal(t, MembershipBillingRecoveryPlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckMembershipBillingRecoverySchema(ctx, db))
	require.NoError(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db))
	for _, check := range []func(context.Context, *gorm.DB) error{
		CheckMembershipPaymentSchema, CheckMembershipPaymentRuntimeSchema,
		CheckShopSchema, CheckShopRuntimeSchema, CheckWalletPaymentSchema, CheckWalletPaymentRuntimeSchema,
		CheckInvoiceSchema, CheckRequestTicketSchema, CheckResellerApplicationSchema, CheckTenantOnlineSchema,
	} {
		require.NoError(t, check(ctx, db))
	}
	if RuntimeMaxVersion >= MembershipBillingRecoverySchemaVersion {
		require.NoError(t, CheckRequired(ctx, db))
	} else {
		assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady, "bridge remains incompatible with v21")
	}
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	require.ErrorIs(t, err, denied, "a completed receipt never bypasses current controller authorization")
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)], "existing canonical migration receipts remain immutable")
	var stored model.MembershipBillingRecord
	require.NoError(t, db.First(&stored, "request_id = ?", reservation.RequestID).Error)
	assert.Equal(t, reservation, stored, "expansion must not release or settle business data")
	for _, table := range []string{"kkai_membership_billing_recovery", "kkai_membership_billing_scan_progress"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		assert.Zero(t, count, "migration does not infer evidence or enqueue historical money")
	}
}

func TestMembershipBillingRecoveryRuntimeSchemaAndReceiptOwnership(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				// Existing fixtures opt in to local-only disposable databases.
				db = newShopSchemaDatabaseFixture(t, dialect)
			}
			ctx := context.Background()
			require.NoError(t, db.Migrator().CreateTable(&AppliedMigration{}))
			for _, item := range planItemsForDialect(dialect, MembershipBillingRecoverySchemaVersion) {
				require.NoError(t, db.Create(&AppliedMigration{Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item)}).Error)
			}
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipBillingRecoverySchemaVersion-1]))
			require.NoError(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), "runtime gate does not scan unrelated tenant or business data")
			for _, id := range []string{"unknown-a", "unknown-b"} {
				require.NoError(t, db.Create(&membershipBillingRecoveryV21{RequestID: id, State: "pending", EvidenceKind: "unknown"}).Error, "NULL receipt keys must allow independent unknown requests")
			}
			receipt := "provider:stable-receipt"
			evidence := membershipBillingRecoveryV21{RequestID: "known", State: "ready", EvidenceKind: "final_usage", ReceiptKey: &receipt}
			require.NoError(t, db.Create(&evidence).Error)
			assert.Error(t, db.Create(&membershipBillingRecoveryV21{RequestID: "another", ReceiptKey: &receipt}).Error, "one final receipt cannot fund two requests")
			assert.Error(t, db.Create(&membershipBillingRecoveryV21{RequestID: "known"}).Error, "one request has one recovery record")
			require.NoError(t, db.Create(&membershipBillingScanProgressV21{Lane: "aged"}).Error)
			assert.Error(t, db.Create(&membershipBillingScanProgressV21{Lane: "aged"}).Error, "one lane has one persistent cursor")
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipBillingRecoverySchemaVersion-1]))
			var saved membershipBillingRecoveryV21
			require.NoError(t, db.First(&saved, "request_id = ?", evidence.RequestID).Error)
			assert.Equal(t, evidence, saved)

			require.NoError(t, db.Where("version = ?", MembershipBillingRecoverySchemaVersion).Delete(&AppliedMigration{}).Error)
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)
			item := migrationSet()[MembershipBillingRecoverySchemaVersion-1]
			require.NoError(t, db.Create(&AppliedMigration{Version: item.Version, Name: item.Name, Checksum: "invalid"}).Error)
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrChecksumMismatch)
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", item.Version).Update("checksum", storedMigrationChecksum(item)).Error)
			require.NoError(t, db.Migrator().DropIndex("kkai_membership_billing_recovery", "idx_membership_recovery_due"))
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, db.Exec("CREATE INDEX idx_membership_recovery_due ON kkai_membership_billing_recovery (state, next_attempt_at, request_id)").Error)
			require.NoError(t, db.Migrator().DropIndex("kkai_membership_billing_recovery", "ux_membership_recovery_receipt"))
			require.NoError(t, db.Exec("CREATE INDEX ux_membership_recovery_receipt ON kkai_membership_billing_recovery (receipt_key)").Error)
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady, "same-name nonunique index must fail closed")
			require.NoError(t, db.Migrator().DropIndex("kkai_membership_billing_recovery", "ux_membership_recovery_receipt"))
			require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_membership_recovery_receipt ON kkai_membership_billing_recovery (receipt_key)").Error)
			require.NoError(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db))
			require.NoError(t, db.Exec("ALTER TABLE kkai_membership_billing_scan_progress DROP COLUMN sweep_upper_request_id").Error)
			assert.ErrorIs(t, CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), ErrSchemaNotReady)
		})
	}
}

func TestMembershipBillingRecoveryExpandCatalogIsAdditiveAcrossDialects(t *testing.T) {
	require.NoError(t, validateMigrationCatalog(migrationSet()))
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			tables := map[string]struct{}{}
			for _, statement := range membershipBillingRecoverySchemaStatements[dialect] {
				require.NoError(t, validateExpandSQL(dialect, statement, tables))
				if statement.Operation == migrationOperationCreateTable {
					tokens, err := expandSQLTokens(dialect, statement.SQL)
					require.NoError(t, err)
					tables[createTableName(tokens)] = struct{}{}
				}
			}
			assert.Len(t, tables, 2)
		})
	}
}
