package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipExpandPreservesV10SchemaAndData(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO users (id,telegram_id) VALUES (42,'123')").Error)
	_, err := applyThroughVersion(context.Background(), db, Options{}, ImageAsyncSchemaVersion, KnownCatalogVersion)
	require.NoError(t, err)
	require.NoError(t, CheckImageAsyncSchema(context.Background(), db))
	require.False(t, db.Migrator().HasTable("kkai_membership_accounts"))
	require.True(t, db.Migrator().HasTable("kkai_image_executions"))
	assert.ErrorIs(t, CheckMembershipSchema(context.Background(), db), ErrSchemaNotReady)
	dry, err := ApplyMembershipExpand(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	require.False(t, db.Migrator().HasTable("kkai_membership_accounts"))
	observation, err := Observe(context.Background(), db)
	require.NoError(t, err)
	assert.EqualValues(t, 10, observation.CurrentVersion)
	var history []AppliedMigration
	require.NoError(t, db.Order("version").Find(&history).Error)
	require.Len(t, history, 10)
	imageUpload := model.KKAIImageReferenceUpload{AssetID: 17, OwnerUserID: 42, UploadKey: "membership-migration", UploadID: "multipart-17", UploadManifest: "[]"}
	require.NoError(t, db.Create(&imageUpload).Error)
	dry, err = ApplyMembershipExpand(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	assert.EqualValues(t, MembershipSchemaVersion, dry.Pending[0].Version)
	require.False(t, db.Migrator().HasTable("kkai_membership_accounts"))
	_, err = ApplyMembershipExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckMembershipSchema(context.Background(), db))
	require.NoError(t, CheckImageAsyncSchema(context.Background(), db))
	var preserved []AppliedMigration
	require.NoError(t, db.Where("version <= ?", ImageAsyncSchemaVersion).Order("version").Find(&preserved).Error)
	assert.Equal(t, history, preserved)
	var restoredUpload model.KKAIImageReferenceUpload
	require.NoError(t, db.First(&restoredUpload, 17).Error)
	assert.Equal(t, imageUpload, restoredUpload)
	for _, record := range []any{
		&model.MembershipCatalogState{ID: 1},
		&model.MembershipPricingRelease{ID: 1, Payload: `{"tiers":[]}`, ActorID: 1},
		&model.MembershipGroupRecord{GroupKey: "gpt", Enabled: true, FirstReleaseID: 1},
		&model.MembershipUserState{UserID: 42, CurrentAccountID: 1},
		&model.MembershipAccount{ID: 1, UserID: 42, TierID: "gold", Rank: 1, ReleaseID: 1, AmountTotal: 100, AmountReserved: 40, Revision: 1, Status: model.MembershipActive},
		&model.MembershipOrder{ID: 1, UserID: 42, TradeNo: "schema-order", TierID: "gold", Status: model.MembershipOrderPending, CheckoutState: "ready", CheckoutData: `{"url":"saved"}`},
		&model.MembershipPaymentReceipt{OrderID: 1, ReceiptKey: "epay:receipt", Provider: "epay", ReceiptID: "receipt", AmountMinor: 1000, Currency: "CNY", OneTime: true, Status: model.MembershipOrderSuccess},
		&model.MembershipBillingRecord{RequestID: "schema-request", Scope: model.MembershipBillingScopeRelay, UserID: 42, AccountID: 1, Source: model.MembershipSource, ReservedQuota: 40, TokenQuotaDebited: true, TokenRefundUnapplied: 2, TokenUsedRefundUnapplied: 3, State: "reserved", DispatchedAt: 100, ReviewAfter: 1000, ReviewReason: "uncertain", ReviewStartedAt: 999, ReviewChannelID: 7},
	} {
		require.NoError(t, db.Create(record).Error)
	}
	var account model.MembershipAccount
	require.NoError(t, db.First(&account, 1).Error)
	assert.Equal(t, 1, account.Rank)
	assert.EqualValues(t, 60, account.Available())
	var record model.MembershipBillingRecord
	require.NoError(t, db.First(&record, "request_id = ?", "schema-request").Error)
	assert.True(t, record.TokenQuotaDebited)
	assert.EqualValues(t, 2, record.TokenRefundUnapplied)
	assert.Equal(t, model.MembershipBillingScopeRelay, record.Scope)
	assert.EqualValues(t, 100, record.DispatchedAt)
	assert.EqualValues(t, 1000, record.ReviewAfter)
	assert.Equal(t, "uncertain", record.ReviewReason)
	assert.Equal(t, 7, record.ReviewChannelID)
	// NULL receipt keys allow multiple unpaid orders; verified provider receipt IDs
	// remain unique in their own ledger even when one order receives extra payments.
	require.NoError(t, db.Create(&model.MembershipOrder{TradeNo: "second-pending"}).Error)
	require.Error(t, db.Create(&model.MembershipPaymentReceipt{OrderID: 2, ReceiptKey: "epay:receipt"}).Error)
	_, err = ApplyMembershipExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	imageResult, err := ApplyImageAsyncExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.Empty(t, imageResult.Pending)
	var telegram string
	require.NoError(t, db.Table("users").Select("telegram_id").Where("id = ?", 42).Scan(&telegram).Error)
	assert.Equal(t, "123", telegram)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, CheckImageAsyncSchema(context.Background(), db))
	require.NoError(t, CheckMembershipSchema(context.Background(), db))
}

