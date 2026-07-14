package service

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const riskStreamTestSecret = "0123456789abcdef0123456789abcdef"

type fakeRiskStreamStore struct {
	reclaimed  []RiskStreamMessage
	new        []RiskStreamMessage
	acked      []string
	dead       []RiskStreamMessage
	deadReason []string
	ensureErr  error
	readErr    error
	claimErr   error
	ackErr     error
	rejectErr  error
}

func (s *fakeRiskStreamStore) EnsureGroup(context.Context) error { return s.ensureErr }
func (s *fakeRiskStreamStore) ReadNew(context.Context, string, int64, time.Duration) ([]RiskStreamMessage, error) {
	return s.new, s.readErr
}
func (s *fakeRiskStreamStore) ClaimPending(context.Context, string, int64, time.Duration) ([]RiskStreamMessage, error) {
	return s.reclaimed, s.claimErr
}
func (s *fakeRiskStreamStore) Ack(_ context.Context, ids ...string) error {
	s.acked = append(s.acked, ids...)
	return s.ackErr
}
func (s *fakeRiskStreamStore) Reject(_ context.Context, message RiskStreamMessage, reason string) error {
	if s.rejectErr != nil {
		return s.rejectErr
	}
	s.dead = append(s.dead, message)
	s.deadReason = append(s.deadReason, reason)
	s.acked = append(s.acked, message.ID)
	return nil
}

type fakeRiskActionApplier struct {
	inputs []RiskActionInput
	result *RiskActionResult
	err    error
}

func (a *fakeRiskActionApplier) Apply(_ context.Context, input RiskActionInput) (*RiskActionResult, error) {
	a.inputs = append(a.inputs, input)
	if a.result == nil {
		a.result = &RiskActionResult{}
	}
	return a.result, a.err
}

func validRiskStreamEvent(now time.Time) RiskStreamEvent {
	return RiskStreamEvent{
		EventID:                "edge-event-0001",
		Source:                 RiskSourceEdgeGuard,
		OccurredAt:             now.Unix(),
		Nonce:                  "0123456789abcdef0123456789abcdef",
		RequestID:              "request-1",
		UserID:                 10,
		TokenID:                11,
		ChannelID:              12,
		ModelName:              "gpt-test",
		RuleVersion:            "rules-v1",
		EvidenceSHA256:         RiskFingerprint("evidence"),
		TokenFingerprint:       RiskFingerprint("token"),
		UpstreamKeyFingerprint: RiskFingerprint("upstream"),
		Recommendation:         RiskDecisionDisable,
		Metadata: map[string]any{
			"case_id":        "edge-case-1",
			"evidence_level": "confirmed",
		},
	}
}

func signedRiskStreamMessage(t *testing.T, id string, event RiskStreamEvent) RiskStreamMessage {
	t.Helper()
	payload, signature, err := SignRiskStreamEvent(event, riskStreamTestSecret)
	require.NoError(t, err)
	return RiskStreamMessage{ID: id, Payload: payload, Signature: signature}
}

func riskStreamTestDecider(event RiskStreamEvent) (string, RiskDurableActions, error) {
	return event.Recommendation, RiskDurableActions{
		DisableToken: event.Recommendation == RiskDecisionDisable,
		DisableUser:  event.Recommendation == RiskDecisionDisable,
	}, nil
}

func newRiskStreamTestConsumer(t *testing.T, store RiskStreamStore, applier RiskActionApplier, now time.Time) *RiskStreamConsumer {
	t.Helper()
	consumer, err := NewRiskStreamConsumer(store, applier, riskStreamTestDecider, riskStreamTestSecret, "consumer-a")
	require.NoError(t, err)
	consumer.now = func() time.Time { return now }
	consumer.readBlock = 0
	return consumer
}

func TestRiskStreamConsumerProcessesReclaimedBeforeNewMessages(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	store := &fakeRiskStreamStore{
		reclaimed: []RiskStreamMessage{signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))},
		new: []RiskStreamMessage{signedRiskStreamMessage(t, "2-0", func() RiskStreamEvent {
			event := validRiskStreamEvent(now)
			event.EventID = "edge-event-0002"
			return event
		}())},
	}
	applier := &fakeRiskActionApplier{}
	consumer := newRiskStreamTestConsumer(t, store, applier, now)

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, result.Read)
	require.Equal(t, 2, result.Applied)
	require.Equal(t, []string{"1-0", "2-0"}, store.acked)
	require.Len(t, applier.inputs, 2)
	require.True(t, applier.inputs[0].Actions.DisableToken)
	require.True(t, applier.inputs[0].Actions.DisableUser)
}

