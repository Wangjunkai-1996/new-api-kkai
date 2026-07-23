package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func topUpRebateOutboxEvent(t *testing.T) model.KKAIOutboxEvent {
	t.Helper()
	payload, err := common.Marshal(model.TopUpCompletedEvent{
		SchemaVersion:   2,
		EventKey:        "newapi:topup:842",
		EventType:       "topup.completed",
		SourceOrderID:   842,
		InviteeID:       3418,
		InviterGroup:    "default",
		CreditedQuota:   37_500_000,
		CompletedAt:     1_784_211_072,
		PaymentProvider: "epay",
	})
	require.NoError(t, err)
	return model.KKAIOutboxEvent{
		EventKey: "newapi:topup:842",
		Topic:    model.KKAIOutboxTopicTopUpCompleted,
		Payload:  string(payload),
	}
}

func TestTopUpRebateOutboxHandlerDeliversTypedIntegerPayload(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+secret, r.Header.Get("Authorization"))
		var payload map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		require.Equal(t, float64(37_500_000), payload["credited_quota"])
		require.NotContains(t, payload, "rebate_base_amount_cents")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	handler := &TopUpRebateOutboxHandler{client: server.Client(), endpoint: server.URL, secret: secret}
	require.NoError(t, handler.Handle(context.Background(), topUpRebateOutboxEvent(t)))
}

func TestTopUpRebateOutboxHandlerAcceptsRedemptionEventIdentity(t *testing.T) {
	event := topUpRebateOutboxEvent(t)
	payload := model.TopUpCompletedEvent{
		SchemaVersion:   2,
		EventKey:        "newapi:redemption:7878",
		EventType:       "topup.completed",
		SourceOrderID:   7878,
		InviteeID:       3120,
		InviterGroup:    "default",
		CreditedQuota:   500_000_000,
		CompletedAt:     1_784_010_626,
		PaymentProvider: model.PaymentProviderRedemption,
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	event.EventKey = payload.EventKey
	event.Payload = string(encoded)
	require.True(t, validTopUpCompletedPayload(event, payload))
}

func TestTopUpRebateOutboxHandlerClassifiesPermanentAndRetryableFailures(t *testing.T) {
	event := topUpRebateOutboxEvent(t)
	for _, testCase := range []struct {
		status    int
		permanent bool
	}{
		{status: http.StatusConflict, permanent: true},
		{status: http.StatusUnprocessableEntity, permanent: true},
		{status: http.StatusUnauthorized, permanent: true},
		{status: http.StatusRequestTimeout, permanent: false},
		{status: http.StatusTooManyRequests, permanent: false},
		{status: http.StatusInternalServerError, permanent: false},
	} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
			}))
			defer server.Close()
			handler := &TopUpRebateOutboxHandler{
				client:   server.Client(),
				endpoint: server.URL,
				secret:   "0123456789abcdef0123456789abcdef",
			}
			err := handler.Handle(context.Background(), event)
			require.Error(t, err)
			var permanent permanentKKAIOutboxError
			require.Equal(t, testCase.permanent, errors.As(err, &permanent))
		})
	}
}

func TestTopUpRebateOutboxHandlerDoesNotFollowRedirects(t *testing.T) {
	event := topUpRebateOutboxEvent(t)
	redirectedRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirectedRequests++
			w.WriteHeader(http.StatusCreated)
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()
	handler := &TopUpRebateOutboxHandler{
		client:   server.Client(),
		endpoint: server.URL,
		secret:   "0123456789abcdef0123456789abcdef",
	}

	err := handler.Handle(context.Background(), event)
	require.Error(t, err)
	var permanent permanentKKAIOutboxError
	require.True(t, errors.As(err, &permanent))
	require.Zero(t, redirectedRequests)
}

func TestTopUpRebateOutboxHandlerRejectsInvalidEventContractBeforeDelivery(t *testing.T) {
	event := topUpRebateOutboxEvent(t)
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverCalls++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	handler := &TopUpRebateOutboxHandler{
		client:   server.Client(),
		endpoint: server.URL,
		secret:   "0123456789abcdef0123456789abcdef",
	}

	for _, payload := range []model.TopUpCompletedEvent{
		{SchemaVersion: 1, EventKey: event.EventKey, EventType: "topup.completed", SourceOrderID: 842, InviteeID: 3418, CreditedQuota: 1, CompletedAt: 1, PaymentProvider: "epay"},
		{SchemaVersion: 2, EventKey: event.EventKey, EventType: "topup.completed", SourceOrderID: 842, InviteeID: 3418, CreditedQuota: 0, CompletedAt: 1, PaymentProvider: "epay"},
		{SchemaVersion: 2, EventKey: event.EventKey, EventType: "topup.completed", SourceOrderID: 842, InviteeID: 3418, CreditedQuota: 1, CompletedAt: 1, PaymentProvider: model.PaymentProviderRedemption},
	} {
		encoded, err := common.Marshal(payload)
		require.NoError(t, err)
		event.Payload = string(encoded)
		err = handler.Handle(context.Background(), event)
		var permanent permanentKKAIOutboxError
		require.ErrorAs(t, err, &permanent)
	}
	require.Zero(t, serverCalls)
}
