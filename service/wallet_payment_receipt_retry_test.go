package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Build a receipted schema through the public maintenance APIs. Tables alone
// must never be sufficient to enable the recovery worker's financial writes.
func walletReceiptRecoveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		require.NoError(t, sqlDB.Close())
	})
	for _, ddl := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY, telegram_id TEXT, username VARCHAR(20), email VARCHAR(50))",
		"CREATE UNIQUE INDEX uni_users_username ON users(username)",
		"CREATE TABLE tokens(id INTEGER PRIMARY KEY)",
		"CREATE TABLE passkey_credentials(id INTEGER PRIMARY KEY)",
		"CREATE TABLE options(key VARCHAR(255) PRIMARY KEY, value TEXT)",
		"CREATE TABLE midjourneys(id INTEGER PRIMARY KEY, mj_id TEXT, channel_id BIGINT, quota BIGINT, status TEXT)",
		"CREATE TABLE top_ups(id INTEGER PRIMARY KEY, user_id INTEGER, trade_no VARCHAR(255), status VARCHAR(32), create_time INTEGER)",
		"CREATE TABLE user_oauth_bindings(id INTEGER PRIMARY KEY)",
		"CREATE TABLE logs(id INTEGER PRIMARY KEY)",
		"CREATE TABLE quota_data(id INTEGER PRIMARY KEY)",
		"CREATE TABLE tasks(id INTEGER PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	ctx := context.Background()
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyLegacyBaseline, kkaimigrate.ApplyOutboxEventKeyCompatibility,
		kkaimigrate.ApplyVideoStudioExpand, kkaimigrate.ApplyVideoSampleCategoryExpand,
		kkaimigrate.ApplyImageStudioExpand, kkaimigrate.ApplyAuthenticationExpand,
		kkaimigrate.ApplyRC41ArchitectureExpand, kkaimigrate.ApplyImageAsyncExpand,
		kkaimigrate.ApplyMembershipExpand, kkaimigrate.ApplyTenantOnlineExpansion,
		kkaimigrate.FinalizeTenantOnlineSchema, kkaimigrate.ApplyResellerApplicationExpand,
		kkaimigrate.ApplyRequestTicketExpand, kkaimigrate.ApplyInvoiceExpand,
		kkaimigrate.ApplyWalletPaymentExpand,
	} {
		_, err := apply(ctx, db, kkaimigrate.Options{Offline: true})
		require.NoError(t, err)
	}
	for _, value := range []any{&model.User{}, &model.TopUp{}} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		present := make(map[string]bool, len(columns))
		for _, column := range columns {
			present[column.Name()] = true
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !field.IgnoreMigration && !present[field.DBName] {
				require.NoError(t, db.Migrator().AddColumn(value, field.Name))
			}
		}
	}
	require.NoError(t, kkaimigrate.CheckWalletPaymentSchema(ctx, db))
	return db
}

func TestWalletReceiptRecoveryAfterCrashBeforeCredit(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	t.Setenv("TOPUP_REBATE_ACTIVE_FROM_ID", "1")
	user := model.User{Id: 41, Username: "recovery-user", AffCode: "recovery-user", Group: "default", Quota: 10}
	require.NoError(t, db.Create(&user).Error)
	now := common.GetTimestamp()
	order := model.TopUp{UserId: user.Id, TradeNo: "crash-before-accounting", Status: "expired", CreateTime: now - 60,
		WalletLifecycleVersion: 1, PaymentProvider: "stripe", PaymentMethod: "stripe", ProviderAccountRef: "original-merchant",
		ProviderEnvironment: "test", PaidMinor: 100, Currency: "USD", CurrencyExponent: 2, PricingMode: "fixed", CreditedQuota: 500}
	require.NoError(t, db.Create(&order).Error)
	// A paused historical run must not pause independently committed paid facts.
	require.NoError(t, db.Create(&model.WalletTopUpRun{ID: "paused-history", Status: "paused", DesiredState: "paused"}).Error)
	receipt, err := model.SaveWalletPaymentReceipt(&model.WalletPaymentReceipt{
		Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment,
		TransactionID: "tx-original", TopUpID: &order.Id, AmountMinor: 100, Currency: "USD", CurrencyExponent: 2,
		PaidAt: now - 10, EvidenceSource: "authenticated_query", EvidenceRef: "provider-event-original",
		VerificationStatus: "verified", PricingVerified: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "pending", receipt.ProcessingStatus)
	counts, err := RetryDueWalletPaymentReceipts(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"credited": 1, "review_required": 0}, counts)
	counts, err = RetryDueWalletPaymentReceipts(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"credited": 0, "review_required": 0}, counts)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 510, user.Quota)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("event_key = ?", "newapi:topup:1").Count(&events).Error)
	assert.EqualValues(t, 1, events)
	var run model.WalletTopUpRun
	require.NoError(t, db.First(&run, "id = ?", "paused-history").Error)
	assert.Equal(t, "paused", run.Status)
}
