package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRateLimitMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Ping(context.Background()).Err())

	common.RedisEnabled = true
	common.RDB = redisClient
	t.Cleanup(func() {
		_ = redisClient.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
	})

	return redisServer, redisClient
}

func performRateLimitRequest(router http.Handler, path string, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRedisIPRateLimiterSharesExistingSlidingWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/limited", rateLimitFactory(2, 37, "TEST"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	key := "rateLimit:TEST192.0.2.10"
	legacyStamp := time.Now().Format(timeFormat)
	_, err := redisServer.Push(key, legacyStamp)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", "192.0.2.10:12345").Code)
	response := performRateLimitRequest(router, "/limited", "192.0.2.10:12345")
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.Equal(t, "37", response.Header().Get("Retry-After"))
	entries, err := redisServer.List(key)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, legacyStamp, entries[1])
	assert.Equal(t, common.RateLimitKeyExpirationDuration, redisServer.TTL(key))
	assert.Equal(t, []string{key}, redisServer.Keys())
}

func TestRedisUserRateLimiterSharesExistingUserWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)
	router := gin.New()
	router.GET("/limited", func(c *gin.Context) { c.Set("id", 42) },
		userRateLimitFactory(1, 23, "USER"), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", "192.0.2.20:12345").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/limited", "198.51.100.20:12345").Code)
	key := "rateLimit:USER:user:42"
	entries, err := redisServer.List(key)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, common.RateLimitKeyExpirationDuration, redisServer.TTL(key))
}