func TestMembershipExpandOnlyAddsCompanionTablesAcrossDialects(t *testing.T) {
	require.NoError(t, validateMigrationCatalog(migrationSet()))
	for _, dialect := range requiredMigrationDialects {
		t.Run(dialect, func(t *testing.T) {
			statements := membershipSchemaStatements[dialect]
			require.NotEmpty(t, statements)
			for _, statement := range statements {
				assert.NotContains(t, strings.ToUpper(statement.SQL), "DROP ")
				assert.NotContains(t, strings.ToUpper(statement.SQL), "ALTER ")
				assert.Contains(t, statement.SQL, "kkai_membership_")
			}
			joined := []string{}
			for _, statement := range statements {
				joined = append(joined, statement.SQL)
			}
			sql := strings.Join(joined, "\n")
			assert.Contains(t, sql, "tier_rank ")
			assert.Contains(t, sql, "CREATE UNIQUE INDEX ux_membership_payments_receipt")
			if dialect == DialectMySQL {
				assert.Contains(t, sql, "payload LONGTEXT NOT NULL")
				assert.Contains(t, sql, "ENGINE=InnoDB")
			}
		})
	}
}

func TestMembershipRuntimeRejectsMissingImageOrPaymentUniqueness(t *testing.T) {
	for _, index := range []struct{ table, name string }{
		{"kkai_image_reference_uploads", "ux_image_reference_upload_key"},
		{"kkai_membership_orders", "ux_membership_orders_trade"},
		{"kkai_membership_orders", "ux_membership_orders_receipt"},
		{"kkai_membership_payments", "ux_membership_payments_receipt"},
	} {
		t.Run(index.name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			_, err := applyThroughVersion(context.Background(), db, Options{}, MembershipSchemaVersion, KnownCatalogVersion)
			require.NoError(t, err)
			_, err = ApplyImageAsyncExpand(context.Background(), db, Options{})
			require.NoError(t, err)
			_, err = ApplyMembershipExpand(context.Background(), db, Options{})
			require.NoError(t, err)
			require.NoError(t, db.Migrator().DropIndex(index.table, index.name))
			assert.ErrorIs(t, CheckMembershipSchema(context.Background(), db), ErrSchemaNotReady)
			assert.ErrorIs(t, CheckImageAsyncSchema(context.Background(), db), ErrSchemaNotReady)
		})
	}
}
