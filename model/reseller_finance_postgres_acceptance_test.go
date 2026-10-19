package model

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This fixture never migrates or drops public tables. The explicit local test
// database restriction also prevents an accidentally inherited production DSN.
func resellerPostgresAcceptanceDB(t *testing.T) (*gorm.DB, *gorm.DB, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_RESELLER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_RESELLER_POSTGRES_DSN is not configured; PostgreSQL acceptance was not run")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, config.Host)
	for _, fallback := range config.Fallbacks {
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
	}
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_"), "requires a dedicated kkai_test_ database")
	config.RuntimeParams["statement_timeout"] = "10000"
	config.RuntimeParams["lock_timeout"] = "5000"
	adminSQL := stdlib.OpenDB(*config)
	admin, err := gorm.Open(postgres.New(postgres.Config{Conn: adminSQL}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "reseller_finance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() {
		assert.NoError(t, admin.Exec(`DROP SCHEMA "`+schema+`" CASCADE`).Error)
		assert.NoError(t, adminSQL.Close())
	})
	config.RuntimeParams["search_path"] = schema
	connections := make([]*gorm.DB, 0, 2)
	for range 2 {
		sqlDB := stdlib.OpenDB(*config)
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		connections = append(connections, db)
		t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	}
	db, second := connections[0], connections[1]
	var firstPID, secondPID int
	require.NoError(t, db.Raw("SELECT pg_backend_pid()").Scan(&firstPID).Error)
	require.NoError(t, second.Raw("SELECT pg_backend_pid()").Scan(&secondPID).Error)
	require.NotEqual(t, firstPID, secondPID, "contention must use independent PostgreSQL sessions")
	previousDB, previousType, previousRedis := DB, common.MainDatabaseType(), common.RedisEnabled
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	DB, common.RedisEnabled = db, false
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		DB, common.RedisEnabled = previousDB, previousRedis
		common.SetMainDatabaseType(previousType)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &TopUp{}, &TenantConsumption{}, &PricingQuote{}, &CustomerPaymentLot{}, &ResellerWallet{}, &ResellerLedgerEntry{}, &ResellerWithdrawal{}, &CustomerPaymentReversal{}, &KKAIOutboxEvent{}))
	return db, second, admin
}

