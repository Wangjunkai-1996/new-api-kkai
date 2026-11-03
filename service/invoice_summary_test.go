package service

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceSummaryInvoicedTotalsCountDeliveredRequestsOnce(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()

	zero, err := s.Summary(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.Equal(t, []InvoiceMoneyTotal{{Currency: "CNY", CurrencyExponent: 2, AmountMinor: "0"}}, zero.InvoicedTotals)

	created, err := s.Create(ctx, user, "summary-create", invoiceCreateInput())
	require.NoError(t, err)
	request := s.DB.Model(&model.InvoiceRequest{}).Where("public_id = ?", created.PublicID)
	for _, state := range []struct {
		status    string
		issued    bool
		delivered bool
		version   int64
		want      string
		wantCount int64
	}{
		{model.InvoicePendingReview, false, false, 0, "0", 0},
		{model.InvoiceApprovedForIssue, true, false, 0, "0", 0},
		{model.InvoiceDelivered, true, true, 1, "50000", 1},
		{model.InvoiceDisputed, true, true, 1, "50000", 1},
		{model.InvoiceFinanceHold, true, true, 2, "50000", 1},
	} {
		require.NoError(t, request.Updates(map[string]any{
			"status":                   state.status,
			"issued_evidence":          state.issued,
			"ever_delivered":           state.delivered,
			"current_delivery_version": state.version,
		}).Error)
		summary, err := s.Summary(ctx, user, InvoiceFilter{})
		require.NoError(t, err)
		assert.Equal(t, state.want, summary.InvoicedTotals[0].AmountMinor, state.status)
		assert.Equal(t, state.wantCount, summary.InvoicedTotals[0].Count, state.status)
	}

	for _, row := range []model.InvoiceRequest{
		{PublicID: "summary-own-cny-zero", TenantID: user.TenantID, UserID: user.UserID, Status: model.InvoiceConfirmed, EverDelivered: true, AmountMinor: 7, Currency: "CNY", CurrencyExponent: 0},
		{PublicID: "summary-own-cny", TenantID: user.TenantID, UserID: user.UserID, Status: model.InvoiceConfirmed, EverDelivered: true, AmountMinor: 2000, Currency: "CNY", CurrencyExponent: 2},
		{PublicID: "summary-own-usd", TenantID: user.TenantID, UserID: user.UserID, Status: model.InvoiceConfirmed, EverDelivered: true, AmountMinor: 345, Currency: "USD", CurrencyExponent: 2},
		{PublicID: "summary-other-user", TenantID: user.TenantID, UserID: 2, Status: model.InvoiceConfirmed, EverDelivered: true, AmountMinor: 99000, Currency: "CNY", CurrencyExponent: 2},
		{PublicID: "summary-other-tenant", TenantID: 9, UserID: user.UserID, Status: model.InvoiceConfirmed, EverDelivered: true, AmountMinor: 88000, Currency: "CNY", CurrencyExponent: 2},
	} {
		require.NoError(t, s.DB.Create(&row).Error)
	}
	summary, err := s.Summary(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.Equal(t, []InvoiceMoneyTotal{
		{Currency: "CNY", CurrencyExponent: 0, AmountMinor: "7", Count: 1},
		{Currency: "CNY", CurrencyExponent: 2, AmountMinor: "52000", Count: 2},
		{Currency: "USD", CurrencyExponent: 2, AmountMinor: "345", Count: 1},
	}, summary.InvoicedTotals)
	encoded, err := common.Marshal(summary)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"invoiced_totals":`)

	other, err := s.Summary(ctx, InvoiceScope{TenantID: 0, UserID: 2}, InvoiceFilter{})
	require.NoError(t, err)
	assert.Equal(t, []InvoiceMoneyTotal{{Currency: "CNY", CurrencyExponent: 2, AmountMinor: "99000", Count: 1}}, other.InvoicedTotals)
	admin, err := s.Summary(ctx, InvoiceScope{TenantID: 0, UserID: 2, Admin: true}, InvoiceFilter{})
	require.NoError(t, err)
	assert.Empty(t, admin.InvoicedTotals)
	adminJSON, err := common.Marshal(admin)
	require.NoError(t, err)
	assert.NotContains(t, string(adminJSON), `"invoiced_totals"`)
}

func TestInvoiceSummaryInvoicedTotalsRejectOverflow(t *testing.T) {
	s, user := invoiceFixture(t)
	for _, row := range []model.InvoiceRequest{
		{PublicID: "summary-overflow-one", TenantID: user.TenantID, UserID: user.UserID, Status: model.InvoiceDelivered, EverDelivered: true, AmountMinor: math.MaxInt64, Currency: "CNY", CurrencyExponent: 2},
		{PublicID: "summary-overflow-two", TenantID: user.TenantID, UserID: user.UserID, Status: model.InvoiceDelivered, EverDelivered: true, AmountMinor: 1, Currency: "CNY", CurrencyExponent: 2},
	} {
		require.NoError(t, s.DB.Create(&row).Error)
	}
	_, err := s.Summary(context.Background(), user, InvoiceFilter{})
	invoiceRequireCode(t, err, "INVOICE_METADATA_MISMATCH")
}
