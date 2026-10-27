package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMembershipAPIErrorHidesInternalDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/membership/orders", nil)

	membershipAPIError(c, errors.New("sql password=super-secret"))

	require.Equal(t, 500, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret")
	require.Contains(t, recorder.Body.String(), `"code":"membership_internal_error"`)
}

func TestMembershipOrderResponseUsesExplicitAudienceDTO(t *testing.T) {
	terms, err := common.Marshal(model.MembershipOrderTerms{
		Tier:   model.MembershipTier{ID: "gold", Name: "Gold", Quota: 1000},
		Action: "purchase",
	})
	require.NoError(t, err)
	resolution, err := common.Marshal(map[string]any{
		"actor_id": 99, "operation_id": "internal-op", "reason": "verified by finance",
	})
	require.NoError(t, err)
	order := &model.MembershipOrder{
		ID: 7, TradeNo: "MEMBER-test", UserID: 42, TierID: "gold", AmountMinor: 9900,
		Currency: "USD", Status: model.MembershipOrderException, Terms: string(terms),
		Resolution: string(resolution), ExceptionReason: "provider mismatch",
		Payments: []model.MembershipPaymentReceipt{{ReceiptID: "receipt-1", Status: model.MembershipOrderException, Reason: "amount mismatch", AmountMinor: 9900, Currency: "USD"}},
	}

	public, err := membershipOrderResponse(order, false)
	require.NoError(t, err)
	publicJSON, err := common.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(publicJSON), "internal-op")
	require.NotContains(t, string(publicJSON), "provider mismatch")
	require.NotContains(t, string(publicJSON), "receipt-1")

	admin, err := membershipOrderResponse(order, true)
	require.NoError(t, err)
	require.Equal(t, 42, admin.UserID)
	require.Equal(t, "verified by finance", admin.ResolutionNote)
	require.Len(t, admin.Payments, 1)
	adminJSON := string(mustMarshalMembershipOrderView(t, admin))
	require.NotContains(t, adminJSON, `"actor_id"`)
	require.NotContains(t, adminJSON, "internal-op")
}

func TestValidateMembershipCheckoutRequiresHTTPS(t *testing.T) {
	for _, test := range []struct {
		name     string
		checkout *membershipCheckout
		invalid  bool
	}{
		{name: "secure payment", checkout: &membershipCheckout{PaymentURL: "https://checkout.example.test/pay"}},
		{name: "secure form", checkout: &membershipCheckout{SubmitURL: "https://pay.example.test/submit", FormFields: map[string]string{"amount": "1.00"}}},
		{name: "http", checkout: &membershipCheckout{PaymentURL: "http://checkout.example.test/pay"}, invalid: true},
		{name: "relative", checkout: &membershipCheckout{PaymentURL: "/pay"}, invalid: true},
		{name: "missing url", checkout: &membershipCheckout{}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateMembershipCheckout(test.checkout)
			if test.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mustMarshalMembershipOrderView(t *testing.T, view *membershipOrderView) []byte {
	t.Helper()
	payload, err := common.Marshal(view)
	require.NoError(t, err)
	return payload
}
