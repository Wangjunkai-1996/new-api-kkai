package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedis86RiskStreamConsumerLifecycle(t *testing.T) {
	address := os.Getenv("KKAI_TEST_REDIS_ADDRESS")
	if address == "" {
		if os.Getenv("KKAI_TEST_REDIS_REQUIRED") == "true" {
			t.Fatal("KKAI_TEST_REDIS_ADDRESS is required for the mandatory Redis integration check")
		}
		t.Skip("Redis 8.6 integration environment is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(ctx).Err())

	store := newRedisRiskStreamStore(client)
	store.stream = "kkai:test:risk:incidents"
	store.deadLetterStream = store.stream + ":dead"
	store.group = "risk-test-group"
	require.NoError(t, store.EnsureGroup(ctx))

	claimed, next, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: store.stream, Group: store.group, Consumer: "worker-a", MinIdle: 0, Start: "0-0", Count: 10,
	}).Result()
	require.NoError(t, err)
	require.Empty(t, claimed)
	require.NotEmpty(t, next)

	messageID, err := store.Publish(ctx, validRiskStreamEvent(time.Now()), riskStreamTestSecret)
	require.NoError(t, err)
	newMessages, err := store.ReadNew(ctx, "worker-a", 10, 10*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, newMessages, 1)
	require.Equal(t, messageID, newMessages[0].ID)

	claimed, _, err = client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: store.stream, Group: store.group, Consumer: "worker-b", MinIdle: 0, Start: "0-0", Count: 10,
	}).Result()
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, messageID, claimed[0].ID)
	require.NoError(t, store.Ack(ctx, messageID))

	deadID, err := store.Publish(ctx, validRiskStreamEvent(time.Now()), riskStreamTestSecret)
	require.NoError(t, err)
	deadMessages, err := store.ReadNew(ctx, "worker-a", 10, 10*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, deadMessages, 1)
	require.Equal(t, deadID, deadMessages[0].ID)
	require.NoError(t, store.Reject(ctx, deadMessages[0], "integration rejection"))
	entries, err := client.XRangeN(ctx, store.deadLetterStream, "-", "+", 10).Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, deadID, entries[0].Values["source_message_id"])

	connection := client.Conn()
	require.NoError(t, connection.Close())
	require.NoError(t, client.Ping(ctx).Err())
	streams, err := store.ReadNew(ctx, "worker-c", 1, 10*time.Millisecond)
	require.NoError(t, err)
	require.Empty(t, streams)
	require.NotEqual(t, riskStreamTestSecret, entries[0].Values["payload"])
}
