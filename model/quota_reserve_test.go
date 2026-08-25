package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useUserCacheMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	oldSyncFrequency := common.SyncFrequency
	common.RedisEnabled = true
	common.SyncFrequency = 2
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
		common.SyncFrequency = oldSyncFrequency
	})
	return server
}

func createReserveTestUser(t *testing.T, quota int) User {
	t.Helper()
	user := User{
		Username: "reserve-user-" + common.GetRandomString(6),
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    int64(quota),
		AffCode:  "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func getUserQuotaFromDB(t *testing.T, id int) int64 {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func getTokenFromDB(t *testing.T, id int) Token {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int64)
		batchUpdateLocks[i].Unlock()
	}
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		for i := 0; i < BatchUpdateTypeCount; i++ {
			batchUpdateLocks[i].Lock()
			batchUpdateStores[i] = make(map[int]int64)
			batchUpdateLocks[i].Unlock()
		}
	})
}

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, int64(40), getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, int64(40), getUserQuotaFromDB(t, user.Id))

	token := createReserveTestToken(t, 80)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestRedisBatchReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, int64(10), getUserQuotaFromDB(t, user.Id), "batch delta is not flushed yet")

	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved, "stale DB balance must not authorize a second spend")
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), cachedUser.Quota)

	token := createReserveTestToken(t, 9)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	batchUpdate()
	assert.Equal(t, int64(2), getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestReserveFallsBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 20)
	require.NoError(t, populateUserCache(user))
	token := createReserveTestToken(t, 20)
	_, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	server.Close()

	// Redis 故障时降级为数据库条件更新：服务保持可用且不会超扣。
	reserved, err := TryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, int64(15), getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 5, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 15, reloadedToken.RemainQuota)
	assert.Equal(t, 5, reloadedToken.UsedQuota)

	// Redis 恢复后仍保存故障前的 20。缓存可以批准 18，但数据库的条件
	// 更新必须拒绝，并清理旧缓存，不能把数据库余额扣成负数。
	require.NoError(t, server.Restart())
	require.NoError(t, common.RDB.Ping(context.Background()).Err())
	reserved, err = TryReserveUserQuota(user.Id, 18)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, int64(15), getUserQuotaFromDB(t, user.Id))
	assert.False(t, server.Exists(getUserCacheKey(user.Id)))

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 18, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	reloadedToken = getTokenFromDB(t, token.Id)
	assert.Equal(t, 15, reloadedToken.RemainQuota)
	assert.Equal(t, 5, reloadedToken.UsedQuota)
	assert.False(t, server.Exists(getTokenCacheKey(token.Key)))

	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	reserved, err = TryReserveUserQuota(user.Id, 15)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 15, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Zero(t, getUserQuotaFromDB(t, user.Id))
	reloadedToken = getTokenFromDB(t, token.Id)
	assert.Zero(t, reloadedToken.RemainQuota)
	assert.Equal(t, 20, reloadedToken.UsedQuota)
}

func TestBatchReserveFailsClosedWhenRedisIsUnavailableBeforeFlush(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	token := createReserveTestToken(t, 9)
	_, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)

	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	require.True(t, reserved)
	assert.Equal(t, int64(10), getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	server.Close()
	reserved, err = TryReserveUserQuota(user.Id, 1)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, errBatchQuotaCacheUnavailable)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 1, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, errBatchQuotaCacheUnavailable)
	assert.Equal(t, int64(10), getUserQuotaFromDB(t, user.Id), "database remains stale until the queued delta is flushed")
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	// 恢复后缓存仍保存第一次预扣后的余额；故障期间的请求没有进入 batch。
	require.NoError(t, server.Restart())
	require.NoError(t, common.RDB.Ping(context.Background()).Err())
	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)

	batchUpdate()
	assert.Equal(t, int64(2), getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestSynchronousReserveCompensatesCacheWhenPersistenceFails(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DB.Delete(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 6)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cached, cacheErr := cacheGetUserBase(user.Id)
	require.NoError(t, cacheErr)
	assert.Equal(t, int64(10), cached.Quota)

	token := createReserveTestToken(t, 12)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, DB.Delete(&token).Error)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cachedToken, cacheErr := cacheGetTokenByKey(token.Key)
	require.NoError(t, cacheErr)
	assert.Equal(t, 12, cachedToken.RemainQuota)
	assert.Zero(t, cachedToken.UsedQuota)
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)

	// 已存在的哈希只刷新 TTL：数据库快照不得覆盖已被原子预扣的余额。
	code, err := cacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)

	// 变更期间：fence 删除缓存并拦截并发读者手中的过期快照。
	require.NoError(t, invalidateTokenCacheForMutation(token.Key))
	code, err = cacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// fence 过期后可重新从数据库水合。
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}
