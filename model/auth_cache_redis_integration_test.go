package model

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthCacheRedis86MetadataRepair(t *testing.T) {
	address := os.Getenv("TEST_AUTH_CACHE_REDIS_ADDR")
	if address == "" || !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires isolated TEST_AUTH_CACHE_REDIS_ADDR and TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host)
	client := redis.NewClient(&redis.Options{Addr: address, DB: 0, MaxRetries: -1})
	ctx := context.Background()
	info, err := client.Info(ctx, "server").Result()
	require.NoError(t, err)
	require.Contains(t, info, "redis_version:8.6.3\r\n")
	require.Contains(t, info, "redis_mode:standalone\r\n")
	previousClient, previousEnabled, previousTTL := common.RDB, common.RedisEnabled, common.SyncFrequency
	common.RDB, common.RedisEnabled, common.SyncFrequency = client, true, 60
	t.Cleanup(func() {
		assert.NoError(t, client.Close())
		common.RDB, common.RedisEnabled, common.SyncFrequency = previousClient, previousEnabled, previousTTL
	})
	truncateTables(t)
	user := createReserveTestUser(t, 100)
	userKey := getUserCacheKey(user.Id)
	t.Cleanup(func() {
		assert.NoError(t, client.Del(ctx, userKey, getUserAuthFenceKey(user.Id), getUserAuthVersionKey(user.Id)).Err())
	})
	_, err = GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, client.HDel(ctx, userKey, "CacheMAC", "CacheExpiresAt").Err())
	require.NoError(t, cacheIncrUserQuota(user.Id, -30))
	_, err = GetUserCache(user.Id)
	require.NoError(t, err)
	cachedUser, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(70), cachedUser.Quota)
	// Frozen predecessor quota scripts still accept the schema-2 signed hash.
	result, err := client.Eval(ctx, userQuotaDeltaScript, []string{userKey}, 5, user.Id, 2).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result)
	assert.Equal(t, "75", client.HGet(ctx, userKey, "Quota").Val())

	token := createReserveTestToken(t, 100)
	token.UserId = user.Id
	require.NoError(t, DB.Save(&token).Error)
	tokenKey := getTokenCacheKey(token.Key)
	t.Cleanup(func() {
		assert.NoError(t, client.Del(ctx, tokenKey, getTokenCacheFenceKey(token.Key), getTokenCacheGenerationKey(token.Key)).Err())
	})
	_, err = GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	require.NoError(t, client.HDel(ctx, tokenKey, "CacheMAC", "CacheExpiresAt").Err())
	observed, err := client.HGetAll(ctx, tokenKey).Result()
	require.NoError(t, err)
	_, err = cacheApplyTokenQuotaDelta(token.Id, token.Key, -20)
	require.NoError(t, err)
	_, err = repairTokenCache(token, generation, observed)
	require.NoError(t, err)
	cachedToken, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 80, cachedToken.RemainQuota)
	assert.Equal(t, 20, cachedToken.UsedQuota)

	// HINCRBY validates canonical signed int64 without Lua's double rounding.
	for _, counter := range []string{"-9223372036854775808", "9223372036854775807", "-9", "0", "01", "+1", "-0", "1e3", "9223372036854775808"} {
		t.Run(counter, func(t *testing.T) {
			require.NoError(t, client.HDel(ctx, tokenKey, "CacheMAC").Err())
			require.NoError(t, client.HSet(ctx, tokenKey, "RemainQuota", counter).Err())
			observed, err := client.HGetAll(ctx, tokenKey).Result()
			require.NoError(t, err)
			_, err = repairTokenCache(token, generation, observed)
			switch counter {
			case "-9223372036854775808", "9223372036854775807", "-9", "0":
				require.NoError(t, err)
				_, err = cacheGetTokenByKey(token.Key)
				require.NoError(t, err)
			default:
				require.ErrorIs(t, err, errAuthCacheUnsafeCounters)
				assert.False(t, client.HExists(ctx, tokenKey, "CacheMAC").Val())
			}
			assert.Equal(t, counter, client.HGet(ctx, tokenKey, "RemainQuota").Val())
		})
	}

	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	session := sessionIntegrityFixture()
	session.SID, session.UserID, session.TenantID = common.GetUUID(), user.Id, user.TenantID
	require.NoError(t, DB.Create(&session).Error)
	sessionKey := userSessionCacheKey(session.SID)
	t.Cleanup(func() {
		assert.NoError(t, client.Del(ctx, sessionKey).Err())
		assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error)
	})
	require.NoError(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()))
	require.NoError(t, client.HDel(ctx, sessionKey, "CacheMAC", "CacheExpiresAt").Err())
	loadedSession, err := GetUserSessionCached(session.SID)
	require.NoError(t, err)
	assert.Equal(t, session.SID, loadedSession.SID)
	var legacySession struct {
		SID         string
		TenantID    int64
		Status      string
		CacheSchema int
	}
	require.NoError(t, common.RedisHGetObj(sessionKey, &legacySession))
	assert.Equal(t, UserSessionStatusActive, legacySession.Status)
	assert.Equal(t, 2, legacySession.CacheSchema)
	// A pre-commit old-writer fence must remain effective after the candidate
	// has signed the hash; no cache clearing is needed to return to the old app.
	require.NoError(t, client.HSet(ctx, sessionKey, "Status", UserSessionStatusRevoking).Err())
	_, err = GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, errUserSessionCachePending)
	require.ErrorIs(t, writeUserSessionCache(session.cacheEntry(), time.Now().Add(time.Minute)), errUserSessionCachePending)
	assert.Equal(t, UserSessionStatusRevoking, client.HGet(ctx, sessionKey, "Status").Val())
}
