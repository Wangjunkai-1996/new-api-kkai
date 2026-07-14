package service

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	KKAIRiskStreamName       = "kkai:risk:incidents"
	KKAIRiskDeadLetterStream = "kkai:risk:incidents:dead"
	KKAIRiskConsumerGroup    = "newapi-risk-actions"
)

var (
	ErrRiskStreamInvalidEvent     = errors.New("invalid risk stream event")
	ErrRiskStreamInvalidSignature = errors.New("invalid risk stream signature")
	ErrRiskStreamStaleEvent       = errors.New("risk stream event outside accepted time window")
	ErrRiskStreamNoSecret         = errors.New("risk stream secret must be at least 32 bytes")
	ErrRiskStreamDecisionRejected = errors.New("risk stream decision rejected")
	ErrRiskStreamInvalidResult    = errors.New("risk action applier returned no result")
)

type RiskStreamEvent struct {
	EventID                string         `json:"event_id"`
	Source                 string         `json:"source"`
	OccurredAt             int64          `json:"occurred_at"`
	Nonce                  string         `json:"nonce"`
	RequestID              string         `json:"request_id"`
	UserID                 int            `json:"user_id"`
	TokenID                int            `json:"token_id"`
	ChannelID              int            `json:"channel_id"`
	ModelName              string         `json:"model"`
	RuleVersion            string         `json:"rule_version"`
	EvidenceSHA256         string         `json:"evidence_sha256"`
	TokenFingerprint       string         `json:"token_fingerprint,omitempty"`
	UpstreamKeyFingerprint string         `json:"upstream_key_fingerprint,omitempty"`
	Recommendation         string         `json:"recommendation"`
	Metadata               map[string]any `json:"metadata,omitempty"`
}

type RiskStreamMessage struct {
	ID        string
	Payload   string
	Signature string
}

type RiskStreamStore interface {
	EnsureGroup(context.Context) error
	ReadNew(context.Context, string, int64, time.Duration) ([]RiskStreamMessage, error)
	ClaimPending(context.Context, string, int64, time.Duration) ([]RiskStreamMessage, error)
	Ack(context.Context, ...string) error
	Reject(context.Context, RiskStreamMessage, string) error
}

type RiskActionApplier interface {
	Apply(context.Context, RiskActionInput) (*RiskActionResult, error)
}

type RiskEventDecider func(RiskStreamEvent) (string, RiskDurableActions, error)

type RiskStreamConsumer struct {
	store      RiskStreamStore
	applier    RiskActionApplier
	decide     RiskEventDecider
	secret     []byte
	consumerID string
	now        func() time.Time
	maxAge     time.Duration
	maxFuture  time.Duration
	claimIdle  time.Duration
	readBlock  time.Duration
	batchSize  int64
}

type RiskStreamBatchResult struct {
	Read         int
	Applied      int
	Replayed     int
	DeadLettered int
	Pending      int
}

func NewRiskStreamConsumer(
	store RiskStreamStore,
	applier RiskActionApplier,
	decide RiskEventDecider,
	secret string,
	consumerID string,
) (*RiskStreamConsumer, error) {
	if store == nil || applier == nil || decide == nil || strings.TrimSpace(consumerID) == "" {
		return nil, ErrRiskStreamInvalidEvent
	}
	if len(secret) < 32 {
		return nil, ErrRiskStreamNoSecret
	}
	return &RiskStreamConsumer{
		store:      store,
		applier:    applier,
		decide:     decide,
		secret:     []byte(secret),
		consumerID: strings.TrimSpace(consumerID),
		now:        time.Now,
		maxAge:     24 * time.Hour,
		maxFuture:  time.Minute,
		claimIdle:  2 * time.Minute,
		readBlock:  time.Second,
		batchSize:  50,
	}, nil
}

func (c *RiskStreamConsumer) ProcessOnce(ctx context.Context) (*RiskStreamBatchResult, error) {
	if c == nil || c.store == nil || c.applier == nil || c.decide == nil || c.batchSize <= 0 {
		return nil, ErrRiskStreamInvalidEvent
	}
	if err := c.store.EnsureGroup(ctx); err != nil {
		return nil, err
	}

	result := &RiskStreamBatchResult{}
	reclaimed, err := c.store.ClaimPending(ctx, c.consumerID, c.batchSize, c.claimIdle)
	if err != nil {
		return nil, err
	}
	if err := c.processMessages(ctx, reclaimed, result); err != nil {
		return nil, err
	}
	remaining := c.batchSize - int64(len(reclaimed))
	if remaining <= 0 {
		return result, nil
	}
	newMessages, err := c.store.ReadNew(ctx, c.consumerID, remaining, c.readBlock)
	if err != nil {
		return nil, err
	}
	if err := c.processMessages(ctx, newMessages, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *RiskStreamConsumer) processMessages(ctx context.Context, messages []RiskStreamMessage, result *RiskStreamBatchResult) error {
	for _, message := range messages {
		result.Read++
		event, err := VerifyRiskStreamMessage(message.Payload, message.Signature, string(c.secret), c.now(), c.maxAge, c.maxFuture)
		if err != nil {
			if err := c.reject(ctx, message, err.Error()); err != nil {
				return err
			}
			result.DeadLettered++
			continue
		}
		decision, actions, err := c.decide(*event)
		if err != nil {
			if errors.Is(err, ErrRiskStreamDecisionRejected) || errors.Is(err, ErrRiskActionInvalidInput) {
				if err := c.reject(ctx, message, "decision rejected: "+err.Error()); err != nil {
					return err
				}
				result.DeadLettered++
				continue
			}
			result.Pending++
			continue
		}
		applyResult, err := c.applier.Apply(ctx, RiskActionInput{
			EventID:                event.EventID,
			Source:                 event.Source,
			OccurredAt:             event.OccurredAt,
			RequestID:              event.RequestID,
			UserID:                 event.UserID,
			TokenID:                event.TokenID,
			ChannelID:              event.ChannelID,
			ModelName:              event.ModelName,
			RuleVersion:            event.RuleVersion,
			EvidenceSHA256:         event.EvidenceSHA256,
			TokenFingerprint:       event.TokenFingerprint,
			UpstreamKeyFingerprint: event.UpstreamKeyFingerprint,
			Decision:               decision,
			Metadata:               event.Metadata,
			Actions:                actions,
		})
		if err != nil {
			if errors.Is(err, ErrRiskActionIdempotencyConflict) || errors.Is(err, ErrRiskActionInvalidInput) {
				if err := c.reject(ctx, message, err.Error()); err != nil {
					return err
				}
				result.DeadLettered++
				continue
			}
			result.Pending++
			continue
		}
		if applyResult == nil {
			return ErrRiskStreamInvalidResult
		}
		if err := c.store.Ack(ctx, message.ID); err != nil {
			return err
		}
		if applyResult.Replayed {
			result.Replayed++
		} else {
			result.Applied++
		}
	}
	return nil
}

func (c *RiskStreamConsumer) reject(ctx context.Context, message RiskStreamMessage, reason string) error {
	return c.store.Reject(ctx, message, sanitizeKKAIOutboxError(errors.New(reason)))
}
