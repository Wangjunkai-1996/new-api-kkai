package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInvoiceWithdrawalReleasesPaymentsAndPreservesHistory(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	input := invoiceCreateInput()
	extra := model.TopUp{UserId: user.UserID, TradeNo: "payment-b", PaidMinor: 20000, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: 100}
	require.NoError(t, s.DB.Create(&extra).Error)
	require.NoError(t, s.DB.Create(&model.InvoicePaymentReceipt{TopUpID: int64(extra.Id), UserID: user.UserID, Provider: "epay", ReceiptKey: strings.Repeat("b", 64), AmountMinor: 20000, Currency: "CNY", Status: "verified", PaidAt: 100}).Error)
	input.Items = append(input.Items, InvoiceOrderRef{SourceType: "topup", OrderID: extra.TradeNo})
	original, err := s.Create(ctx, user, "withdraw-create", input)
	require.NoError(t, err)
	require.Contains(t, original.AllowedActions, "withdraw")
	version := InvoiceActionInput{Version: original.Version}
	withdrawn, err := s.Mutate(ctx, user, original.PublicID, "withdraw", "withdraw-key", version)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceWithdrawn, withdrawn.Status)
	assert.Equal(t, original.Version+1, withdrawn.Version)
	assert.Empty(t, withdrawn.AllowedActions)
	assert.Equal(t, original.Profile, withdrawn.Profile)
	assert.Equal(t, original.Items, withdrawn.Items)
	require.Len(t, withdrawn.Events, 2)
	assert.Equal(t, "submitted", withdrawn.Events[0].Kind)
	assert.Equal(t, "withdraw", withdrawn.Events[1].Kind)
	assert.False(t, withdrawn.Events[1].ActorAdmin)

	completed, err := s.List(ctx, user, InvoiceFilter{Status: model.InvoiceWithdrawn, Group: "completed"})
	require.NoError(t, err)
	require.Len(t, completed.Items, 1)
	assert.Equal(t, original.PublicID, completed.Items[0].PublicID)
	summary, err := s.Summary(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.Zero(t, summary.ProcessingCount)
	assert.Zero(t, summary.AwaitingConfirmationCount)
	assert.EqualValues(t, 1, summary.Counts[model.InvoiceWithdrawn])
	require.Len(t, summary.InvoicedTotals, 1)
	assert.Equal(t, "0", summary.InvoicedTotals[0].AmountMinor)
	require.Len(t, summary.Totals, 1)
	assert.EqualValues(t, 2, summary.EligibleCount)
	assert.Equal(t, "70000", summary.Totals[0].AmountMinor)
	reapplied, err := s.Create(ctx, user, "withdraw-reapply", input)
	require.NoError(t, err)
	assert.NotEqual(t, original.PublicID, reapplied.PublicID)

	replay, err := s.Mutate(ctx, user, original.PublicID, "withdraw", "withdraw-key", version)
	require.NoError(t, err)
	assert.Equal(t, withdrawn, replay)
	_, err = s.Mutate(ctx, user, original.PublicID, "withdraw", "withdraw-key", InvoiceActionInput{Version: withdrawn.Version})
	invoiceRequireCode(t, err, "IDEMPOTENCY_CONFLICT")
	_, err = s.Mutate(ctx, user, original.PublicID, "withdraw", "withdraw-new", InvoiceActionInput{Version: withdrawn.Version})
	invoiceRequireCode(t, err, "INVALID_STATE")
	quote, err := s.Quote(ctx, user, input.InvoiceQuoteInput)
	require.NoError(t, err)
	require.Len(t, quote.Items, 2)
	for _, item := range quote.Items {
		assert.Equal(t, reapplied.PublicID, item.ClaimedRequestPublicID)
	}
	admin := InvoiceScope{UserID: 2, Admin: true}
	notifications, err := s.Notifications(ctx, admin, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 3, notifications.Total)
	notifications, err = s.Notifications(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.Zero(t, notifications.Total)
}

func TestInvoiceWithdrawalRollsBackIfNotificationCannotBeQueued(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	original, err := s.Create(ctx, user, "rollback-create", invoiceCreateInput())
	require.NoError(t, err)
	require.NoError(t, s.DB.Exec("CREATE TRIGGER fail_enqueue BEFORE INSERT ON kkai_outbox BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END").Error)
	input := InvoiceActionInput{Version: original.Version}
	_, err = s.Mutate(ctx, user, original.PublicID, "withdraw", "rollback-withdraw", input)
	require.Error(t, err)
	current, err := s.Get(ctx, user, original.PublicID)
	require.NoError(t, err)
	assert.Equal(t, original, current)
	candidates, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "eligible"})
	require.NoError(t, err)
	assert.Zero(t, candidates.Total)
	require.NoError(t, s.DB.Exec("DROP TRIGGER fail_enqueue").Error)
	current, err = s.Mutate(ctx, user, original.PublicID, "withdraw", "rollback-withdraw", input)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceWithdrawn, current.Status)
}

