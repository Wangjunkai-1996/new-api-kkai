package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenCacheIntegrityRepairsMetadataPreservingLiveQuota(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	token.TenantID = 42
	token.Group, token.AutoGroups = "auto", `["default"]`
	require.NoError(t, DB.Save(&token).Error)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	ctx, key := context.Background(), getTokenCacheKey(token.Key)
	for _, field := range []string{"CacheMAC", "Group", "AutoGroups", "Status", "UnlimitedQuota", "AllowUserQuotaQuery", "ModelLimits", "AllowIps"} {
		t.Run(field, func(t *testing.T) {
			// Legacy writers do not know the MAC fields. Their metadata writes
			// must cause a database read, while pending debits/refunds survive.
			require.NoError(t, common.RDB.HSet(ctx, key, "RemainQuota", "-7", "UsedQuota", "107").Err())
			if field == "CacheMAC" {
				require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
			} else {
				require.NoError(t, common.RDB.HSet(ctx, key, field, "tampered").Err())
			}
			_, err := cacheGetTokenByKey(token.Key)
			require.Error(t, err)
			loaded, err := GetTokenByKey(token.Key, false)
			require.NoError(t, err)
			assert.Equal(t, token.Group, loaded.Group)
			cached, err := cacheGetTokenByKey(token.Key)
			require.NoError(t, err)
			assert.Equal(t, token.TenantID, cached.TenantID)
			assert.Equal(t, token.AutoGroups, cached.AutoGroups)
			assert.Equal(t, -7, cached.RemainQuota)
			assert.Equal(t, 107, cached.UsedQuota)
			assert.Equal(t, token.Status, cached.Status)
			assert.Equal(t, token.AllowUserQuotaQuery, cached.AllowUserQuotaQuery)
		})
	}

	// An old binary still reads the same hash and counters after repair/abort.
	var legacy struct {
		Id          int
		UserId      int
		RemainQuota int
		UsedQuota   int
		Group       string
	}
	require.NoError(t, common.RedisHGetObj(key, &legacy))
	assert.Equal(t, token.Id, legacy.Id)
	assert.Equal(t, -7, legacy.RemainQuota)
	assert.Equal(t, token.Group, legacy.Group)
}

func TestTokenCacheIntegrityRetryFailureDoesNotBypassMutationGuard(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	ctx, key := context.Background(), getTokenCacheKey(token.Key)
	require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC").Err())
	queries := 0
	const callback = "test:token_integrity_retry_failure"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			queries++
			if queries == 1 {
				require.NoError(t, common.RDB.HSet(ctx, key, "Group", "concurrent-writer").Err())
			}
		}
	}))
	t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })
	conflicted := false
	common.RDB.AddHook(authCacheRetryFailureHook{afterEval: func(command redis.Cmder) {
		if reply, ok := command.(*redis.Cmd).Val().(int64); ok && reply == -1 {
			conflicted = true
			require.NoError(t, server.Set(getTokenCacheFenceKey(token.Key), "1"))
			server.Close()
		}
	}})
	loaded, err := GetTokenByKey(token.Key, false)
	require.Error(t, err)
	assert.True(t, conflicted)
	assert.Nil(t, loaded)
	assert.Equal(t, 1, queries)
}

func TestTokenCacheIntegrityGuardFailureDoesNotReturnRevokedSnapshot(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.NoError(t, common.RDB.HDel(context.Background(), getTokenCacheKey(token.Key), "CacheMAC").Err())
	const callback = "test:token_integrity_revoke_after_read"
	injected := false
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "tokens" || injected {
			return
		}
		injected = true
		token.Status = common.TokenStatusDisabled
		require.NoError(t, token.SelectUpdate())
		assert.True(t, server.Exists(getTokenCacheFenceKey(token.Key)))
		server.Close()
	}))
	t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })
	loaded, err := GetTokenByKey(token.Key, false)
	require.Error(t, err)
	assert.Nil(t, loaded, "the SQL snapshot predates the committed revocation")
	assert.True(t, injected)
	assert.Equal(t, common.TokenStatusDisabled, getTokenFromDB(t, token.Id).Status)
}

