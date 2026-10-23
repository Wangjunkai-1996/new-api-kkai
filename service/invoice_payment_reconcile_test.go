package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceEpayMerchantQueryValidatesEvidenceAndProtectsCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		patch  map[string]any
		quoted bool
	}{
		{name: "valid"},
		{name: "valid quoted", quoted: true},
		{name: "missing merchant", patch: map[string]any{"pid": nil}},
		{name: "wrong merchant", patch: map[string]any{"pid": "456"}},
		{name: "wrong order", patch: map[string]any{"out_trade_no": "someone-else"}},
		{name: "quoted wrong order", patch: map[string]any{"out_trade_no": "someone-else"}, quoted: true},
		{name: "missing transaction", patch: map[string]any{"trade_no": ""}},
		{name: "unpaid", patch: map[string]any{"status": 0}},
		{name: "refunded", patch: map[string]any{"status": 2}},
		{name: "lookup failed", patch: map[string]any{"code": 0, "msg": "secret provider detail"}},
		{name: "unsupported currency", patch: map[string]any{"currency": "USD"}},
		{name: "partial refund", patch: map[string]any{"refund_money": "0.01"}},
		{name: "quoted partial refund", patch: map[string]any{"refund_money": "0.01"}, quoted: true},
		{name: "refund status", patch: map[string]any{"refund_status": 1}},
		{name: "invalid amount", patch: map[string]any{"money": "1.001"}},
		{name: "negative amount", patch: map[string]any{"money": "-1.00"}},
		{name: "exponent amount", patch: map[string]any{"money": "1e2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{"code": 1, "status": 1, "pid": 123, "out_trade_no": "legacy-order", "trade_no": "provider-receipt", "money": "250.01", "type": "wxpay2"}
			for k, v := range tc.patch {
				data[k] = v
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/merchant/api.php", r.URL.Path)
				assert.Equal(t, "order", r.URL.Query().Get("act"))
				assert.Equal(t, "test-secret", r.URL.Query().Get("key"))
				assert.Equal(t, "legacy-order", r.URL.Query().Get("out_trade_no"))
				body, err := common.Marshal(data)
				require.NoError(t, err)
				if tc.quoted {
					body, err = common.Marshal(string(body))
					require.NoError(t, err)
				}
				_, _ = w.Write(body)
			}))
			defer server.Close()
			order := model.TopUp{TradeNo: "legacy-order", CompleteTime: 1_700_000_000}
			receipt, paidAt, err := queryInvoiceEpayPayment(context.Background(), server.Client(), server.URL+"/merchant", "123", "test-secret", order)
			if tc.patch == nil {
				require.NoError(t, err)
				assert.EqualValues(t, 25001, receipt.AmountMinor)
				assert.Equal(t, "CNY", receipt.Currency)
				assert.Equal(t, "provider-receipt", receipt.ProviderReceiptID)
				assert.Equal(t, order.CompleteTime, paidAt)
				return
			}
			invoiceRequireCode(t, err, "PAYMENT_VERIFICATION_FAILED")
			assert.NotContains(t, err.Error(), "test-secret")
			assert.NotContains(t, err.Error(), "secret provider detail")
		})
	}
	t.Run("invalid string payloads are not evidence", func(t *testing.T) {
		for _, body := range []string{`"not JSON"`, `"{broken}"`, `"\"{}\""`, `"null"`, `"[]"`, `"1"`, `"{}" {}`} {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			_, _, err := queryInvoiceEpayPayment(context.Background(), server.Client(), server.URL, "123", "test-secret", model.TopUp{TradeNo: "legacy-order"})
			server.Close()
			invoiceRequireCode(t, err, "PAYMENT_VERIFICATION_UNAVAILABLE")
		}
	})
	t.Run("redirect never receives credentials", func(t *testing.T) {
		calls := 0
		destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
		defer destination.Close()
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusFound)
		}))
		defer server.Close()
		_, _, err := queryInvoiceEpayPayment(context.Background(), server.Client(), server.URL, "123", "test-secret", model.TopUp{TradeNo: "legacy-order"})
		invoiceRequireCode(t, err, "PAYMENT_VERIFICATION_UNAVAILABLE")
		assert.Zero(t, calls)
	})
	t.Run("insecure gateway is rejected before request", func(t *testing.T) {
		_, _, err := queryInvoiceEpayPayment(context.Background(), http.DefaultClient, "http://pay.example.com", "123", "test-secret", model.TopUp{TradeNo: "legacy-order"})
		invoiceRequireCode(t, err, "PAYMENT_VERIFICATION_UNAVAILABLE")
	})
	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 65537))) }))
		defer server.Close()
		_, _, err := queryInvoiceEpayPayment(context.Background(), server.Client(), server.URL, "123", "test-secret", model.TopUp{TradeNo: "legacy-order"})
		invoiceRequireCode(t, err, "PAYMENT_VERIFICATION_UNAVAILABLE")
	})
}