func TestInvoiceWithdrawalGuards(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	request, err := s.Create(ctx, user, "guards-create", invoiceCreateInput())
	require.NoError(t, err)
	version := InvoiceActionInput{Version: request.Version}
	for _, invalid := range []int64{0, -1, request.Version + 1} {
		_, err = s.Mutate(ctx, user, request.PublicID, "withdraw", "guards-version", InvoiceActionInput{Version: invalid})
		if invalid <= 0 {
			invoiceRequireCode(t, err, "VALIDATION_FAILED")
		} else {
			invoiceRequireCode(t, err, "VERSION_CONFLICT")
		}
	}
	_, err = s.Mutate(ctx, user, request.PublicID, "withdraw", "", version)
	invoiceRequireCode(t, err, "IDEMPOTENCY_KEY_REQUIRED")
	_, err = s.Mutate(ctx, InvoiceScope{UserID: 2}, request.PublicID, "withdraw", "guards-owner", version)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = s.Mutate(ctx, InvoiceScope{TenantID: 1, UserID: user.UserID}, request.PublicID, "withdraw", "guards-tenant", version)
	invoiceRequireCode(t, err, "FORBIDDEN")
	_, err = s.Mutate(ctx, InvoiceScope{Admin: true, UserID: 2}, request.PublicID, "withdraw", "guards-admin", version)
	invoiceRequireCode(t, err, "INVALID_STATE")
	for _, state := range []struct {
		status            string
		delivered, issued bool
	}{
		{model.InvoiceApprovedForIssue, false, false},
		{model.InvoiceFinanceHold, false, false},
		{model.InvoiceDelivered, true, true},
		{model.InvoiceConfirmed, true, true},
		{model.InvoiceDisputed, true, true},
		{model.InvoiceAwaitingReconfirmation, true, true},
		{model.InvoiceRejected, false, false},
		{model.InvoicePendingReview, true, false},
		{model.InvoicePendingReview, false, true},
	} {
		require.NoError(t, s.DB.Model(&model.InvoiceRequest{}).Where("public_id = ?", request.PublicID).Updates(map[string]any{"status": state.status, "ever_delivered": state.delivered, "issued_evidence": state.issued}).Error)
		_, err = s.Mutate(ctx, user, request.PublicID, "withdraw", "guard-status", version)
		invoiceRequireCode(t, err, "INVALID_STATE")
	}
	var count int64
	require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, s.DB.Model(&model.InvoiceEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInvoiceWithdrawalRace(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var s InvoiceService
			var user InvoiceScope
			if dialect == "sqlite" {
				s, user = invoiceFixture(t)
			} else {
				// Use a fresh local test database for each run. Refuse existing tables.
				port := os.Getenv("KKAI_INVOICE_TEST_" + strings.ToUpper(dialect) + "_PORT")
				if port == "" {
					t.Skip("local test database port is unset")
				}
				_, err := strconv.ParseUint(port, 10, 16)
				require.NoError(t, err)
				var driver gorm.Dialector
				dbType := common.DatabaseTypePostgreSQL
				if dialect == "mysql" {
					driver = mysql.New(mysql.Config{DSN: fmt.Sprintf("invoice_test:invoice_test@tcp(127.0.0.1:%s)/kkai_test_invoice?parseTime=true", port), DefaultStringSize: 191})
					dbType = common.DatabaseTypeMySQL
				} else {
					driver = postgres.Open("host=127.0.0.1 port=" + port + " dbname=kkai_test_invoice user=invoice_test password=invoice_test sslmode=disable")
				}
				db, err := gorm.Open(driver)
				require.NoError(t, err)
				raw, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { _ = raw.Close() })
				tables, err := db.Migrator().GetTables()
				require.NoError(t, err)
				require.Empty(t, tables)
				prior := common.MainDatabaseType()
				common.SetMainDatabaseType(dbType)
				t.Cleanup(func() { common.SetMainDatabaseType(prior) })
				s, user = invoiceFixtureWithDB(t, db)
			}
			ctx := context.Background()
			request, err := s.Create(ctx, user, "race-create", invoiceCreateInput())
			require.NoError(t, err)
			start := make(chan struct{})
			withdrawResult, approveResult := make(chan error, 1), make(chan error, 1)
			version := InvoiceActionInput{Version: request.Version}
			go func() {
				<-start
				_, e := s.Mutate(ctx, user, request.PublicID, "withdraw", "race-withdraw", version)
				withdrawResult <- e
			}()
			go func() {
				<-start
				_, e := s.Mutate(ctx, InvoiceScope{UserID: 2, Admin: true}, request.PublicID, "approve", "race-approve", version)
				approveResult <- e
			}()
			close(start)
			withdrawErr, approveErr := <-withdrawResult, <-approveResult
			current, err := s.Get(ctx, user, request.PublicID)
			require.NoError(t, err)
			var claims int64
			require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&claims).Error)
			if withdrawErr == nil {
				invoiceRequireCode(t, approveErr, "VERSION_CONFLICT")
				assert.Equal(t, model.InvoiceWithdrawn, current.Status)
				assert.Zero(t, claims)
			} else {
				require.NoError(t, approveErr)
				invoiceRequireCode(t, withdrawErr, "VERSION_CONFLICT")
				assert.Equal(t, model.InvoiceApprovedForIssue, current.Status)
				assert.EqualValues(t, 1, claims)
			}
			assert.Equal(t, request.Version+1, current.Version)
			require.Len(t, current.Events, 2)
			var operations int64
			require.NoError(t, s.DB.Model(&model.InvoiceOperation{}).Count(&operations).Error)
			assert.EqualValues(t, 2, operations)
		})
	}
}