func TestTokenCacheIntegrityBindsKeyAndAbsoluteExpiry(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	ctx, key := context.Background(), getTokenCacheKey(token.Key)
	fields, err := common.RDB.HGetAll(ctx, key).Result()
	require.NoError(t, err)
	require.NoError(t, common.RDB.HSet(ctx, getTokenCacheKey("different-key"), fields).Err())
	_, err = cacheGetTokenByKey("different-key")
	require.ErrorIs(t, err, errAuthCacheIntegrity)

	token.CacheExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	mac, err := token.cacheMAC()
	require.NoError(t, err)
	require.NoError(t, common.RDB.HSet(ctx, key, "CacheMAC", mac, "CacheExpiresAt", token.CacheExpiresAt).Err())
	require.NoError(t, common.RDB.Expire(ctx, key, time.Hour).Err())
	_, err = cacheGetTokenByKey(token.Key)
	require.ErrorIs(t, err, errAuthCacheIntegrity)
	_, err = GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	_, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
}

func TestTokenCacheIntegrityRepairRejectsSameMACMetadataRace(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	ctx, key := context.Background(), getTokenCacheKey(token.Key)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	observed, err := common.RDB.HGetAll(ctx, key).Result()
	require.NoError(t, err)
	require.NoError(t, common.RDB.HSet(ctx, key, "Group", "old-writer-new-group").Err())
	beforeTTL, err := common.RDB.PTTL(ctx, key).Result()
	require.NoError(t, err)
	_, err = repairTokenCache(token, generation, observed)
	require.ErrorIs(t, err, errAuthCacheSnapshotChanged)
	assert.Equal(t, "old-writer-new-group", common.RDB.HGet(ctx, key, "Group").Val())
	assert.LessOrEqual(t, common.RDB.PTTL(ctx, key).Val(), beforeTTL)

	// A delta after the observation is allowed; the script validates and
	// preserves the current counter instead of copying its old observed value.
	observed, err = common.RDB.HGetAll(ctx, key).Result()
	require.NoError(t, err)
	_, err = cacheApplyTokenQuotaDelta(token.Id, token.Key, -35)
	require.NoError(t, err)
	_, err = repairTokenCache(token, generation, observed)
	require.NoError(t, err)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 65, cached.RemainQuota)
	assert.Equal(t, 35, cached.UsedQuota)
}

func TestTokenCacheIntegrityRejectsUnsafeCounterOwnership(t *testing.T) {
	for _, tc := range []struct{ field, value string }{
		{"Id", "999999"}, {"UserId", "999999"}, {"TenantID", "99"},
		{"RemainQuota", "9223372036854775808"}, {"UsedQuota", "-9223372036854775809"},
		{"AccessedTime", "not-an-integer"}, {"RemainQuota", ""},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			truncateTables(t)
			useUserCacheMiniRedis(t)
			token := createReserveTestToken(t, 100)
			_, err := GetTokenByKey(token.Key, false)
			require.NoError(t, err)
			ctx, key := context.Background(), getTokenCacheKey(token.Key)
			require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC").Err())
			if tc.value == "" {
				require.NoError(t, common.RDB.HDel(ctx, key, tc.field).Err())
			} else {
				require.NoError(t, common.RDB.HSet(ctx, key, tc.field, tc.value).Err())
			}
			before, err := common.RDB.HGetAll(ctx, key).Result()
			require.NoError(t, err)
			_, err = GetTokenByKey(token.Key, false)
			require.ErrorIs(t, err, errAuthCacheUnsafeCounters)
			after, err := common.RDB.HGetAll(ctx, key).Result()
			require.NoError(t, err)
			assert.Equal(t, before, after, "unsafe hashes must not be signed, reset, or deleted")
		})
	}
}