func TestInvoicePaymentReconciliationRestoresEligibilityAndPreservesMinimumAndClaim(t *testing.T) {
	for _, amount := range []int{150, 250} {
		t.Run(fmt.Sprintf("CNY%d", amount), func(t *testing.T) {
			s, scope := invoiceFixture(t)
			require.NoError(t, s.DB.Where("top_up_id = ?", 1).Delete(&model.InvoicePaymentReceipt{}).Error)
			require.NoError(t, s.DB.Model(&model.TopUp{}).Where("id = ?", 1).Updates(map[string]any{"paid_minor": 0, "currency": "", "complete_time": 0, "money": 999}).Error)
			calls := 0
			providerAmount := amount
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				body, err := common.Marshal(fmt.Sprintf(`{"code":1,"status":1,"pid":"123","trade_no":"paid-order","out_trade_no":"payment-a","money":"%d.00","endtime":"2026-01-01 12:00:00"}`, providerAmount))
				require.NoError(t, err)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
			operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = server.URL, "123", "test-secret"
			t.Cleanup(func() {
				operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
			})
			ctx := context.Background()
			input := InvoiceOrderRef{SourceType: "topup", OrderID: "payment-a"}
			unavailable, err := s.Candidates(ctx, scope, InvoiceFilter{Eligibility: "unavailable"})
			require.NoError(t, err)
			require.Len(t, unavailable.Items, 1)
			assert.Contains(t, unavailable.Items[0].AllowedActions, "verify_payment")
			// Scope/ownership/provider checks happen before any request to the gateway.
			_, err = s.Candidates(ctx, InvoiceScope{TenantID: 1, UserID: scope.UserID}, InvoiceFilter{})
			invoiceRequireCode(t, err, "FORBIDDEN")
			err = s.verifyPayment(ctx, InvoiceScope{UserID: 9}, input, server.Client())
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			err = s.verifyPayment(ctx, InvoiceScope{TenantID: 1, UserID: scope.UserID}, input, server.Client())
			invoiceRequireCode(t, err, "FORBIDDEN")
			assert.Zero(t, calls)
			require.NoError(t, s.verifyPayment(ctx, scope, input, server.Client()))
			available, err := s.Candidates(ctx, scope, InvoiceFilter{})
			require.NoError(t, err)
			require.Len(t, available.Items, 1)
			assert.Equal(t, fmt.Sprint(amount*100), *available.Items[0].AmountMinor)
			assert.NotContains(t, available.Items[0].AllowedActions, "verify_payment")
			var proof model.InvoicePaymentReceipt
			require.NoError(t, s.DB.First(&proof).Error)
			assert.Equal(t, time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC).Unix(), proof.PaidAt)
			request := invoiceCreateInput()
			created, err := s.Create(ctx, scope, "reconciled-order-create", request)
			if amount < 200 {
				invoiceRequireCode(t, err, "INVOICE_BELOW_MINIMUM")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "25000", created.AmountMinor)
			require.NoError(t, s.verifyPayment(ctx, scope, input, server.Client()))
			_, err = s.Create(ctx, scope, "reconciled-order-duplicate", request)
			invoiceRequireCode(t, err, "PAYMENT_CHANGED")
			var claims int64
			require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&claims).Error)
			assert.EqualValues(t, 1, claims)
			providerAmount--
			err = s.verifyPayment(ctx, scope, input, server.Client())
			invoiceRequireCode(t, err, "PAYMENT_EXCEPTION")
			var held model.InvoiceRequest
			require.NoError(t, s.DB.Where("public_id = ?", created.PublicID).First(&held).Error)
			assert.Equal(t, model.InvoiceFinanceHold, held.Status)
			require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&claims).Error)
			assert.EqualValues(t, 1, claims)
			callsBeforeRetry := calls
			err = s.verifyPayment(ctx, scope, input, server.Client())
			invoiceRequireCode(t, err, "PAYMENT_EXCEPTION")
			assert.Equal(t, callsBeforeRetry, calls, "existing exceptions require financial review, not another provider replay")
		})
	}
}
