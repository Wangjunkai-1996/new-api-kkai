package service

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The first read establishes the same repeatable-read snapshot that operation
// and request lookups establish before invoice payment locks in production.
// A separate connection then commits the anomaly before the payment lock.
func TestInvoiceMySQLCurrentPaymentFactsAfterRepeatableRead(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("KKAI_TEST_MYSQL57_DSN"))
	if dsn == "" {
		t.Skip("KKAI_TEST_MYSQL57_DSN is not configured")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(config.DBName, "kkai_test_"), "integration DSN must use a dedicated kkai_test_* database")
	s, user := invoiceFixture(t)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(3)
	t.Cleanup(func() { _ = raw.Close() })
	// Refuse to alter an existing business table even inside a test database.
	tables := []any{&model.TopUp{}, &model.InvoicePaymentReceipt{}, &model.MembershipOrder{}, &model.MembershipPaymentReceipt{}, &model.SubscriptionOrder{}, &model.InvoiceRequest{}, &model.InvoiceOrderClaim{}}
	for _, table := range tables {
		require.False(t, db.Migrator().HasTable(table), "invoice integration tables must not already exist")
	}
	t.Cleanup(func() {
		for _, table := range tables {
			require.NoError(t, db.Migrator().DropTable(table))
		}
	})
	require.NoError(t, db.AutoMigrate(tables...))
	previousDBType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	t.Cleanup(func() { common.SetMainDatabaseType(previousDBType) })
	s.DB = db
	now := time.Now().Unix() - 100
	require.NoError(t, db.Create(&model.TopUp{Id: 1, UserId: 1, TradeNo: "payment-a", PaidMinor: 50000, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: now}).Error)
	require.NoError(t, db.Create(&model.InvoicePaymentReceipt{TopUpID: 1, UserID: 1, Provider: "epay", ReceiptKey: strings.Repeat("a", 64), AmountMinor: 50000, Currency: "CNY", Status: "verified", PaidAt: now, CreatedAt: now}).Error)
	key := "epay:rr-receipt"
	require.NoError(t, db.Create(&model.MembershipOrder{ID: 10, TradeNo: "member-paid", UserID: 1, AmountMinor: 69900, Currency: "CNY", Provider: "epay", Status: model.MembershipOrderSuccess, ReceiptKey: &key, ReceiptID: "rr-receipt", ActualAmountMinor: 69900, ActualCurrency: "CNY", ActualProvider: "epay", ActualOneTime: true, PaidAt: now}).Error)
	require.NoError(t, db.Create(&model.MembershipPaymentReceipt{OrderID: 10, ReceiptKey: key, ReceiptID: "rr-receipt", Provider: "epay", AmountMinor: 69900, Currency: "CNY", OneTime: true, Status: model.MembershipOrderSuccess}).Error)
	for _, tc := range []struct {
		name string
		ref  InvoiceOrderRef
		fail func() error
	}{
		{"topup status", InvoiceOrderRef{SourceType: "topup", OrderID: "payment-a"}, func() error { return db.Model(&model.TopUp{}).Where("id = ?", 1).Update("status", "failed").Error }},
		{"topup receipt status", InvoiceOrderRef{SourceType: "topup", OrderID: "payment-a"}, func() error {
			return db.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", 1).Update("status", "exception").Error
		}},
		{"cross-source receipt claim", InvoiceOrderRef{SourceType: "topup", OrderID: "payment-a"}, func() error {
			return db.Create(&model.InvoiceOrderClaim{TenantID: 0, CanonicalPaymentKey: invoiceHash("payment:receipt:v1:0:" + strings.Repeat("a", 64)), RequestID: 91, CreatedAt: now}).Error
		}},
		{"membership receipt status", InvoiceOrderRef{SourceType: "membership", OrderID: "member-paid"}, func() error {
			return db.Model(&model.MembershipPaymentReceipt{}).Where("order_id = ?", 10).Update("status", model.MembershipOrderException).Error
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", 1).Update("status", common.TopUpStatusSuccess).Error)
			require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", 1).Update("status", "verified").Error)
			require.NoError(t, db.Where("1 = 1").Delete(&model.InvoiceOrderClaim{}).Error)
			ctx := context.Background()
			tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
			require.NoError(t, tx.Error)
			t.Cleanup(func() { _ = tx.Rollback().Error })
			input := InvoiceQuoteInput{Items: []InvoiceOrderRef{tc.ref}, InvoiceType: "ordinary", PolicyVersion: "test-v1"}
			before, err := s.quote(ctx, user, tx, input, false, 0)
			require.NoError(t, err)
			require.True(t, before.Eligible)
			require.NoError(t, tc.fail())
			stale, err := s.quote(ctx, user, tx, input, false, 0)
			require.NoError(t, err)
			require.True(t, stale.Eligible, "the ordinary read must retain the old RR snapshot for this regression")
			current, err := s.quote(ctx, user, tx, input, true, 0)
			require.NoError(t, err)
			assert.False(t, current.Eligible, "invoice writes must validate the current locked payment and receipt facts")
			assert.Equal(t, "0", current.AmountMinor)
		})
	}
}
