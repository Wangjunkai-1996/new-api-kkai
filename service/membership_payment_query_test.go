package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func membershipEpayQueryFixture() (*model.MembershipOrder, *MembershipEpaySnapshot, map[string]any) {
	return &model.MembershipOrder{
			TradeNo: "MEMBER-query", Provider: model.PaymentProviderEpay,
			PaymentMethod: "wxpay", Currency: "CNY", AmountMinor: 188800, CreatedAt: 1699999940,
		}, &MembershipEpaySnapshot{
			MerchantID: "123", APIKey: "original-merchant-key", PaymentMethod: "wxpay",
		}, map[string]any{
			"code": 1, "status": 1, "pid": 123,
			"out_trade_no": "MEMBER-query", "trade_no": "provider-receipt",
			"money": "1888.00", "currency": "CNY", "type": "wxpay",
			"endtime": "2023-11-15 06:13:20",
		}
}

func TestQueryMembershipEpayEvidence(t *testing.T) {
	tests := []struct {
		name            string
		fields          map[string]any
		quoted          bool
		objectExtension string
		trailing        string
		wantErr         error
		wantUnpaid      bool
	}{
		{name: "paid"},
		{name: "merchant payment method alias", fields: map[string]any{"type": "wxpay2"}},
		{name: "one quoted JSON layer", quoted: true},
		{name: "unpaid", fields: map[string]any{"status": 0}, wantUnpaid: true},
		{name: "provider temporarily unavailable", fields: map[string]any{"code": 0}, wantErr: ErrMembershipPaymentUnavailable},
		{name: "wrong merchant", fields: map[string]any{"pid": 456}, wantErr: ErrMembershipPaymentEvidence},
		{name: "wrong order", fields: map[string]any{"out_trade_no": "MEMBER-other"}, wantErr: ErrMembershipPaymentEvidence},
		{name: "missing merchant identity", fields: map[string]any{"pid": nil}, wantErr: ErrMembershipPaymentUnavailable},
		{name: "missing order identity", fields: map[string]any{"out_trade_no": ""}, wantErr: ErrMembershipPaymentUnavailable},
		{name: "duplicate escaped merchant key", objectExtension: `,"\u0070id":123`, wantErr: ErrMembershipPaymentUnavailable},
		{name: "duplicate escaped nested key", objectExtension: `,"extra":{"value":1,"\u0076alue":1}`, wantErr: ErrMembershipPaymentUnavailable},
		{name: "trailing JSON", trailing: " {}", wantErr: ErrMembershipPaymentUnavailable},
		{name: "unknown payment status", fields: map[string]any{"status": 2}, wantErr: ErrMembershipPaymentUnavailable},
		{name: "invalid amount", fields: map[string]any{"money": "1e3"}, wantErr: ErrMembershipPaymentUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, snapshot, payload := membershipEpayQueryFixture()
			for key, value := range tt.fields {
				payload[key] = value
			}
			body, err := common.Marshal(payload)
			require.NoError(t, err)
			if tt.objectExtension != "" {
				body = []byte(strings.TrimSuffix(string(body), "}") + tt.objectExtension + "}")
			}
			body = append(body, tt.trailing...)
			if tt.quoted {
				body, err = common.Marshal(string(body))
				require.NoError(t, err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/merchant/api.php", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				assert.Equal(t, "order", r.URL.Query().Get("act"))
				assert.Equal(t, snapshot.MerchantID, r.URL.Query().Get("pid"))
				assert.Equal(t, snapshot.APIKey, r.URL.Query().Get("key"))
				assert.Equal(t, order.TradeNo, r.URL.Query().Get("out_trade_no"))
				w.Header().Set("Content-Type", "application/json")
				_, writeErr := w.Write(body)
				assert.NoError(t, writeErr)
			}))
			defer server.Close()
			snapshot.APIURL = server.URL + "/merchant"

			got, err := queryMembershipEpay(context.Background(), server.Client(), order, snapshot)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.NotContains(t, err.Error(), snapshot.APIKey)
				assert.Equal(t, membershipEpayQuery{}, got)
				return
			}
			require.NoError(t, err)
			if tt.wantUnpaid {
				assert.Equal(t, membershipEpayQuery{}, got)
				return
			}
			assert.Equal(t, membershipEpayQuery{
				Paid: true, ReceiptID: "provider-receipt", AmountMinor: 188800,
				Currency: "CNY", ProviderPaidAt: 1700000000,
			}, got)
		})
	}
}

