package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	RebateEventIngestURLEnv    = "REBATE_EVENT_INGEST_URL"
	RebateEventIngestSecretEnv = "REBATE_EVENT_INGEST_SECRET"
)

var ErrTopUpRebateDeliveryInvalidConfiguration = errors.New("invalid topup rebate delivery configuration")

type TopUpRebateOutboxHandler struct {
	client   *http.Client
	endpoint string
	secret   string
}

func NewTopUpRebateOutboxHandlerFromEnvironment() (*TopUpRebateOutboxHandler, error) {
	endpoint := strings.TrimSpace(os.Getenv(RebateEventIngestURLEnv))
	secret := os.Getenv(RebateEventIngestSecretEnv)
	if endpoint == "" && secret == "" {
		return nil, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrTopUpRebateDeliveryInvalidConfiguration
	}
	if len(secret) < 32 || strings.IndexFunc(secret, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 {
		return nil, ErrTopUpRebateDeliveryInvalidConfiguration
	}
	return &TopUpRebateOutboxHandler{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: endpoint,
		secret:   secret,
	}, nil
}

func (h *TopUpRebateOutboxHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	if h == nil || h.client == nil || h.endpoint == "" || h.secret == "" {
		return ErrTopUpRebateDeliveryInvalidConfiguration
	}
	if event.Topic != model.KKAIOutboxTopicTopUpCompleted {
		return PermanentKKAIOutboxError(errors.New("unexpected topup rebate outbox topic"))
	}
	var payload model.TopUpCompletedEvent
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil || !validTopUpCompletedPayload(event, payload) {
		return PermanentKKAIOutboxError(errors.New("invalid topup completed payload"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewBufferString(event.Payload))
	if err != nil {
		return PermanentKKAIOutboxError(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.secret)
	req.Header.Set("Content-Type", "application/json")
	client := *h.client
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusNoContent {
		return nil
	}
	deliveryErr := fmt.Errorf("rebate event endpoint returned HTTP %d", response.StatusCode)
	if response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError {
		return deliveryErr
	}
	return PermanentKKAIOutboxError(deliveryErr)
}

func validTopUpCompletedPayload(event model.KKAIOutboxEvent, payload model.TopUpCompletedEvent) bool {
	eventKeyPrefix := "topup"
	if payload.PaymentProvider == model.PaymentProviderRedemption {
		eventKeyPrefix = "redemption"
	}
	expectedEventKey := fmt.Sprintf("newapi:%s:%d", eventKeyPrefix, payload.SourceOrderID)
	return payload.SchemaVersion == 2 &&
		payload.EventType == "topup.completed" &&
		payload.EventKey == expectedEventKey &&
		event.EventKey == payload.EventKey &&
		payload.SourceOrderID > 0 &&
		payload.InviteeID > 0 &&
		payload.CreditedQuota > 0 &&
		payload.CompletedAt > 0 &&
		strings.TrimSpace(payload.PaymentProvider) != ""
}
