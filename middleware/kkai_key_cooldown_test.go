package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMiddlewareKeyCooldownStore struct {
	state       service.KKAIPolicyKeyCooldownState
	err         error
	checkKeys   []string
	recordCalls int
}

func (s *fakeMiddlewareKeyCooldownStore) Check(_ context.Context, key string) (service.KKAIPolicyKeyCooldownState, error) {
	s.checkKeys = append(s.checkKeys, key)
	return s.state, s.err
}

func (s *fakeMiddlewareKeyCooldownStore) Record(context.Context, string, string) (service.KKAIPolicyKeyCooldownState, error) {
	s.recordCalls++
	return service.KKAIPolicyKeyCooldownState{}, nil
}

func runKeyCooldownMiddleware(t *testing.T, tokenID int, store service.KKAIPolicyKeyCooldownStore) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reached := false
	engine.Use(func(c *gin.Context) {
		if tokenID > 0 {
			common.SetContextKey(c, constant.ContextKeyTokenId, tokenID)
		}
		c.Set(common.RequestIdKey, "req-key-cooldown")
		c.Next()
	})
	engine.Use(kkaiPolicyKeyCooldown(store))
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	return recorder, reached
}

func TestKKAIPolicyKeyCooldownBlocksBeforeDownstreamWork(t *testing.T) {
	store := &fakeMiddlewareKeyCooldownStore{state: service.KKAIPolicyKeyCooldownState{
		Blocked: true, RetryAfter: 37, Strike: 1,
	}}

	recorder, reached := runKeyCooldownMiddleware(t, 42, store)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.False(t, reached)
	assert.Equal(t, "37", recorder.Header().Get("Retry-After"))
	require.Len(t, store.checkKeys, 1)
	assert.Zero(t, store.recordCalls)
	expectedKey, ok := service.KKAIPolicyKeyCooldownRedisKey(42)
	require.True(t, ok)
	assert.Equal(t, expectedKey, store.checkKeys[0])

	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, string(types.ErrorCodeKeyCooldown), response.Error.Code)
	assert.Contains(t, response.Error.Message, "37 秒")
	assert.NotContains(t, response.Error.Message, "上游")
	assert.NotContains(t, response.Error.Message, "渠道 Key")
	assert.Contains(t, response.Error.Message, "req-key-cooldown")
}

func TestKKAIPolicyKeyCooldownSwitchBypassesExistingCooldown(t *testing.T) {
	previous := common.CyberPolicyKeyCooldownEnabled
	common.CyberPolicyKeyCooldownEnabled = false
	t.Cleanup(func() { common.CyberPolicyKeyCooldownEnabled = previous })
	store := &fakeMiddlewareKeyCooldownStore{state: service.KKAIPolicyKeyCooldownState{
		Blocked: true, RetryAfter: 60, Strike: 1,
	}}

	recorder, reached := runKeyCooldownMiddleware(t, 42, store)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, reached)
	assert.Empty(t, store.checkKeys)
}

func TestKKAIPolicyKeyCooldownHonorsEmergencyBlockBeforeRedis(t *testing.T) {
	const tokenID = 887712
	gin.SetMode(gin.TestMode)
	seed, _ := gin.CreateTestContext(httptest.NewRecorder())
	seed.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(seed, constant.ContextKeyTokenId, tokenID)
	service.RecordKKAIPolicyEmergencyKeyCooldown(seed, time.Now())
	store := &fakeMiddlewareKeyCooldownStore{}

	recorder, reached := runKeyCooldownMiddleware(t, tokenID, store)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.False(t, reached)
	assert.Empty(t, store.checkKeys)
	assert.NotEmpty(t, recorder.Header().Get("Retry-After"))
}

func TestKKAIPolicyKeyCooldownWithoutTokenFailsOpen(t *testing.T) {
	store := &fakeMiddlewareKeyCooldownStore{state: service.KKAIPolicyKeyCooldownState{Blocked: true, RetryAfter: 60}}

	recorder, reached := runKeyCooldownMiddleware(t, 0, store)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, reached)
	assert.Empty(t, store.checkKeys)
}

func TestKKAIPolicyKeyCooldownRedisFailureFailsClosedAndWarns(t *testing.T) {
	store := &fakeMiddlewareKeyCooldownStore{err: errors.New("redis unavailable")}
	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	recorder, reached := runKeyCooldownMiddleware(t, 42, store)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.False(t, reached)
	assert.Contains(t, logBuffer.String(), "failing closed")
	assert.Contains(t, logBuffer.String(), "redis unavailable")
	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, string(types.ErrorCodePolicyAuditUnavailable), response.Error.Code)
	assert.Contains(t, response.Error.Message, "req-key-cooldown")
}

func TestKKAIPolicyKeyCooldownUnavailableStoreFailsClosedWhenRedisEnabled(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	recorder, reached := runKeyCooldownMiddleware(t, 42, nil)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.False(t, reached)
}

func TestKKAIPolicyKeyCooldownUnavailableStoreSkipsWhenRedisExplicitlyDisabled(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	recorder, reached := runKeyCooldownMiddleware(t, 42, nil)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, reached)
}

func TestKKAIPolicyKeyCooldownRetryAfterHasMinimumOneSecond(t *testing.T) {
	store := &fakeMiddlewareKeyCooldownStore{state: service.KKAIPolicyKeyCooldownState{Blocked: true}}
	recorder, reached := runKeyCooldownMiddleware(t, 42, store)

	assert.False(t, reached)
	assert.Equal(t, strconv.Itoa(1), recorder.Header().Get("Retry-After"))
}
