package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKKAIPolicyKeyCooldownStore struct {
	checkState           KKAIPolicyKeyCooldownState
	checkErr             error
	recordState          KKAIPolicyKeyCooldownState
	recordErr            error
	checkKeys            []string
	recordKeys           []string
	eventDigests         []string
	recordCtxErr         []error
	recordCtxHasDeadline []bool
}

func (s *fakeKKAIPolicyKeyCooldownStore) Check(_ context.Context, key string) (KKAIPolicyKeyCooldownState, error) {
	s.checkKeys = append(s.checkKeys, key)
	return s.checkState, s.checkErr
}

func (s *fakeKKAIPolicyKeyCooldownStore) Record(ctx context.Context, key string, eventDigest string) (KKAIPolicyKeyCooldownState, error) {
	s.recordKeys = append(s.recordKeys, key)
	s.eventDigests = append(s.eventDigests, eventDigest)
	s.recordCtxErr = append(s.recordCtxErr, ctx.Err())
	_, hasDeadline := ctx.Deadline()
	s.recordCtxHasDeadline = append(s.recordCtxHasDeadline, hasDeadline)
	if s.recordErr == nil && s.recordState == (KKAIPolicyKeyCooldownState{}) {
		return KKAIPolicyKeyCooldownState{Blocked: true, RetryAfter: 60, Strike: 1}, nil
	}
	return s.recordState, s.recordErr
}

type fakeKKAIPolicyKeyCooldownRedisClient struct {
	result any
	err    error
	script string
	keys   []string
	args   []interface{}
}

func (c *fakeKKAIPolicyKeyCooldownRedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	c.script = script
	c.keys = append([]string(nil), keys...)
	c.args = append([]interface{}(nil), args...)
	cmd := redis.NewCmd(ctx)
	cmd.SetVal(c.result)
	cmd.SetErr(c.err)
	return cmd
}

func newKKAIPolicyCooldownTestContext(userID, tokenID int, sessionID string) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, userID)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, tokenID)
	if sessionID != "" {
		ctx.Request.Header.Set("X-Session-Id", sessionID)
	}
	return ctx
}

func TestKKAIPolicyKeyCooldownScopesToUserTokenAndConversation(t *testing.T) {
	originalSecret := common.CryptoSecret
	common.CryptoSecret = "key-cooldown-test-secret"
	t.Cleanup(func() { common.CryptoSecret = originalSecret })

	ctx := newKKAIPolicyCooldownTestContext(7, 42, "conversation-a")
	key, ok := KKAIPolicyKeyCooldownRedisKey(ctx)
	require.True(t, ok)
	expected := kkaiPolicyKeyCooldownKeyPrefix + common.GenerateHMAC(
		kkaiPolicyKeyCooldownScopeDomain+"\x00user_id\x007\x00token_id\x0042\x00session_id\x00conversation-a",
	)
	assert.Equal(t, expected, key)
	assert.Len(t, key, len(kkaiPolicyKeyCooldownKeyPrefix)+64)

	sameKey, ok := KKAIPolicyKeyCooldownRedisKey(ctx)
	require.True(t, ok)
	assert.Equal(t, key, sameKey)
	ctx.Request.Header.Set("X-Session-Id", "conversation-b")
	otherKey, ok := KKAIPolicyKeyCooldownRedisKey(ctx)
	require.True(t, ok)
	assert.NotEqual(t, key, otherKey)

	for _, other := range []*gin.Context{
		newKKAIPolicyCooldownTestContext(8, 42, "conversation-a"),
		newKKAIPolicyCooldownTestContext(7, 43, "conversation-a"),
	} {
		otherKey, valid := KKAIPolicyKeyCooldownRedisKey(other)
		require.True(t, valid)
		assert.NotEqual(t, key, otherKey)
	}
	for _, invalid := range []*gin.Context{
		newKKAIPolicyCooldownTestContext(0, 42, "conversation-a"),
		newKKAIPolicyCooldownTestContext(7, 0, "conversation-a"),
		newKKAIPolicyCooldownTestContext(7, 42, ""),
	} {
		_, valid := KKAIPolicyKeyCooldownRedisKey(invalid)
		assert.False(t, valid)
	}
	assert.NotContains(t, key, "conversation-a")
}