func TestRiskStreamConsumerAcknowledgesIdempotentReplay(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	store := &fakeRiskStreamStore{new: []RiskStreamMessage{signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))}}
	applier := &fakeRiskActionApplier{result: &RiskActionResult{Replayed: true}}
	consumer := newRiskStreamTestConsumer(t, store, applier, now)

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Replayed)
	require.Equal(t, []string{"1-0"}, store.acked)
}

func TestRiskStreamConsumerRejectsInvalidSignature(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	message := signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))
	message.Signature = "bad"
	store := &fakeRiskStreamStore{new: []RiskStreamMessage{message}}
	applier := &fakeRiskActionApplier{}
	consumer := newRiskStreamTestConsumer(t, store, applier, now)

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.DeadLettered)
	require.Equal(t, []string{"1-0"}, store.acked)
	require.Len(t, store.dead, 1)
	require.Empty(t, applier.inputs)
}

func TestRiskStreamConsumerRejectsIdempotencyConflict(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	store := &fakeRiskStreamStore{new: []RiskStreamMessage{signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))}}
	applier := &fakeRiskActionApplier{err: ErrRiskActionIdempotencyConflict}
	consumer := newRiskStreamTestConsumer(t, store, applier, now)

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.DeadLettered)
	require.Equal(t, []string{"1-0"}, store.acked)
	require.Len(t, store.dead, 1)
}

func TestRiskStreamConsumerLeavesTransientDatabaseFailurePending(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	store := &fakeRiskStreamStore{new: []RiskStreamMessage{signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))}}
	applier := &fakeRiskActionApplier{err: errors.New("database unavailable")}
	consumer := newRiskStreamTestConsumer(t, store, applier, now)

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	require.Empty(t, store.acked)
	require.Empty(t, store.dead)
}

func TestRiskStreamConsumerLeavesTransientDecisionFailurePending(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	store := &fakeRiskStreamStore{new: []RiskStreamMessage{signedRiskStreamMessage(t, "1-0", validRiskStreamEvent(now))}}
	decider := func(RiskStreamEvent) (string, RiskDurableActions, error) {
		return "", RiskDurableActions{}, errors.New("policy database unavailable")
	}
	consumer, err := NewRiskStreamConsumer(store, &fakeRiskActionApplier{}, decider, riskStreamTestSecret, "consumer-a")
	require.NoError(t, err)
	consumer.now = func() time.Time { return now }
	consumer.readBlock = 0

	result, err := consumer.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	require.Empty(t, store.acked)
	require.Empty(t, store.dead)
}

func TestVerifyRiskStreamMessageRejectsTamperingAndStaleTimestamp(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	event := validRiskStreamEvent(now)
	payload, signature, err := SignRiskStreamEvent(event, riskStreamTestSecret)
	require.NoError(t, err)

	_, err = VerifyRiskStreamMessage(payload+" ", signature, riskStreamTestSecret, now, time.Hour, time.Minute)
	require.ErrorIs(t, err, ErrRiskStreamInvalidSignature)

	event.OccurredAt = now.Add(-2 * time.Hour).Unix()
	payload, signature, err = SignRiskStreamEvent(event, riskStreamTestSecret)
	require.NoError(t, err)
	_, err = VerifyRiskStreamMessage(payload, signature, riskStreamTestSecret, now, time.Hour, time.Minute)
	require.ErrorIs(t, err, ErrRiskStreamStaleEvent)
}

func TestVerifyRiskStreamMessageRejectsUnknownFields(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	payload, _, err := SignRiskStreamEvent(validRiskStreamEvent(now), riskStreamTestSecret)
	require.NoError(t, err)
	payload = strings.TrimSuffix(payload, "}") + `,"raw_token":"must-not-be-accepted"}`
	signature := hex.EncodeToString(riskStreamMAC([]byte(payload), []byte(riskStreamTestSecret)))

	_, err = VerifyRiskStreamMessage(payload, signature, riskStreamTestSecret, now, time.Hour, time.Minute)
	require.ErrorIs(t, err, ErrRiskStreamInvalidEvent)
}

func TestVerifyRiskStreamMessageRejectsOversizedPayload(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	payload := strings.Repeat("x", maxRiskStreamPayloadBytes+1)
	signature := hex.EncodeToString(riskStreamMAC([]byte(payload), []byte(riskStreamTestSecret)))

	_, err := VerifyRiskStreamMessage(payload, signature, riskStreamTestSecret, now, time.Hour, time.Minute)
	require.ErrorIs(t, err, ErrRiskStreamInvalidEvent)
}

func TestNewRiskStreamConsumerRequiresStrongSecret(t *testing.T) {
	_, err := NewRiskStreamConsumer(&fakeRiskStreamStore{}, &fakeRiskActionApplier{}, riskStreamTestDecider, "short", "consumer")
	require.ErrorIs(t, err, ErrRiskStreamNoSecret)
}