func TestResellerPostgresPaidConsumptionWithdrawalChargeback(t *testing.T) {
	db, _, _ := resellerPostgresAcceptanceDB(t)
	user := User{TenantID: 42, Username: "paid-customer", AffCode: "paid-customer", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := Token{TenantID: 42, UserId: user.Id, Key: "paid-consumption", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 42, ExactProfitCarry: "0", Version: 1}).Error)
	topup := TopUp{TenantID: 42, UserId: user.Id, TradeNo: "paid-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, topup.SetCNYPaymentSnapshot("1000.00"))
	require.NoError(t, topup.Insert())
	input := FinalizeTopUpInput{TradeNo: topup.TradeNo, ExpectedProvider: PaymentProviderEpay, CompletedAt: 1000, Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
		return TopUpCompletion{QuotaDelta: 1000}, nil
	}}
	paid, err := FinalizeTopUp(input)
	require.NoError(t, err)
	assert.False(t, paid.AlreadyCompleted)
	replayed, err := FinalizeTopUp(input)
	require.NoError(t, err)
	assert.True(t, replayed.AlreadyCompleted)
	var lot CustomerPaymentLot
	require.NoError(t, db.First(&lot, "top_up_id = ?", topup.Id).Error)
	assert.Equal(t, int64(100000), lot.PaidMinor)
	assert.Equal(t, int64(1600), lot.PaymentFeeMinor)
	assert.Equal(t, int64(1000), lot.RemainingQuota)
	var outboxCount, lotCount int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.NoError(t, db.Model(&CustomerPaymentLot{}).Count(&lotCount).Error)
	assert.Zero(t, outboxCount, "agent payments must not create platform invitation rewards")
	assert.Equal(t, int64(1), lotCount, "duplicate payment callbacks must credit only one paid lot")

	quoteInput := ResellerPricingQuoteInput{TenantID: 42, CustomerID: user.Id, RequestID: "paid-request", PriceVersionID: 1, Model: "text", Group: "supply", SupplierPriceMinor: 20000, SaleMinor: 40000, SupplierMinorPerQuota: "50", SaleMinorPerQuota: "100", SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 400, Now: 1001}
	request, err := ReserveTenantConsumption(context.Background(), db, quoteInput, token.Id)
	require.NoError(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, int64(600), user.Quota)
	assert.Equal(t, 600, token.RemainQuota)
	// The upstream result is injected at its durable model boundary; no external
	// model or payment provider is contacted by this database acceptance test.
	require.NoError(t, RecordTenantConsumptionResult(db, request.RequestID, 500, 0, false))
	require.NoError(t, SettleTenantConsumption(context.Background(), db, request.RequestID))
	require.NoError(t, SettleTenantConsumption(context.Background(), db, request.RequestID))
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, int64(500), user.Quota)
	assert.Equal(t, int64(500), user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 500, token.RemainQuota)
	assert.Equal(t, 500, token.UsedQuota)
	require.NoError(t, db.First(&lot, lot.ID).Error)
	assert.Equal(t, int64(500), lot.ConsumedQuota)
	assert.Equal(t, int64(500), lot.RemainingQuota)
	var settlement ResellerLedgerEntry
	require.NoError(t, db.First(&settlement, "operation_id = ?", "settlement:paid-request").Error)
	assert.Equal(t, int64(50000), settlement.SaleMinor)
	assert.Equal(t, int64(800), settlement.PaymentFeeMinor)
	assert.Equal(t, "25000", settlement.SupplierMinorExact)
	assert.Equal(t, int64(24200), settlement.AmountMinor)
	assert.Equal(t, settlement.CreatedAt+24*60*60, settlement.AvailableAt)
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
	assert.Equal(t, int64(24200), wallet.PendingMinor)
	assert.Zero(t, wallet.AvailableMinor)
	for _, release := range []struct {
		at    int64
		count int
	}{{settlement.AvailableAt - 1, 0}, {settlement.AvailableAt, 1}, {settlement.AvailableAt + 1, 0}} {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			count, err := ReleaseResellerPendingTx(tx, 42, release.at, 0)
			assert.Equal(t, release.count, count)
			return err
		}))
	}
	var withdrawal *ResellerWithdrawal
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		withdrawal, err = RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 42, IdempotencyKey: "paid-withdrawal", AmountMinor: 20000, FeeMinor: 100, FeePolicySnapshot: "flat:100", AlipayAccount: "agent@example.test", AlipayName: "Agent", Now: settlement.AvailableAt + 2})
		return err
	}))
	assert.Equal(t, int64(19900), withdrawal.NetMinor)
	require.NoError(t, db.First(&wallet, wallet.ID).Error)
	assert.Equal(t, int64(4200), wallet.AvailableMinor)
	assert.Equal(t, int64(20000), wallet.FrozenMinor)
	for _, status := range []string{ResellerWithdrawalApproved, ResellerWithdrawalPaid} {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			ref := ""
			if status == ResellerWithdrawalPaid {
				ref = "confirmed-manual-alipay"
			}
			_, err := ReviewResellerWithdrawalTx(tx, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, ReviewerID: 99, Status: status, PaymentRef: ref, Now: settlement.AvailableAt + 3})
			return err
		}))
	}
	chargeback := CustomerPaymentReversalInput{TenantID: 42, TopUpID: int64(topup.Id), IdempotencyKey: "provider-chargeback", Kind: "chargeback", ActorID: 99, Reason: "provider confirmed reversal", PaymentReference: "provider-reversal-proof", Now: settlement.AvailableAt + 4}
	for range 2 {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			reversal, err := ReverseCustomerPaymentTx(tx, chargeback)
			if err == nil {
				assert.Equal(t, int64(500), reversal.RemovedQuota)
				assert.EqualValues(t, "24200", reversal.ReversedProfitMinorExact)
			}
			return err
		}))
	}
	require.NoError(t, db.First(withdrawal, withdrawal.ID).Error)
	assert.Equal(t, ResellerWithdrawalPaid, withdrawal.Status)
	assert.Equal(t, "flat:100", withdrawal.FeePolicySnapshot)
	assert.Equal(t, "confirmed-manual-alipay", withdrawal.PaymentRef)
	var originalSettlement ResellerLedgerEntry
	require.NoError(t, db.First(&originalSettlement, settlement.ID).Error)
	assert.Equal(t, settlement, originalSettlement, "chargebacks must append reversals without rewriting historical settlement")
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.Zero(t, wallet.AvailableMinor)
	assert.Zero(t, wallet.FrozenMinor)
	assert.Equal(t, int64(20000), wallet.DebtMinor)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	require.NoError(t, db.First(&lot, lot.ID).Error)
	assert.Equal(t, int64(500), lot.RefundedQuota)
	assert.Zero(t, lot.RemainingQuota)
	assert.Equal(t, "chargeback", lot.RefundStatus)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 42, IdempotencyKey: "blocked-debt", AmountMinor: 10000, AlipayAccount: "agent@example.test", AlipayName: "Agent"})
		return err
	}), ErrResellerDebtOutstanding)
	var entries []ResellerLedgerEntry
	require.NoError(t, db.Order("id").Find(&entries).Error)
	require.Len(t, entries, 5, "replay must not append a second settlement or chargeback")
	var pending, available, frozen, debt int64
	for _, entry := range entries {
		pending += entry.PendingDelta
		available += entry.AvailableDelta
		frozen += entry.FrozenDelta
		debt += entry.DebtDelta
	}
	assert.Equal(t, wallet.PendingMinor, pending)
	assert.Equal(t, wallet.AvailableMinor, available)
	assert.Equal(t, wallet.FrozenMinor, frozen)
	assert.Equal(t, wallet.DebtMinor, debt)
}

