package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func tenantConsumptionFixture(t *testing.T, db *gorm.DB) (User, Token, ResellerPricingQuoteInput) {
	t.Helper()
	t.Cleanup(func() {
		for _, row := range []any{&TenantConsumption{}, &PricingQuote{}, &CustomerPaymentLot{}, &ResellerWallet{}, &ResellerLedgerEntry{}, &ResellerWithdrawal{}, &TenantPriceVersion{}, &TenantCatalogEntry{}, &ResellerApplication{}, &TenantProfile{}, &TenantMembership{}, &TenantDomain{}, &Tenant{}, &Token{}, &User{}} {
			require.NoError(t, db.Migrator().DropTable(row))
		}
	})
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Tenant{}, &TenantDomain{}, &TenantMembership{}, &TenantProfile{}, &ResellerApplication{}, &TenantCatalogEntry{}, &TenantPriceVersion{}, &TenantConsumption{}, &PricingQuote{}, &CustomerPaymentLot{}, &ResellerWallet{}, &ResellerLedgerEntry{}, &ResellerWithdrawal{}))
	// A second migration must preserve all indices and data on every dialect.
	require.NoError(t, db.AutoMigrate(&TenantConsumption{}, &PricingQuote{}, &CustomerPaymentLot{}, &ResellerWallet{}, &ResellerLedgerEntry{}, &ResellerWithdrawal{}))
	user := User{Username: "tenant-customer", TenantID: 42, Status: common.UserStatusEnabled, Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, TenantID: 42, Key: "tenant-consumption-key", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 42, ExactProfitCarry: "0", Version: 1}).Error)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 42, CustomerID: user.Id, TopUpID: 1, OperationID: "tenant-paid", PaidMinor: 10000, PaymentFeeMinor: 160, CreditedQuota: 1000})
	require.NoError(t, err)
	input := ResellerPricingQuoteInput{TenantID: 42, CustomerID: user.Id, RequestID: "tenant-request", PriceVersionID: 1, Model: "text", Group: "supply", SupplierPriceMinor: 500, SaleMinor: 1000, SupplierMinorPerQuota: "5", SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 100}
	return user, token, input
}

func TestTenantConsumptionAtomicSettlementDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			switch dialect {
			case "sqlite":
				dialector = sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
			case "postgres":
				dsn := os.Getenv("TEST_TENANT_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_TENANT_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(dsn)
			case "mysql":
				dsn := os.Getenv("TEST_TENANT_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_TENANT_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			user, token, input := tenantConsumptionFixture(t, db)
			record, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
			require.NoError(t, err)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 900, user.Quota)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 900, token.RemainQuota)
			// Actual usage can exceed the original reservation. Usage and cash settle
			// atomically, and retries preserve both the quote and account balances.
			require.NoError(t, RecordTenantConsumptionResult(db, record.RequestID, 150, 0, false))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
			require.ErrorIs(t, RecordTenantConsumptionResult(db, record.RequestID, 0, 0, true), ErrTenantConsumptionConflict)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 850, user.Quota)
			assert.EqualValues(t, 150, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 850, token.RemainQuota)
			assert.Equal(t, 150, token.UsedQuota)
			var ledger []ResellerLedgerEntry
			require.NoError(t, db.Where("tenant_id = ?", 42).Find(&ledger).Error)
			require.Len(t, ledger, 1)
			assert.EqualValues(t, 726, ledger[0].AmountMinor)
			var wallet ResellerWallet
			require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
			assert.EqualValues(t, 726, wallet.PendingMinor)
			input.RequestID = "tenant-failed"
			failed, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
			require.NoError(t, err)
			require.NoError(t, RecordTenantConsumptionResult(db, failed.RequestID, 0, 0, true))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, failed.RequestID))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, failed.RequestID))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 850, user.Quota)
			var count int64
			require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestTenantConsumptionRejectsCrossTenantAndUnbackedCredit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-consumption-rejection?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	user, token, input := tenantConsumptionFixture(t, db)
	input.TenantID = 43
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrTenantMembershipMissing)
	input.TenantID = 42
	input.SupplierMinorPerQuota = "10.01"
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrResellerPriceBelowCost)
	input.SupplierMinorPerQuota = "5"
	require.NoError(t, db.Model(&user).Update("quota", 1001).Error)
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrResellerInsufficientFunds)
	var count int64
	require.NoError(t, db.Model(&PricingQuote{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestTenantConsumptionSettlementFailureRetainsRetryableIntent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-consumption-retry?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	user, token, input := tenantConsumptionFixture(t, db)
	record, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err)
	require.NoError(t, RecordTenantConsumptionResult(db, record.RequestID, 100, 0, false))
	require.NoError(t, db.Migrator().DropTable(&ResellerLedgerEntry{}))
	require.Error(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 900, user.Quota)
	assert.Zero(t, user.UsedQuota)
	var lot CustomerPaymentLot
	require.NoError(t, db.First(&lot).Error)
	assert.EqualValues(t, 1000, lot.RemainingQuota)
	require.NoError(t, db.First(record, record.ID).Error)
	assert.Equal(t, "settlement_pending", record.Status)
	assert.NotEmpty(t, record.LastError)
	input.RequestID = "new-request-while-settlement-pending"
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrTenantConsumptionConflict)
	require.NoError(t, db.AutoMigrate(&ResellerLedgerEntry{}))
	require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 100, user.UsedQuota)
}

func TestTenantConsumptionReservationsCannotHideUnbackedCredit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-consumption-backed-reservations?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	user, token, input := tenantConsumptionFixture(t, db)
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err)
	// 900 spendable + 100 reserved is fully funded. An administrator adding 50
	// quota must not become spendable merely because another request is in flight.
	require.NoError(t, db.Model(&user).Update("quota", 950).Error)
	input.RequestID = "unbacked-during-reservation"
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrResellerInsufficientFunds)
	require.NoError(t, db.Model(&user).Update("quota", 900).Error)
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err)
}

func TestTenantConsumptionFreeUnknownDoesNotBlockPaidReservation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-consumption-free-unknown?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	user, token, input := tenantConsumptionFixture(t, db)
	free, err := ReserveTenantFreeConsumption(context.Background(), db, user.TenantID, user.Id, token.Id, "free-unknown")
	require.NoError(t, err)
	require.NoError(t, MarkTenantConsumptionUncertain(db, free.RequestID, 1))
	_, err = ReserveTenantFreeConsumption(context.Background(), db, user.TenantID, user.Id, token.Id, free.RequestID)
	require.ErrorIs(t, err, ErrTenantConsumptionConflict)
	paid, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err, "a free unknown request reserves no customer funds")
	require.NoError(t, db.First(free, free.ID).Error)
	assert.Equal(t, "reserved", free.Status)
	assert.Equal(t, "upstream_final_usage_unknown", free.LastError)
	assert.Zero(t, free.ReservedQuota)
	require.NoError(t, MarkTenantConsumptionUncertain(db, paid.RequestID, 1))
	input.RequestID = "after-paid-unknown"
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrTenantConsumptionConflict, "an unknown paid request retains its fund reservation")
}
