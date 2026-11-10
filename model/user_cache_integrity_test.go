package model

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserCacheIntegrityRepairPreservesLiveQuota(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	ctx := context.Background()
	key := getUserCacheKey(user.Id)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, cacheIncrUserQuota(user.Id, -70))
	// An old writer can replace metadata while retaining the newer MAC fields.
	require.NoError(t, common.RDB.HSet(ctx, key, "Role", common.RoleAdminUser, "Group", "stale-group").Err())

	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, user.Role, loaded.Role)
	assert.Equal(t, user.Group, loaded.Group)
	assert.Equal(t, int64(30), loaded.Quota)
	assert.Equal(t, int64(100), getUserQuotaFromDB(t, user.Id))
	_, err = cacheGetUserBase(user.Id)
	require.NoError(t, err)

	// Quota-only reconciliation must remain independent of the signed fields.
	mac := loaded.CacheMAC
	require.NoError(t, updateUserQuotaCache(user.Id, 45))
	loaded, err = cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(45), loaded.Quota)
	assert.Equal(t, mac, loaded.CacheMAC)
}

func TestUserCacheMetadataRefreshReadsCompleteDatabaseState(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DB.Model(&user).Updates(map[string]any{
		"tenant_id": 42, "group": "authoritative-group", "setting": `{"language":"en"}`,
	}).Error)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, cacheIncrUserQuota(user.Id, -20))

	require.NoError(t, updateUserCache(User{Id: user.Id}))
	require.NoError(t, updateUserSettingCache(user.Id, `{"language":"stale"}`))
	require.NoError(t, updateUserNameCache(user.Id, "stale-name"))
	loaded, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(42), loaded.TenantID)
	assert.Equal(t, "authoritative-group", loaded.Group)
	assert.Equal(t, user.Username, loaded.Username)
	assert.Equal(t, `{"language":"en"}`, loaded.Setting)
	assert.Equal(t, user.Status, loaded.Status)
	assert.Equal(t, int64(80), loaded.Quota)
}

func TestUserCacheRepairConvergesAfterSameVersionMetadataChange(t *testing.T) {
	for _, oldWriterUpdatesRedis := range []bool{false, true} {
		t.Run(strconv.FormatBool(oldWriterUpdatesRedis), func(t *testing.T) {
			truncateTables(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			ctx := context.Background()
			key := getUserCacheKey(user.Id)
			require.NoError(t, populateUserCache(user))
			require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
			moved := false
			callback := "test:user_cache_same_version:" + t.Name()
			require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if moved || tx.Statement.Table != "users" {
					return
				}
				moved = true
				require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "new-group").Error)
				require.NoError(t, cacheIncrUserQuota(user.Id, -25))
				if oldWriterUpdatesRedis {
					// Keep AuthVersion and the MAC exactly as observed by the repair.
					require.NoError(t, common.RDB.HSet(ctx, key, "Group", "new-group").Err())
				}
			}))
			t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })

			loaded, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.True(t, moved)
			assert.Equal(t, "new-group", loaded.Group)
			assert.Equal(t, user.AuthVersion, loaded.AuthVersion)
			assert.Equal(t, int64(75), loaded.Quota)
			cached, err := cacheGetUserBase(user.Id)
			require.NoError(t, err)
			assert.Equal(t, loaded.Group, cached.Group)
		})
	}
}

func TestUserCacheRepairRejectsUnboundOrMalformedCounters(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		field string
		value any
		omit  bool
	}{
		{name: "wrong user", field: "Id", value: -1},
		{name: "wrong tenant", field: "TenantID", value: 42},
		{name: "missing tenant", field: "TenantID", omit: true},
		{name: "missing quota", field: "Quota", omit: true},
		{name: "fractional quota", field: "Quota", value: "1.5"},
		{name: "exponent quota", field: "Quota", value: "1e3"},
		{name: "overflow quota", field: "Quota", value: "9223372036854775808"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			truncateTables(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			ctx := context.Background()
			key := getUserCacheKey(user.Id)
			require.NoError(t, populateUserCache(user))
			require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
			if scenario.omit {
				require.NoError(t, common.RDB.HDel(ctx, key, scenario.field).Err())
			} else {
				require.NoError(t, common.RDB.HSet(ctx, key, scenario.field, scenario.value).Err())
			}
			before, err := common.RDB.HGetAll(ctx, key).Result()
			require.NoError(t, err)
			_, err = GetUserCache(user.Id)
			require.ErrorIs(t, err, errAuthCacheUnsafeCounters)
			after, err := common.RDB.HGetAll(ctx, key).Result()
			require.NoError(t, err)
			assert.Equal(t, before, after, "failed repair must not replace counters or sign a mismatched identity")
		})
	}
}

func TestUserCacheRepairPreservesSignedInt64Quota(t *testing.T) {
	for _, quota := range []int64{-1, math.MinInt64, math.MaxInt64} {
		t.Run(strconv.FormatInt(quota, 10), func(t *testing.T) {
			truncateTables(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			ctx := context.Background()
			key := getUserCacheKey(user.Id)
			require.NoError(t, populateUserCache(user))
			require.NoError(t, common.RDB.HSet(ctx, key, "Quota", strconv.FormatInt(quota, 10)).Err())
			require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
			loaded, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, quota, loaded.Quota)
			stored, err := common.RDB.HGet(ctx, key, "Quota").Result()
			require.NoError(t, err)
			assert.Equal(t, strconv.FormatInt(quota, 10), stored)
		})
	}
}

