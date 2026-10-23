package service

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceOrderAmountHistoricalDisplayDoesNotAuthorizeInvoice(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	for _, order := range []model.TopUp{
		{UserId: user.UserID, TradeNo: "historical-float", Money: 19.995, Currency: "", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: now},
		{UserId: user.UserID, TradeNo: "historical-exact", PaidMinor: 1234, Money: 999, Currency: "USD", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: now},
	} {
		require.NoError(t, s.DB.Create(&order).Error)
	}
	page, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "unavailable"})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	byOrder := map[string]InvoiceCandidate{}
	for _, candidate := range page.Items {
		byOrder[candidate.OrderID] = candidate
		assert.Nil(t, candidate.AmountMinor)
		assert.False(t, candidate.Eligible)
		assert.Contains(t, candidate.ReasonCodes, "PAYMENT_UNVERIFIED")
	}
	require.NotNil(t, byOrder["historical-float"].OrderAmountMinor)
	assert.Equal(t, "2000", *byOrder["historical-float"].OrderAmountMinor)
	assert.Empty(t, byOrder["historical-float"].OrderCurrency)
	require.NotNil(t, byOrder["historical-exact"].OrderAmountMinor)
	assert.Equal(t, "1234", *byOrder["historical-exact"].OrderAmountMinor)
	assert.Equal(t, "USD", byOrder["historical-exact"].OrderCurrency)

	encoded, err := common.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"order_amount_minor":"2000"`)
	assert.Contains(t, string(encoded), `"order_currency":"USD"`)
	assert.Contains(t, string(encoded), `"amount_minor":null`)
	summary, err := s.Summary(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.EligibleCount)
	require.Len(t, summary.Totals, 1)
	assert.Equal(t, "50000", summary.Totals[0].AmountMinor)
	input := invoiceCreateInput()
	input.Items = []InvoiceOrderRef{{SourceType: "topup", OrderID: "historical-float"}, {SourceType: "topup", OrderID: "historical-exact"}}
	quote, err := s.Quote(ctx, user, input.InvoiceQuoteInput)
	require.NoError(t, err)
	assert.False(t, quote.Eligible)
	assert.Equal(t, "0", quote.AmountMinor)
	_, err = s.Create(ctx, user, "historical-display-create", input)
	invoiceRequireCode(t, err, "PAYMENT_CHANGED")
}

func TestInvoiceOrderAmountMembershipAndLegacyKeepOriginalCurrency(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	member := model.MembershipOrder{TradeNo: "member-original-price", UserID: user.UserID, AmountMinor: 69900, Currency: "USD", Provider: "epay", ActualAmountMinor: 100, ActualCurrency: "CNY", ActualProvider: "epay", Status: model.MembershipOrderException, PaidAt: now}
	require.NoError(t, s.DB.Create(&member).Error)
	legacy := model.SubscriptionOrder{TradeNo: "legacy-original-price", UserId: user.UserID, Money: 88.88, PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: now}
	require.NoError(t, s.DB.Create(&legacy).Error)
	page, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "unavailable"})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	byOrder := map[string]InvoiceCandidate{}
	for _, candidate := range page.Items {
		byOrder[candidate.OrderID] = candidate
		assert.Nil(t, candidate.AmountMinor)
		assert.False(t, candidate.Eligible)
	}
	require.NotNil(t, byOrder[member.TradeNo].OrderAmountMinor)
	assert.Equal(t, "69900", *byOrder[member.TradeNo].OrderAmountMinor)
	assert.Equal(t, "USD", byOrder[member.TradeNo].OrderCurrency)
	require.NotNil(t, byOrder[legacy.TradeNo].OrderAmountMinor)
	assert.Equal(t, "8888", *byOrder[legacy.TradeNo].OrderAmountMinor)
	assert.Empty(t, byOrder[legacy.TradeNo].OrderCurrency, "legacy subscription orders contain no immutable currency field")
}

func TestInvoiceOrderAmountInvalidLegacyFloatsRemainUnavailable(t *testing.T) {
	s, user := invoiceFixture(t)
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -1} {
		t.Run(strconv.FormatFloat(value, 'g', -1, 64), func(t *testing.T) {
			snapshot := &invoicePaymentSnapshot{TopUps: []model.TopUp{{Id: 99, UserId: user.UserID, TradeNo: "invalid-money", Money: value, Status: common.TopUpStatusSuccess, CompleteTime: time.Now().Unix()}}}
			candidates, err := s.candidateRowsFor(context.Background(), user, s.DB, false, nil, false, snapshot)
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			assert.Nil(t, candidates[0].OrderAmountMinor)
			assert.Nil(t, candidates[0].AmountMinor)
			assert.False(t, candidates[0].Eligible)
			_, err = common.Marshal(candidates)
			require.NoError(t, err, "invalid source floats must not panic or enter API JSON")
		})
	}
}