func TestKKAIPolicyClientSessionIDUsesOnlyValidExplicitHeaders(t *testing.T) {
	ctx := newKKAIPolicyCooldownTestContext(7, 42, "")
	ctx.Request.Header.Set("X-Session-Id", "  conversation-42  ")
	assert.Equal(t, "conversation-42", KKAIPolicyClientSessionID(ctx))
	ctx.Request.Header.Set("session_id", "first-choice")
	assert.Equal(t, "first-choice", KKAIPolicyClientSessionID(ctx))
	ctx.Request.Header.Set("session_id", "bad\tvalue")
	assert.Equal(t, "conversation-42", KKAIPolicyClientSessionID(ctx))
	ctx.Request.Header.Set("X-Session-Id", strings.Repeat("x", 256))
	assert.Empty(t, KKAIPolicyClientSessionID(ctx))
	ctx.Request.Header.Del("X-Session-Id")
	ctx.Request.Header.Set("X-Request-Id", "request-only")
	assert.Empty(t, KKAIPolicyClientSessionID(ctx))
}

func TestRecordKKAIPolicyKeyCooldownHashesEventAndIgnoresOtherContext(t *testing.T) {
	originalSecret := common.CryptoSecret
	common.CryptoSecret = "key-cooldown-test-secret"
	t.Cleanup(func() { common.CryptoSecret = originalSecret })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Session-Id", "conversation-a")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 99)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 42)
	ctx.Set("channel_id", 7)
	ctx.Set("token_key", "sk-client-secret")
	store := &fakeKKAIPolicyKeyCooldownStore{}

	_, err := RecordKKAIPolicyKeyCooldown(ctx, store, "event-with-request-and-channel-data")
	require.NoError(t, err)
	require.Len(t, store.recordKeys, 1)
	require.Len(t, store.eventDigests, 1)
	expectedKey, _ := KKAIPolicyKeyCooldownRedisKey(ctx)
	assert.Equal(t, expectedKey, store.recordKeys[0])
	assert.NotContains(t, store.recordKeys[0], "sk-client-secret")
	assert.NotContains(t, store.eventDigests[0], "event-with-request-and-channel-data")
	assert.Len(t, store.eventDigests[0], 64)

	ctx.Set("channel_id", 8)
	ctx.Set("token_key", "different-secret")
	_, err = RecordKKAIPolicyKeyCooldown(ctx, store, "second-event")
	require.NoError(t, err)
	require.Len(t, store.recordKeys, 2)
	assert.Equal(t, store.recordKeys[0], store.recordKeys[1])
	ctx.Request.Header.Set("X-Session-Id", "conversation-b")
	_, err = RecordKKAIPolicyKeyCooldown(ctx, store, "third-event")
	require.NoError(t, err)
	require.Len(t, store.recordKeys, 3)
	assert.NotEqual(t, store.recordKeys[0], store.recordKeys[2])
}

func TestRecordKKAIPolicyKeyCooldownWithoutTokenFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	store := &fakeKKAIPolicyKeyCooldownStore{}

	state, err := RecordKKAIPolicyKeyCooldown(ctx, store, "event-1")
	require.NoError(t, err)
	assert.Equal(t, KKAIPolicyKeyCooldownState{}, state)
	assert.Empty(t, store.recordKeys)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 42)
	state, err = RecordKKAIPolicyKeyCooldown(ctx, store, "event-2")
	require.NoError(t, err)
	assert.Equal(t, KKAIPolicyKeyCooldownState{}, state)
	assert.Empty(t, store.recordKeys)
}