func TestUserCacheFailedDatabaseRecheckDiscardsOnlySignature(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	ctx := context.Background()
	key := getUserCacheKey(user.Id)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
	require.NoError(t, cacheIncrUserQuota(user.Id, -40))
	initialTTL := server.TTL(key)
	queries := 0
	injected := errors.New("database recheck unavailable")
	callback := "test:user_cache_failed_recheck"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		queries++
		if queries == 2 {
			server.FastForward(250 * time.Millisecond)
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })

	_, err := GetUserCache(user.Id)
	require.ErrorIs(t, err, injected)
	exists, err := common.RDB.HExists(ctx, key, "CacheMAC").Result()
	require.NoError(t, err)
	assert.False(t, exists)
	quota, err := common.RDB.HGet(ctx, key, "Quota").Result()
	require.NoError(t, err)
	assert.Equal(t, "60", quota)
	assert.Less(t, server.TTL(key), initialTTL)
}

func TestUserCacheRepeatedMetadataRaceStopsWithoutPublishing(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	ctx := context.Background()
	key := getUserCacheKey(user.Id)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
	initialTTL := server.TTL(key)
	queries := 0
	callback := "test:user_cache_busy_metadata"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		queries++
		group := "changed-" + strconv.Itoa(queries)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", group).Error)
		require.NoError(t, common.RDB.HSet(ctx, key, "Group", group).Err())
		server.FastForward(100 * time.Millisecond)
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })

	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "changed-3", loaded.Group, "the bounded fallback re-reads committed database metadata")
	exists, err := common.RDB.HExists(ctx, key, "CacheMAC").Result()
	require.NoError(t, err)
	assert.False(t, exists, "ongoing old-writer changes must not acquire a new signature")
	assert.Less(t, server.TTL(key), initialTTL)
}

func TestUserCacheAbsoluteSignatureExpirySurvivesLegacyTTLRefresh(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	key := getUserCacheKey(user.Id)
	ctx := context.Background()
	loaded.CacheExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	mac, err := authCacheMAC(key, userCacheAuthMetadata(loaded))
	require.NoError(t, err)
	require.NoError(t, common.RDB.HSet(ctx, key, "CacheMAC", mac, "CacheExpiresAt", loaded.CacheExpiresAt).Err())
	require.NoError(t, common.RDB.Expire(ctx, key, time.Hour).Err())
	_, err = cacheGetUserBase(user.Id)
	require.ErrorIs(t, err, errAuthCacheIntegrity)
	_, err = GetUserCache(user.Id)
	require.NoError(t, err)
}

func TestUserCacheRedisFailureAfterDatabaseReadFailsClosed(t *testing.T) {
	for _, failAfterQuery := range []int{1, 2} {
		t.Run(strconv.Itoa(failAfterQuery), func(t *testing.T) {
			truncateTables(t)
			server := useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			require.NoError(t, populateUserCache(user))
			require.NoError(t, common.RDB.HDel(context.Background(), getUserCacheKey(user.Id), "CacheMAC", "CacheExpiresAt").Err())
			queries := 0
			callback := "test:user_cache_redis_guard_failure:" + t.Name()
			require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table != "users" {
					return
				}
				queries++
				if queries == failAfterQuery {
					// A restrictive transaction has published its pending version
					// after this request observed the old, still-active database row.
					require.NoError(t, SetUserAuthVersionFence(user.Id, user.AuthVersion+1))
					server.Close()
				}
			}))
			t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })

			loaded, err := GetUserCache(user.Id)
			require.Error(t, err)
			assert.Nil(t, loaded, "an unconfirmed post-read fence must not authorize the old snapshot")
		})
	}
}

func TestUserCacheRedisFailureBeforeObservationUsesDatabase(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	server.Close()
	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, user.Id, loaded.Id)
	assert.Equal(t, user.TenantID, loaded.TenantID)
	assert.Equal(t, int64(100), loaded.Quota)
}

type authCacheRetryFailureHook struct {
	afterEval func(redis.Cmder)
}

func (hook authCacheRetryFailureHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (hook authCacheRetryFailureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (hook authCacheRetryFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		err := next(ctx, command)
		if err == nil && command.Name() == "eval" {
			hook.afterEval(command)
		}
		return err
	}
}

func TestUserCacheRedisFailureAfterRepairConflictFailsClosed(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	ctx := context.Background()
	key := getUserCacheKey(user.Id)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, common.RDB.HDel(ctx, key, "CacheMAC", "CacheExpiresAt").Err())
	queries := 0
	const callback = "test:user_cache_retry_observation_failure"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			queries++
			if queries == 1 {
				// Same-version metadata from an old writer forces the first
				// repair to conflict without changing the database authority.
				require.NoError(t, common.RDB.HSet(ctx, key, "Group", "old-writer-change").Err())
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })
	conflicted := false
	common.RDB.AddHook(authCacheRetryFailureHook{afterEval: func(command redis.Cmder) {
		reply, err := command.(*redis.Cmd).Slice()
		require.NoError(t, err)
		require.Equal(t, int64(-1), reply[0], "the first guarded repair must actually detect the old writer")
		conflicted = true
		// The restriction is pending in Redis while SQL still has the old
		// version. A failed retry observation cannot downgrade its floor check.
		require.NoError(t, server.Set(getUserAuthFenceKey(user.Id), strconv.FormatInt(user.AuthVersion+1, 10)))
		server.Close()
	}})
	loaded, err := GetUserCache(user.Id)
	require.Error(t, err)
	assert.True(t, conflicted)
	assert.Nil(t, loaded)
	assert.Equal(t, 1, queries, "retry failure must not fall back to an unchecked SQL snapshot")
}