func TestRedisRateLimitTenantIsolation(t *testing.T) {
	for _, scope := range []string{"ip", "user", "email"} {
		t.Run(scope, func(t *testing.T) {
			redisServer, _ := useRateLimitMiniRedis(t)
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			limit := rateLimitFactory(1, 30, "TENANT")
			key := "rateLimit:TENANT192.0.2.21"
			if scope == "user" {
				limit = userRateLimitFactory(1, 30, "TENANT")
				key = "rateLimit:TENANT:user:42"
			} else if scope == "email" {
				limit = EmailVerificationRateLimit()
				key = "emailVerification:EV:192.0.2.21"
			}
			router.GET("/:tenant", func(c *gin.Context) {
				c.Set("id", 42)
				if c.Param("tenant") == "tenant" {
					common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
				}
			}, limit, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/platform", "192.0.2.21:1000").Code)
			if scope == "email" {
				assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/platform", "192.0.2.21:1000").Code)
			}
			assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/platform", "192.0.2.21:1000").Code)
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/tenant", "192.0.2.21:1000").Code)
			assert.ElementsMatch(t, []string{key, "tenant:42:" + key}, redisServer.Keys())
		})
	}
}

func TestRedisEmailVerificationRateLimiterPreservesResponseAndTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/verify", EmailVerificationRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	remoteAddr := "192.0.2.30:12345"
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	response := performRateLimitRequest(router, "/verify", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.Equal(t, "30", response.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"success":false,"message":"Please wait 30 seconds before requesting another verification code.","message_key":"Please wait {{seconds}} seconds before requesting another verification code.","message_params":{"seconds":30}}`, response.Body.String())
	key := "emailVerification:EV:192.0.2.30"
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "3", count)
	assert.Equal(t, time.Duration(EmailVerificationDuration)*time.Second, redisServer.TTL(key))
}

func TestRedisSlidingWindowIsAtomicUnderConcurrency(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const requestCount, maximumCount = 20, 7
	key := "rateLimit:CONCURRENT192.0.2.40"
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var allowedCount atomic.Int64
	errorsFound := make(chan error, requestCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for range requestCount {
		go func() {
			defer waitGroup.Done()
			allowed, _, err := redisSlidingWindowTake(context.Background(), key, maximumCount, 41, now)
			if err != nil {
				errorsFound <- err
				return
			}
			if allowed {
				allowedCount.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(maximumCount), allowedCount.Load())
	entries, err := redisServer.List(key)
	require.NoError(t, err)
	assert.Len(t, entries, maximumCount)
	assert.Equal(t, common.RateLimitKeyExpirationDuration, redisServer.TTL(key))
}

func TestRedisSlidingWindowPreservesStaggeredBoundaries(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	key := "rateLimit:BOUNDARY192.0.2.50"
	// Existing nodes store local wall time with a literal Z, including non-UTC hosts.
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	for _, tc := range []struct {
		offset  time.Duration
		allowed bool
		retry   int64
	}{
		{0, true, 0}, {9 * time.Second, true, 0},
		{9999 * time.Millisecond, false, 1}, {10 * time.Second, true, 0},
		{10 * time.Second, false, 9}, {19 * time.Second, true, 0},
	} {
		allowed, retry, err := redisSlidingWindowTake(context.Background(), key, 2, 10, now.Add(tc.offset))
		require.NoError(t, err)
		assert.Equal(t, tc.allowed, allowed, "offset %s", tc.offset)
		assert.Equal(t, tc.retry, retry, "offset %s", tc.offset)
	}
	entries, err := redisServer.List(key)
	require.NoError(t, err)
	assert.Equal(t, []string{now.Add(19 * time.Second).Format(timeFormat), now.Add(10 * time.Second).Format(timeFormat)}, entries)
}

func TestRedisSlidingWindowRejectsMalformedExistingTimestamp(t *testing.T) {
	for _, stamp := range []string{"malformed", "2026-02-30T00:00:00.000Z"} {
		t.Run(stamp, func(t *testing.T) {
			redisServer, _ := useRateLimitMiniRedis(t)
			key := "rateLimit:BAD192.0.2.52"
			_, err := redisServer.Push(key, stamp)
			require.NoError(t, err)
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			router.GET("/limited", rateLimitFactory(1, 30, "BAD"), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := performRateLimitRequest(router, "/limited", "192.0.2.52:1000")
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			entries, err := redisServer.List(key)
			require.NoError(t, err)
			assert.Equal(t, []string{stamp}, entries)
		})
	}
}

func TestRedisEmailVerificationRepairsCounterWithoutTTL(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	key := "emailVerification:EV:192.0.2.51"
	require.NoError(t, redisServer.Set(key, "5"))
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/verify", EmailVerificationRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := performRateLimitRequest(router, "/verify", "192.0.2.51:1000")
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.Equal(t, "30", response.Header().Get("Retry-After"))
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "6", count)
	assert.Equal(t, 30*time.Second, redisServer.TTL(key))
	redisServer.FastForward(30 * time.Second)
	assert.False(t, redisServer.Exists(key))
}

func TestRedisFailurePolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, redisClient := useRateLimitMiniRedis(t)
	require.NoError(t, redisClient.Close())

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/ip", rateLimitFactory(1, 30, "FAIL-IP"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET(
		"/user",
		func(c *gin.Context) { c.Set("id", 7) },
		userRateLimitFactory(1, 30, "FAIL-USER"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	router.GET("/email", EmailVerificationRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	ipResponse := performRateLimitRequest(router, "/ip", "192.0.2.60:12345")
	assert.Equal(t, http.StatusInternalServerError, ipResponse.Code)
	assert.Empty(t, ipResponse.Body.String())
	userResponse := performRateLimitRequest(router, "/user", "192.0.2.61:12345")
	assert.Equal(t, http.StatusInternalServerError, userResponse.Code)
	assert.Empty(t, userResponse.Body.String())
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/email", "192.0.2.62:12345").Code)
}

func TestGlobalWebRateLimitCountsFrontendFilesInStaticBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWebEnable, previousWebNum, previousWebDuration := common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration
	previousStaticEnable, previousStaticNum, previousStaticDuration := common.GlobalStaticRateLimitEnable, common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration
	t.Cleanup(func() {
		common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration = previousWebEnable, previousWebNum, previousWebDuration
		common.GlobalStaticRateLimitEnable, common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration = previousStaticEnable, previousStaticNum, previousStaticDuration
	})
	common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration = true, 2, 41
	common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration = 2, 43

	frontendDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), []byte("app"), 0o644))
	frontendFS := static.LocalFile(frontendDir, false)

	const clientIP = "192.0.2.70"
	const remoteAddr = clientIP + ":12345"

	t.Run("static limiter disabled by default", func(t *testing.T) {
		redisServer, _ := useRateLimitMiniRedis(t)
		common.GlobalStaticRateLimitEnable = false
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(nil))
		router.NoRoute(GlobalWebRateLimit(frontendFS), func(c *gin.Context) { c.Status(http.StatusNoContent) })

		for range 5 {
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/static/js/app.js", remoteAddr).Code)
		}
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/channels", remoteAddr).Code)
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/static/js/missing.js", remoteAddr).Code)
		pageResponse := performRateLimitRequest(router, "/channels", remoteAddr)
		assert.Equal(t, http.StatusTooManyRequests, pageResponse.Code)
		assert.Equal(t, "41", pageResponse.Header().Get("Retry-After"))

		webCount, err := redisServer.List("rateLimit:GW" + clientIP)
		require.NoError(t, err)
		assert.Len(t, webCount, 2)
		assert.False(t, redisServer.Exists("rateLimit:GS"+clientIP))
	})

	t.Run("static limiter enabled uses its own bucket", func(t *testing.T) {
		redisServer, _ := useRateLimitMiniRedis(t)
		common.GlobalStaticRateLimitEnable = true
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(nil))
		router.NoRoute(GlobalWebRateLimit(frontendFS), func(c *gin.Context) { c.Status(http.StatusNoContent) })

		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/static/js/app.js", remoteAddr).Code)
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/static/js/app.js", remoteAddr).Code)
		staticResponse := performRateLimitRequest(router, "/static/js/app.js", remoteAddr)
		assert.Equal(t, http.StatusTooManyRequests, staticResponse.Code)
		assert.Equal(t, "43", staticResponse.Header().Get("Retry-After"))
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/channels", remoteAddr).Code)

		staticCount, err := redisServer.List("rateLimit:GS" + clientIP)
		require.NoError(t, err)
		assert.Len(t, staticCount, 2)
		webCount, err := redisServer.List("rateLimit:GW" + clientIP)
		require.NoError(t, err)
		assert.Len(t, webCount, 1)
	})
}
