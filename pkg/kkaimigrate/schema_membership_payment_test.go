package kkaimigrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipPaymentExpandPreservesPriorContractsAndPayments(t *testing.T) {
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
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	order := model.MembershipOrder{ID: 941, UserID: 101, TradeNo: "legacy-paid-order", Status: model.MembershipOrderSuccess, AccountID: 73, PaidAt: 100, Terms: "frozen-legacy-terms"}
	receipt := model.MembershipPaymentReceipt{OrderID: order.ID, ReceiptKey: "epay:legacy-paid-receipt", ReceiptID: "legacy-paid-receipt", AmountMinor: 88800, Currency: "CNY", Status: model.MembershipOrderSuccess, CreatedAt: 100}
	require.NoError(t, db.Create(&order).Error)
	require.NoError(t, db.Create(&receipt).Error)

	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	denied := errors.New("controller authorization denied")
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	require.ErrorIs(t, err, denied)
	plan, err := ApplyMembershipPaymentExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.Equal(t, MembershipPaymentSchemaVersion, plan.Pending[0].Version)
	present, err := MembershipPaymentSchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.False(t, present)
	require.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady)

	// A resumable DDL prefix cannot authorize use before its canonical receipt.
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, migrationSet()[MembershipPaymentSchemaVersion-1]))
	require.ErrorIs(t, CheckMembershipPaymentSchema(ctx, db), ErrSchemaNotReady)
	require.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady)
	result, err := ApplyMembershipPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.Equal(t, MembershipPaymentPlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckMembershipPaymentSchema(ctx, db))
	require.NoError(t, CheckMembershipPaymentRuntimeSchema(ctx, db))
	for _, check := range []func(context.Context, *gorm.DB) error{
		CheckShopSchema, CheckShopRuntimeSchema, CheckWalletPaymentSchema, CheckWalletPaymentRuntimeSchema,
		CheckInvoiceSchema, CheckRequestTicketSchema, CheckResellerApplicationSchema, CheckTenantOnlineSchema,
	} {
		require.NoError(t, check(ctx, db))
	}
	if RuntimeMaxVersion >= MembershipPaymentSchemaVersion {
		require.NoError(t, CheckRequired(ctx, db))
	}
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	var savedOrder model.MembershipOrder
	var savedReceipt model.MembershipPaymentReceipt
	require.NoError(t, db.First(&savedOrder, order.ID).Error)
	require.NoError(t, db.First(&savedReceipt, receipt.ID).Error)
	assert.Equal(t, order, savedOrder)
	assert.Equal(t, receipt, savedReceipt)
	for _, value := range model.MembershipPaymentModels() {
		var count int64
		require.NoError(t, db.Model(value).Count(&count).Error)
		assert.Zero(t, count, "migration must not infer payment evidence or original credentials")
	}

	require.NoError(t, db.Migrator().DropIndex("kkai_membership_payment_evidence", "ux_membership_payment_evidence_receipt"))
	assert.ErrorIs(t, CheckMembershipPaymentSchema(ctx, db), ErrSchemaNotReady)
	require.NoError(t, CheckWalletPaymentSchema(ctx, db), "membership physical shape does not gate wallet payments")
	require.NoError(t, CheckShopRuntimeSchema(ctx, db), "membership physical shape does not gate shop requests")
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", MembershipPaymentSchemaVersion).Update("checksum", "invalid").Error)
	assert.ErrorIs(t, CheckWalletPaymentRuntimeSchema(ctx, db), ErrChecksumMismatch, "canonical later receipts are still required")
}

