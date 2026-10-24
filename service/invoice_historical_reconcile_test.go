package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceHistoricalEpayReconciliationMakesSixPaymentsSelectable(t *testing.T) {
	s, scope := invoiceFixture(t)
	require.NoError(t, s.DB.Where("top_up_id = ?", 1).Delete(&model.InvoicePaymentReceipt{}).Error)
	require.NoError(t, s.DB.Where("id = ?", 1).Delete(&model.TopUp{}).Error)
	require.NoError(t, s.DB.Exec("INSERT INTO users (id,tenant_id,username,email) VALUES (140,0,'historical-customer','historical@example.com')").Error)
	scope.UserID = 140
	ctx := context.Background()
	payments := []struct {
		id          int
		orderNo     string
		amount      int64
		paidMinor   int64
		createdAt   int64
		missingTime bool
	}{
		{7172, "USR140NOJj25jW1789467785", 1000, 7275, 1789467785, false},
		{6564, "USR140NOMPk79G1789013139", 1000, 7275, 1789013139, false},
		{5409, "USR140NOvcu24b1788148302", 500, 3750, 1788148302, false},
		{1243, "USR140NOITOgr61784466147", 500, 3750, 1784466147, false},
		{410, "USR140NOKRrUlq1779616102", 500, 3750, 1779616102, true},
		{48, "USR297NO5s5f8L1778816336", 200, 1000, 1778816336, true},
	}
	refs := make([]InvoiceOrderRef, 0, len(payments))
	expected := make(map[string]int64, len(payments))
	for i, payment := range payments {
		method := "alipay"
		if i == len(payments)-1 {
			method = "wxpay"
		}
		order := model.TopUp{Id: payment.id, UserId: scope.UserID, TradeNo: payment.orderNo, Amount: payment.amount, Money: 999, PaymentMethod: method, PaymentProvider: model.PaymentProviderEpay, CreateTime: payment.createdAt, CompleteTime: payment.createdAt + 60, Status: common.TopUpStatusSuccess}
		if payment.missingTime {
			order.CompleteTime = 0
		}
		require.NoError(t, s.DB.Create(&order).Error)
		refs = append(refs, InvoiceOrderRef{SourceType: "topup", OrderID: payment.orderNo})
		expected[payment.orderNo] = payment.paidMinor
	}
	unavailable, err := s.Candidates(ctx, scope, InvoiceFilter{Eligibility: "unavailable"})
	require.NoError(t, err)
	require.Len(t, unavailable.Items, 6)
	for _, candidate := range unavailable.Items {
		assert.False(t, candidate.Eligible)
		assert.NotContains(t, candidate.AllowedActions, "select")
		assert.Nil(t, candidate.AmountMinor, "legacy Money and credited Amount are not authenticated cash receipts")
	}
	for _, payment := range payments {
		receipt := model.TopUpPaymentReceipt{ProviderReceiptID: "test-provider-receipt:" + payment.orderNo, AmountMinor: payment.paidMinor, Currency: "CNY"}
		require.NoError(t, model.ReconcileInvoiceTopUpPayment(ctx, s.DB, scope.TenantID, scope.UserID, payment.orderNo, receipt, payment.createdAt+60))
	}
	available, err := s.Candidates(ctx, scope, InvoiceFilter{})
	require.NoError(t, err)
	require.Len(t, available.Items, 6)
	var total int64
	for _, candidate := range available.Items {
		assert.True(t, candidate.Eligible, candidate.OrderID)
		assert.Contains(t, candidate.AllowedActions, "select", candidate.OrderID)
		require.NotNil(t, candidate.AmountMinor, candidate.OrderID)
		assert.Equal(t, strconv.FormatInt(expected[candidate.OrderID], 10), *candidate.AmountMinor, candidate.OrderID)
		amount, err := strconv.ParseInt(*candidate.AmountMinor, 10, 64)
		require.NoError(t, err)
		total += amount
	}
	assert.EqualValues(t, 26800, total, "only independently evidenced cash is invoiceable, regardless of recharge multiplier")
	for _, payment := range payments {
		receipt := model.TopUpPaymentReceipt{ProviderReceiptID: "test-provider-receipt:" + payment.orderNo, AmountMinor: payment.paidMinor, Currency: "CNY"}
		require.NoError(t, model.ReconcileInvoiceTopUpPayment(ctx, s.DB, scope.TenantID, scope.UserID, payment.orderNo, receipt, payment.createdAt+60))
	}
	var count int64
	require.NoError(t, s.DB.Model(&model.InvoicePaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 6, count, "replaying the same payment evidence must not duplicate receipts")
	quote, err := s.Quote(ctx, scope, InvoiceQuoteInput{Items: refs, InvoiceType: "ordinary", PolicyVersion: "test-v1"})
	require.NoError(t, err)
	assert.True(t, quote.Eligible)
	assert.Equal(t, "26800", quote.AmountMinor)
	assert.Equal(t, "CNY", quote.Currency)
	assert.Len(t, quote.Items, 6)
}
