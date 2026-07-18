package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	riskStreamDeadLetterMaxLen = 10_000
	riskStreamRejectScript     = `
local dead_id = redis.call(
  'XADD', KEYS[2], 'MAXLEN', '~', ARGV[1], '*',
  'source_message_id', ARGV[3],
  'reason', ARGV[4],
  'payload_sha256', ARGV[5],
  'failed_at', ARGV[6]
)
redis.call('XACK', KEYS[1], ARGV[2], ARGV[3])
return dead_id
`
)

type riskStreamRedisClient interface {
	XGroupCreateMkStream(context.Context, string, string, string) *redis.StatusCmd
	XReadGroup(context.Context, *redis.XReadGroupArgs) *redis.XStreamSliceCmd
	XAutoClaim(context.Context, *redis.XAutoClaimArgs) *redis.XAutoClaimCmd
	XAck(context.Context, string, string, ...string) *redis.IntCmd
	XAdd(context.Context, *redis.XAddArgs) *redis.StringCmd
	XPending(context.Context, string, string) *redis.XPendingCmd
	XLen(context.Context, string) *redis.IntCmd
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}

type RedisRiskStreamStore struct {
	client           riskStreamRedisClient
	stream           string
	deadLetterStream string
	group            string
	now              func() time.Time
}

func NewRedisRiskStreamStore(client *redis.Client) *RedisRiskStreamStore {
	return newRedisRiskStreamStore(client)
}

func newRedisRiskStreamStore(client riskStreamRedisClient) *RedisRiskStreamStore {
	return &RedisRiskStreamStore{
		client:           client,
		stream:           KKAIRiskStreamName,
		deadLetterStream: KKAIRiskDeadLetterStream,
		group:            KKAIRiskConsumerGroup,
		now:              time.Now,
	}
}

func (s *RedisRiskStreamStore) EnsureGroup(ctx context.Context) error {
	if !s.configured() {
		return errors.New("risk stream Redis client is unavailable")
	}
	err := s.client.XGroupCreateMkStream(ctx, s.stream, s.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (s *RedisRiskStreamStore) ReadNew(ctx context.Context, consumer string, count int64, block time.Duration) ([]RiskStreamMessage, error) {
	if !s.configured() || strings.TrimSpace(consumer) == "" || count <= 0 || block < 0 {
		return nil, ErrRiskStreamInvalidEvent
	}
	streams, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    s.group,
		Consumer: consumer,
		Streams:  []string{s.stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return flattenRiskStreamMessages(streams), nil
}

func (s *RedisRiskStreamStore) ClaimPending(ctx context.Context, consumer string, count int64, minIdle time.Duration) ([]RiskStreamMessage, error) {
	if !s.configured() || strings.TrimSpace(consumer) == "" || count <= 0 || minIdle < 0 {
		return nil, ErrRiskStreamInvalidEvent
	}
	messages, _, err := s.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   s.stream,
		Group:    s.group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    count,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]RiskStreamMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, riskStreamMessageFromRedis(message))
	}
	return result, nil
}

func (s *RedisRiskStreamStore) Ack(ctx context.Context, ids ...string) error {
	if !s.configured() {
		return errors.New("risk stream Redis client is unavailable")
	}
	if len(ids) == 0 {
		return nil
	}
	return s.client.XAck(ctx, s.stream, s.group, ids...).Err()
}

func (s *RedisRiskStreamStore) Reject(ctx context.Context, message RiskStreamMessage, reason string) error {
	if !s.configured() || strings.TrimSpace(message.ID) == "" {
		return ErrRiskStreamInvalidEvent
	}
	digest := sha256.Sum256([]byte(message.Payload))
	return s.client.Eval(
		ctx,
		riskStreamRejectScript,
		[]string{s.stream, s.deadLetterStream},
		strconv.FormatInt(riskStreamDeadLetterMaxLen, 10),
		s.group,
		message.ID,
		sanitizeKKAIOutboxError(errors.New(reason)),
		hex.EncodeToString(digest[:]),
		strconv.FormatInt(s.now().Unix(), 10),
	).Err()
}

func (s *RedisRiskStreamStore) Status(ctx context.Context) (RiskStreamStoreStatus, error) {
	if !s.configured() {
		return RiskStreamStoreStatus{}, errors.New("risk stream Redis client is unavailable")
	}
	pending, err := s.client.XPending(ctx, s.stream, s.group).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return RiskStreamStoreStatus{}, err
	}
	deadLetter, err := s.client.XLen(ctx, s.deadLetterStream).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return RiskStreamStoreStatus{}, err
	}
	status := RiskStreamStoreStatus{DeadLetter: deadLetter}
	if pending != nil {
		status.Pending = pending.Count
		status.OldestPendingAt = riskStreamUnixFromID(pending.Lower)
	}
	return status, nil
}

func (s *RedisRiskStreamStore) Publish(ctx context.Context, event RiskStreamEvent, secret string) (string, error) {
	if !s.configured() {
		return "", errors.New("risk stream Redis client is unavailable")
	}
	payload, signature, err := SignRiskStreamEvent(event, secret)
	if err != nil {
		return "", err
	}
	return s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: s.stream,
		Values: map[string]any{
			"payload":   payload,
			"signature": signature,
		},
	}).Result()
}

func (s *RedisRiskStreamStore) configured() bool {
	return s != nil && s.client != nil && s.stream != "" && s.deadLetterStream != "" && s.group != "" && s.now != nil
}

func flattenRiskStreamMessages(streams []redis.XStream) []RiskStreamMessage {
	var result []RiskStreamMessage
	for _, stream := range streams {
		for _, message := range stream.Messages {
			result = append(result, riskStreamMessageFromRedis(message))
		}
	}
	return result
}

func riskStreamMessageFromRedis(message redis.XMessage) RiskStreamMessage {
	payload, _ := riskStreamRedisString(message.Values["payload"])
	signature, _ := riskStreamRedisString(message.Values["signature"])
	return RiskStreamMessage{ID: message.ID, Payload: payload, Signature: signature}
}

func riskStreamRedisString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

func riskStreamUnixFromID(id string) int64 {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	milliseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || milliseconds < 0 {
		return 0
	}
	return milliseconds / 1000
}