// Execute the second operation while the first transaction's row lock is held.
// Observing PostgreSQL's lock graph makes this a contention regression rather
// than two goroutines that might run sequentially through one connection pool.
func contendResellerPostgresTransactions(t *testing.T, first, second, observer *gorm.DB, operation func(*gorm.DB, int) error) [2]error {
	t.Helper()
	tx := first.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	require.NoError(t, operation(tx, 0))
	var firstPID, secondPID int
	require.NoError(t, tx.Raw("SELECT pg_backend_pid()").Scan(&firstPID).Error)
	require.NoError(t, second.Raw("SELECT pg_backend_pid()").Scan(&secondPID).Error)
	result := make(chan error, 1)
	go func() { result <- second.Transaction(func(tx *gorm.DB) error { return operation(tx, 1) }) }()
	blocked := assert.Eventually(t, func() bool {
		var waits bool
		err := observer.Raw("SELECT ? = ANY(pg_blocking_pids(?))", firstPID, secondPID).Scan(&waits).Error
		return err == nil && waits
	}, 3*time.Second, 10*time.Millisecond, "second independent session must wait on the first business operation's row lock")
	if !blocked {
		tx.Rollback()
		<-result
		t.FailNow()
	}
	return [2]error{tx.Commit().Error, <-result}
}

func TestResellerPostgresConcurrentWithdrawals(t *testing.T) {
	if os.Getenv("TEST_RESELLER_POSTGRES_DSN") == "" {
		t.Skip("TEST_RESELLER_POSTGRES_DSN is not configured; PostgreSQL acceptance was not run")
	}
	for _, sameKey := range []bool{false, true} {
		name := "competing-withdrawals"
		if sameKey {
			name = "same-idempotency-key"
		}
		t.Run(name, func(t *testing.T) {
			db, second, observer := resellerPostgresAcceptanceDB(t)
			require.NoError(t, db.Create(&ResellerWallet{TenantID: 42, AvailableMinor: 15000, ExactProfitCarry: "0", Version: 1}).Error)
			var withdrawalIDs [2]int64
			results := contendResellerPostgresTransactions(t, db, second, observer, func(tx *gorm.DB, index int) error {
				key := "first"
				if index == 1 && !sameKey {
					key = "second"
				}
				withdrawal, err := RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 42, IdempotencyKey: key, AmountMinor: 10000, FeeMinor: 100, FeePolicySnapshot: "flat:100", AlipayAccount: "agent@example.test", AlipayName: "Agent", Now: 100})
				if err == nil {
					withdrawalIDs[index] = withdrawal.ID
				}
				return err
			})
			require.NoError(t, results[0])
			if sameKey {
				require.NoError(t, results[1])
				assert.Equal(t, withdrawalIDs[0], withdrawalIDs[1])
			} else {
				assert.ErrorIs(t, results[1], ErrResellerInsufficientFunds)
			}
			var wallet ResellerWallet
			require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
			assert.Equal(t, int64(5000), wallet.AvailableMinor)
			assert.Equal(t, int64(10000), wallet.FrozenMinor)
			var withdrawalCount, ledgerCount int64
			require.NoError(t, db.Model(&ResellerWithdrawal{}).Count(&withdrawalCount).Error)
			require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&ledgerCount).Error)
			assert.Equal(t, int64(1), withdrawalCount)
			assert.Equal(t, int64(1), ledgerCount)
		})
	}
}

func TestResellerPostgresConcurrentSettlement(t *testing.T) {
	db, second, observer := resellerPostgresAcceptanceDB(t)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 42, ExactProfitCarry: "0", Version: 1}).Error)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 42, CustomerID: 7, TopUpID: 1, OperationID: "paid", PaidMinor: 10000, PaymentFeeMinor: 160, CreditedQuota: 1000, Now: 100})
	require.NoError(t, err)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 42, CustomerID: 7, RequestID: "concurrent-settlement", PriceVersionID: 1, Model: "text", Group: "supply", SupplierPriceMinor: 500, SaleMinor: 1000, SupplierMinorPerQuota: "5", SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 100, Now: 100})
	require.NoError(t, err)
	var entryIDs [2]int64
	results := contendResellerPostgresTransactions(t, db, second, observer, func(tx *gorm.DB, index int) error {
		entry, err := SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 42, CustomerID: 7, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 150, Now: 200 + int64(index)})
		if err == nil {
			entryIDs[index] = entry.ID
		}
		return err
	})
	require.NoError(t, results[0])
	require.NoError(t, results[1])
	assert.Equal(t, entryIDs[0], entryIDs[1])
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
	assert.Equal(t, int64(726), wallet.PendingMinor)
	var lot CustomerPaymentLot
	require.NoError(t, db.First(&lot, "top_up_id = ?", 1).Error)
	assert.Equal(t, int64(150), lot.ConsumedQuota)
	assert.Equal(t, int64(850), lot.RemainingQuota)
	var count int64
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