func TestRedisKKAIPolicyKeyCooldownStoreUsesAtomicScriptWithoutRawEvent(t *testing.T) {
	client := &fakeKKAIPolicyKeyCooldownRedisClient{
		result: []interface{}{int64(1), int64(60), int64(1), int64(1_720_000_060_000)},
	}
	store := newRedisKKAIPolicyKeyCooldownStore(client)
	eventDigest := common.GenerateHMAC("event-digest")

	state, err := store.Record(context.Background(), "kkai:policy:key-cooldown:v3:digest", eventDigest)
	require.NoError(t, err)
	assert.True(t, state.Blocked)
	assert.Equal(t, 60, state.RetryAfter)
	assert.Equal(t, 1, state.Strike)
	assert.Contains(t, client.script, `redis.call("TIME")`)
	assert.Contains(t, client.script, "HEXISTS")
	assert.Contains(t, client.script, "proposed_blocked_until")
	assert.Contains(t, client.script, "if proposed_blocked_until > blocked_until then")
	assert.Equal(t, []string{"kkai:policy:key-cooldown:v3:digest"}, client.keys)
	assert.Equal(t, []interface{}{"record", eventDigest}, client.args)
	assert.NotContains(t, client.args, "event-digest")
}

func TestEmergencyKeyCooldownSweepsExpiredOtherTokens(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	expiredKey, ok := KKAIPolicyKeyCooldownRedisKey(newKKAIPolicyCooldownTestContext(7, 900001, "expired"))
	require.True(t, ok)
	activeKey, ok := KKAIPolicyKeyCooldownRedisKey(newKKAIPolicyCooldownTestContext(7, 900002, "active"))
	require.True(t, ok)

	kkaiPolicyEmergencyCooldowns.Lock()
	kkaiPolicyEmergencyCooldowns.blockedUntil = map[string]time.Time{
		expiredKey: now.Add(-time.Second),
		activeKey:  now.Add(time.Hour),
	}
	kkaiPolicyEmergencyCooldowns.lastSweep = time.Time{}
	kkaiPolicyEmergencyCooldowns.Unlock()
	t.Cleanup(func() {
		kkaiPolicyEmergencyCooldowns.Lock()
		delete(kkaiPolicyEmergencyCooldowns.blockedUntil, expiredKey)
		delete(kkaiPolicyEmergencyCooldowns.blockedUntil, activeKey)
		kkaiPolicyEmergencyCooldowns.lastSweep = time.Time{}
		kkaiPolicyEmergencyCooldowns.Unlock()
	})

	state := CheckKKAIPolicyEmergencyKeyCooldown(activeKey, now)
	require.True(t, state.Blocked)
	kkaiPolicyEmergencyCooldowns.Lock()
	_, expiredStillPresent := kkaiPolicyEmergencyCooldowns.blockedUntil[expiredKey]
	_, activeStillPresent := kkaiPolicyEmergencyCooldowns.blockedUntil[activeKey]
	kkaiPolicyEmergencyCooldowns.Unlock()
	assert.False(t, expiredStillPresent)
	assert.True(t, activeStillPresent)
}

func TestRedisKKAIPolicyKeyCooldownStorePropagatesFailure(t *testing.T) {
	expected := errors.New("redis unavailable")
	store := newRedisKKAIPolicyKeyCooldownStore(&fakeKKAIPolicyKeyCooldownRedisClient{err: expected})

	_, err := store.Check(context.Background(), "kkai:policy:key-cooldown:v3:digest")
	require.ErrorIs(t, err, expected)
}

func TestParseKKAIPolicyKeyCooldownResultRejectsInvalidData(t *testing.T) {
	_, err := parseKKAIPolicyKeyCooldownResult("invalid")
	require.Error(t, err)
	_, err = parseKKAIPolicyKeyCooldownResult([]interface{}{1, 2, 3})
	require.Error(t, err)

	invalidStates := []any{
		[]interface{}{int64(2), int64(60), int64(1), int64(1)},
		[]interface{}{int64(1), int64(0), int64(1), int64(1)},
		[]interface{}{int64(1), int64(60), int64(0), int64(1)},
		[]interface{}{int64(1), int64(61), int64(1), int64(1)},
		[]interface{}{int64(0), int64(1), int64(1), int64(1)},
		[]interface{}{int64(0), int64(0), int64(2), int64(1)},
		[]interface{}{int64(0), int64(0), int64(1), int64(-1)},
	}
	for _, value := range invalidStates {
		_, err = parseKKAIPolicyKeyCooldownResult(value)
		require.Error(t, err)
	}
}
