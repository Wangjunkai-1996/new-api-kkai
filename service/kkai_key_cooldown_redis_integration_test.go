package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedis86KKAIPolicyKeyCooldownStateMachine(t *testing.T) {
	address := os.Getenv("KKAI_TEST_REDIS_ADDRESS")
	if address == "" {
		if os.Getenv("KKAI_TEST_REDIS_REQUIRED") == "true" {
			t.Fatal("KKAI_TEST_REDIS_ADDRESS is required for the mandatory Redis integration check")
		}
		t.Skip("Redis 8.6 integration environment is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address, DB: 14})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(ctx).Err())

	store := newRedisKKAIPolicyKeyCooldownStore(client)
	key := "kkai:policy:key-cooldown:v2:integration-scope"
	digest := func(event string) string {
		value, ok := KKAIPolicyKeyCooldownEventDigest(event)
		require.True(t, ok)
		return value
	}

	state, err := store.Record(ctx, key, digest("event-a"))
	require.NoError(t, err)
	assert.True(t, state.Blocked)
	assert.Equal(t, 60, state.RetryAfter)
	assert.Equal(t, 1, state.Strike)

	checked, err := store.Check(ctx, key)
	require.NoError(t, err)
	assert.True(t, checked.Blocked)
	assert.GreaterOrEqual(t, checked.RetryAfter, 59)
	assert.LessOrEqual(t, checked.RetryAfter, 60)

	replayed, err := store.Record(ctx, key, digest("event-a"))
	require.NoError(t, err)
	assert.Equal(t, 1, replayed.Strike)

	duringCooldown, err := store.Record(ctx, key, digest("event-b"))
	require.NoError(t, err)
	assert.Equal(t, 1, duringCooldown.Strike)
	assert.GreaterOrEqual(t, duringCooldown.RetryAfter, 59)
	assert.LessOrEqual(t, duringCooldown.RetryAfter, 60)
	ttlAfterConcurrentEvent, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttlAfterConcurrentEvent, 23*time.Hour)
	require.NoError(t, client.HSet(ctx, key, "blocked_until", 0).Err())
	ignoredReplay, err := store.Record(ctx, key, digest("event-b"))
	require.NoError(t, err)
	assert.False(t, ignoredReplay.Blocked)
	assert.Equal(t, 1, ignoredReplay.Strike)

	require.NoError(t, client.HSet(ctx, key, "blocked_until", 0).Err())
	reset, err := store.Record(ctx, key, digest("event-after-reset"))
	require.NoError(t, err)
	assert.Equal(t, 60, reset.RetryAfter)
	assert.Equal(t, 1, reset.Strike)

	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 23*time.Hour)
	fields, err := client.HKeys(ctx, key).Result()
	require.NoError(t, err)
	for _, field := range fields {
		assert.NotContains(t, field, "event-after-reset")
		assert.NotContains(t, field, "sk-")
	}
}
