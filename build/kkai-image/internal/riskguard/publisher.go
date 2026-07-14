package riskguard

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	riskStreamName   = "kkai:risk:incidents"
	riskStreamMaxLen = 100_000
)

type Detection struct {
	RequestID        string
	Model            string
	RuleID           string
	RuleVersion      string
	EvidenceSHA256   string
	TokenFingerprint string
	BodyBytes        int64
}

type event struct {
	EventID                string         `json:"event_id"`
	Source                 string         `json:"source"`
	OccurredAt             int64          `json:"occurred_at"`
	Nonce                  string         `json:"nonce"`
	RequestID              string         `json:"request_id"`
	UserID                 int            `json:"user_id"`
	TokenID                int            `json:"token_id"`
	ChannelID              int            `json:"channel_id"`
	Model                  string         `json:"model"`
	RuleVersion            string         `json:"rule_version"`
	EvidenceSHA256         string         `json:"evidence_sha256"`
	TokenFingerprint       string         `json:"token_fingerprint,omitempty"`
	UpstreamKeyFingerprint string         `json:"upstream_key_fingerprint,omitempty"`
	Recommendation         string         `json:"recommendation"`
	Metadata               map[string]any `json:"metadata"`
}

type Publisher interface {
	Publish(context.Context, Detection) (string, error)
}

type redisStreamClient interface {
	XAdd(context.Context, *redis.XAddArgs) *redis.StringCmd
}

type RedisPublisher struct {
	client redisStreamClient
	secret []byte
	now    func() time.Time
}

func NewRedisPublisher(client redisStreamClient, secret string) (*RedisPublisher, error) {
	if client == nil || len(secret) < 32 {
		return nil, errors.New("invalid risk publisher configuration")
	}
	return &RedisPublisher{client: client, secret: []byte(secret), now: time.Now}, nil
}

func (p *RedisPublisher) Publish(ctx context.Context, detection Detection) (string, error) {
	if p == nil || p.client == nil || detection.RuleID == "" || detection.EvidenceSHA256 == "" {
		return "", errors.New("invalid risk detection")
	}
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	now := p.now()
	eventID := fmt.Sprintf("edge.%d.%s", now.Unix(), nonce)
	payload, err := json.Marshal(event{
		EventID:          eventID,
		Source:           "edge_guard",
		OccurredAt:       now.Unix(),
		Nonce:            nonce,
		RequestID:        detection.RequestID,
		Model:            detection.Model,
		RuleVersion:      detection.RuleVersion,
		EvidenceSHA256:   detection.EvidenceSHA256,
		TokenFingerprint: detection.TokenFingerprint,
		Recommendation:   "reject",
		Metadata: map[string]any{
			"case_id":                     detection.RuleID,
			"causality":                   "ambiguous",
			"client_token_action_allowed": false,
			"evidence_level":              "confirmed",
			"request_body_bytes":          detection.BodyBytes,
			"request_body_sha256":         detection.EvidenceSHA256,
			"rule_id":                     detection.RuleID,
			"upstream_action_allowed":     false,
		},
	})
	if err != nil || len(payload) > 16*1024 {
		return "", errors.New("risk event payload is invalid")
	}
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	_, err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: riskStreamName,
		MaxLen: riskStreamMaxLen,
		Approx: true,
		Values: map[string]any{"payload": string(payload), "signature": signature},
	}).Result()
	return eventID, err
}

func randomHex(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