func TestMembershipPaymentRuntimeSchemaAndUniqueness(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				// Reuse the existing opt-in local-only disposable database fixture.
				db = newShopSchemaDatabaseFixture(t, dialect)
			}
			ctx := context.Background()
			require.NoError(t, db.Migrator().CreateTable(&AppliedMigration{}))
			for _, item := range planItemsForDialect(dialect, MembershipPaymentSchemaVersion) {
				require.NoError(t, db.Create(&AppliedMigration{Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item)}).Error)
			}
			require.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipPaymentSchemaVersion-1]))
			require.NoError(t, CheckMembershipPaymentRuntimeSchema(ctx, db), "unrelated application tables are not a request-time gate")
			for _, value := range model.MembershipPaymentModels() {
				statement := &gorm.Statement{DB: db}
				require.NoError(t, statement.Parse(value))
				require.NoError(t, (tenantOnlineStep{table: statement.Schema.Table, model: value}).validate(db))
				columns, err := db.Migrator().ColumnTypes(value)
				require.NoError(t, err)
				assert.Len(t, columns, len(statement.Schema.DBNames))
			}
			check := model.MembershipPaymentCheck{OrderID: 101, TradeNo: "member-101", UserID: 11, MerchantID: "merchant", EncryptedSnapshot: "encrypted", Status: "pending"}
			require.NoError(t, db.Create(&check).Error)
			assert.Error(t, db.Create(&model.MembershipPaymentCheck{OrderID: 102, TradeNo: check.TradeNo}).Error, "one remote order cannot be claimed for another membership order")
			assert.Error(t, db.Create(&model.MembershipPaymentCheck{OrderID: 101, TradeNo: "member-102"}).Error, "one order has one schedule")
			evidence := model.MembershipPaymentEvidence{OrderID: 101, ReceiptKey: "epay:receipt", ReceiptID: "receipt", MerchantID: "merchant", AmountMinor: 88800, Currency: "CNY", Source: "merchant_query", Status: "verified"}
			require.NoError(t, db.Create(&evidence).Error)
			assert.Error(t, db.Create(&model.MembershipPaymentEvidence{OrderID: 102, ReceiptKey: evidence.ReceiptKey}).Error, "one provider receipt cannot belong to two orders")
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipPaymentSchemaVersion-1]))
			var saved model.MembershipPaymentEvidence
			require.NoError(t, db.First(&saved, evidence.ID).Error)
			assert.Equal(t, evidence, saved, "replaying additive DDL preserves verified money")

			require.NoError(t, db.Where("version = ?", MembershipPaymentSchemaVersion).Delete(&AppliedMigration{}).Error)
			assert.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady)
			item := migrationSet()[MembershipPaymentSchemaVersion-1]
			require.NoError(t, db.Create(&AppliedMigration{Version: item.Version, Name: item.Name, Checksum: "invalid"}).Error)
			assert.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrChecksumMismatch)
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", item.Version).Update("checksum", storedMigrationChecksum(item)).Error)

			require.NoError(t, db.Migrator().DropIndex("kkai_membership_payment_checks", "idx_membership_payment_check_due"))
			assert.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady, "bounded reconciliation scans require their due index")
			require.NoError(t, db.Exec("CREATE INDEX idx_membership_payment_check_due ON kkai_membership_payment_checks (status, next_check_at, claim_until)").Error)
			require.NoError(t, db.Migrator().DropIndex("kkai_membership_payment_evidence", "ux_membership_payment_evidence_receipt"))
			require.NoError(t, db.Exec("CREATE INDEX ux_membership_payment_evidence_receipt ON kkai_membership_payment_evidence (receipt_key)").Error)
			assert.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady, "a same-name nonunique index must not authorize fulfillment")
			require.NoError(t, db.Migrator().DropIndex("kkai_membership_payment_evidence", "ux_membership_payment_evidence_receipt"))
			require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_membership_payment_evidence_receipt ON kkai_membership_payment_evidence (receipt_key)").Error)
			require.NoError(t, CheckMembershipPaymentRuntimeSchema(ctx, db))
			require.NoError(t, db.Exec("ALTER TABLE kkai_membership_payment_evidence DROP COLUMN validation_error").Error)
			assert.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), ErrSchemaNotReady, "lost validation evidence must fail closed")
		})
	}
}

func TestMembershipPaymentExpandCatalogIsAdditiveAcrossDialects(t *testing.T) {
	require.NoError(t, validateMigrationCatalog(migrationSet()))
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			tables := map[string]struct{}{}
			for _, statement := range membershipPaymentSchemaStatements[dialect] {
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

func TestMembershipPaymentSchemaRejectsCompositeSchedulePrimaryKey(t *testing.T) {
	db := newMigrationTestDB(t)
	for i, statement := range membershipPaymentSchemaStatements[DialectSQLite] {
		if i == 0 {
			statement.SQL = strings.Replace(statement.SQL, "order_id INTEGER PRIMARY KEY,", "order_id INTEGER NOT NULL,", 1)
			statement.SQL = strings.Replace(statement.SQL, "close_reason VARCHAR(32) NOT NULL\n)", "close_reason VARCHAR(32) NOT NULL,\nPRIMARY KEY(order_id,trade_no)\n)", 1)
		}
		require.NoError(t, db.Exec(statement.SQL).Error)
	}
	// A composite replacement would allow two schedules for the same order.
	err := validateMembershipPaymentSchema(db, true)
	require.ErrorIs(t, err, ErrSchemaNotReady)
	assert.Contains(t, err.Error(), "invalid primary key")
}