func TestQueryMembershipEpayPaidEvidenceRequiringReview(t *testing.T) {
	tests := []struct {
		name            string
		fields          map[string]any
		wantValidation  string
		wantAmount      int64
		wantCurrency    string
		wantTimeMissing bool
	}{
		{name: "amount mismatch", fields: map[string]any{"money": "1887.00"}, wantAmount: 188700, wantValidation: "payment amount or currency mismatch"},
		{name: "currency mismatch", fields: map[string]any{"currency": "USD"}, wantCurrency: "USD", wantValidation: "payment amount or currency mismatch"},
		{name: "refund money", fields: map[string]any{"refund_money": "1.00"}, wantValidation: "provider refund signal requires review"},
		{name: "refund amount", fields: map[string]any{"refund_amount": "1888.00"}, wantValidation: "provider refund signal requires review"},
		{name: "refund status", fields: map[string]any{"refund_status": 1}, wantValidation: "provider refund signal requires review"},
		{name: "invalid refund evidence", fields: map[string]any{"refund_money": "unknown"}, wantValidation: "provider refund signal requires review"},
		{name: "no refund", fields: map[string]any{"refund_money": "0.00", "refund_amount": nil, "refund_status": 0}},
		{name: "missing payment time", fields: map[string]any{"endtime": ""}, wantTimeMissing: true, wantValidation: "provider payment time unavailable"},
		{name: "invalid payment time", fields: map[string]any{"endtime": "yesterday"}, wantTimeMissing: true, wantValidation: "provider payment time unavailable"},
		{name: "payment predates order", fields: map[string]any{"endtime": "2023-11-15 06:12:19"}, wantTimeMissing: true, wantValidation: "provider payment time unavailable"},
		{name: "future payment time", fields: map[string]any{"endtime": time.Now().Add(24 * time.Hour).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")}, wantTimeMissing: true, wantValidation: "provider payment time unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, snapshot, payload := membershipEpayQueryFixture()
			for key, value := range tt.fields {
				payload[key] = value
			}
			body, err := common.Marshal(payload)
			require.NoError(t, err)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, writeErr := w.Write(body)
				assert.NoError(t, writeErr)
			}))
			defer server.Close()
			snapshot.APIURL = server.URL

			got, err := queryMembershipEpay(context.Background(), server.Client(), order, snapshot)
			require.NoError(t, err)
			want := membershipEpayQuery{
				Paid: true, ReceiptID: "provider-receipt", AmountMinor: 188800,
				Currency: "CNY", ProviderPaidAt: 1700000000, ValidationError: tt.wantValidation,
			}
			if tt.wantAmount != 0 {
				want.AmountMinor = tt.wantAmount
			}
			if tt.wantCurrency != "" {
				want.Currency = tt.wantCurrency
			}
			if tt.wantTimeMissing {
				want.ProviderPaidAt = 0
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestQueryMembershipEpayDoesNotFollowRedirects(t *testing.T) {
	order, snapshot, _ := membershipEpayQueryFixture()
	var destinationRequests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationRequests.Add(1)
	}))
	defer destination.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"?"+r.URL.RawQuery, http.StatusFound)
	}))
	defer server.Close()
	snapshot.APIURL = server.URL

	got, err := queryMembershipEpay(context.Background(), server.Client(), order, snapshot)
	require.ErrorIs(t, err, ErrMembershipPaymentUnavailable)
	assert.NotContains(t, err.Error(), snapshot.APIKey)
	assert.Equal(t, membershipEpayQuery{}, got)
	assert.Zero(t, destinationRequests.Load(), "redirect must not forward merchant credentials")
}

func TestQueryMembershipEpayResponseLimit(t *testing.T) {
	for _, size := range []int{64 << 10, (64 << 10) + 1} {
		name := "exactly 64 KiB"
		if size > 64<<10 {
			name = "over 64 KiB"
		}
		t.Run(name, func(t *testing.T) {
			order, snapshot, payload := membershipEpayQueryFixture()
			body, err := common.Marshal(payload)
			require.NoError(t, err)
			body = append(body, strings.Repeat(" ", size-len(body))...)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, writeErr := w.Write(body)
				assert.NoError(t, writeErr)
			}))
			defer server.Close()
			snapshot.APIURL = server.URL

			got, err := queryMembershipEpay(context.Background(), server.Client(), order, snapshot)
			if size > 64<<10 {
				require.ErrorIs(t, err, ErrMembershipPaymentUnavailable)
				assert.Equal(t, membershipEpayQuery{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, membershipEpayQuery{
				Paid: true, ReceiptID: "provider-receipt", AmountMinor: 188800,
				Currency: "CNY", ProviderPaidAt: 1700000000,
			}, got)
		})
	}
}

func TestQueryMembershipEpayCanceledContext(t *testing.T) {
	order, snapshot, _ := membershipEpayQueryFixture()
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	snapshot.APIURL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := queryMembershipEpay(ctx, server.Client(), order, snapshot)
	require.ErrorIs(t, err, ErrMembershipPaymentUnavailable)
	assert.NotContains(t, err.Error(), snapshot.APIKey)
	assert.Equal(t, membershipEpayQuery{}, got)
	assert.Zero(t, requests.Load())
}
