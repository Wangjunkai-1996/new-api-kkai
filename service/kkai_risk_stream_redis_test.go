package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type fakeRiskStreamRedisClient struct {
	groupErr   error
	readErr    error
	claimErr   error
	ackErr     error
	addErr     error
	evalErr    error
	streams    []redis.XStream
	claimed    []redis.XMessage
	addArgs    *redis.XAddArgs
	evalScript string
	evalKeys   []string
	evalArgs   []interface{}
	pending    *redis.XPending
	pendingErr error
	deadLetter int64
	xlenErr    error
}

func (c *fakeRiskStreamRedisClient) XGroupCreateMkStream(ctx context.Context, _, _, _ string) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("OK")
	cmd.SetErr(c.groupErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XReadGroup(ctx context.Context, _ *redis.XReadGroupArgs) *redis.XStreamSliceCmd {
	cmd := redis.NewXStreamSliceCmd(ctx)
	cmd.SetVal(c.streams)
	cmd.SetErr(c.readErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XAutoClaim(ctx context.Context, _ *redis.XAutoClaimArgs) *redis.XAutoClaimCmd {
	cmd := redis.NewXAutoClaimCmd(ctx)
	cmd.SetVal(c.claimed, "0-0")
	cmd.SetErr(c.claimErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XAck(ctx context.Context, _, _ string, _ ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)
	cmd.SetVal(1)
	cmd.SetErr(c.ackErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	copied := *args
	c.addArgs = &copied
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal("1-0")
	cmd.SetErr(c.addErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	c.evalScript = script
	c.evalKeys = append([]string(nil), keys...)
	c.evalArgs = append([]interface{}(nil), args...)
	cmd := redis.NewCmd(ctx)
	cmd.SetVal("2-0")
	cmd.SetErr(c.evalErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XPending(ctx context.Context, _, _ string) *redis.XPendingCmd {
	cmd := redis.NewXPendingCmd(ctx)
	cmd.SetVal(c.pending)
	cmd.SetErr(c.pendingErr)
	return cmd
}

func (c *fakeRiskStreamRedisClient) XLen(ctx context.Context, _ string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)
	cmd.SetVal(c.deadLetter)
	cmd.SetErr(c.xlenErr)
	return cmd
}

func TestRedisRiskStreamStoreRejectsAtomicallyWithoutRawPayload(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	client := &fakeRiskStreamRedisClient{}
	store := newRedisRiskStreamStore(client)
	store.now = func() time.Time { return now }
	message := RiskStreamMessage{
		ID:        "1-0",
		Payload:   `{"event_id":"secret-payload","token":"sk-sensitive-value"}`,
		Signature: "sensitive-signature",
	}

	err := store.Reject(context.Background(), message, "authorization: Bearer sensitive-value")
	require.NoError(t, err)
	require.Contains(t, client.evalScript, "XADD")
	require.Contains(t, client.evalScript, "XACK")
	require.Equal(t, []string{KKAIRiskStreamName, KKAIRiskDeadLetterStream}, client.evalKeys)
	require.Len(t, client.evalArgs, 6)
	require.Equal(t, KKAIRiskConsumerGroup, client.evalArgs[1])
	require.Equal(t, message.ID, client.evalArgs[2])
	require.NotContains(t, client.evalArgs[3], "sensitive-value")
	digest := sha256.Sum256([]byte(message.Payload))
	require.Equal(t, hex.EncodeToString(digest[:]), client.evalArgs[4])
	require.Equal(t, fmt.Sprint(now.Unix()), client.evalArgs[5])
	for _, arg := range client.evalArgs {
		require.NotEqual(t, message.Payload, arg)
		require.NotEqual(t, message.Signature, arg)
	}
}

func TestRedisRiskStreamStorePropagatesAtomicRejectFailure(t *testing.T) {
	expected := errors.New("redis unavailable")
	store := newRedisRiskStreamStore(&fakeRiskStreamRedisClient{evalErr: expected})

	err := store.Reject(context.Background(), RiskStreamMessage{ID: "1-0", Payload: "payload"}, "invalid")
	require.ErrorIs(t, err, expected)
}

func TestRiskStreamMessageFromRedisRejectsUnexpectedFieldTypes(t *testing.T) {
	message := riskStreamMessageFromRedis(redis.XMessage{
		ID: "1-0",
		Values: map[string]interface{}{
			"payload":   123,
			"signature": true,
		},
	})

	require.Equal(t, "1-0", message.ID)
	require.Empty(t, message.Payload)
	require.Empty(t, message.Signature)
}

func TestRedisRiskStreamStorePublishesSignedEnvelope(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	client := &fakeRiskStreamRedisClient{}
	store := newRedisRiskStreamStore(client)

	id, err := store.Publish(context.Background(), validRiskStreamEvent(now), riskStreamTestSecret)
	require.NoError(t, err)
	require.Equal(t, "1-0", id)
	require.Equal(t, KKAIRiskStreamName, client.addArgs.Stream)
	values, ok := client.addArgs.Values.(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, values["payload"])
	require.NotEmpty(t, values["signature"])
}

func TestRedisRiskStreamStoreStatusReportsPendingAgeAndDeadLetters(t *testing.T) {
	store := newRedisRiskStreamStore(&fakeRiskStreamRedisClient{
		pending:    &redis.XPending{Count: 3, Lower: "1720000000123-0"},
		deadLetter: 2,
	})

	status, err := store.Status(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, status.Pending)
	require.EqualValues(t, 1_720_000_000, status.OldestPendingAt)
	require.EqualValues(t, 2, status.DeadLetter)
}
