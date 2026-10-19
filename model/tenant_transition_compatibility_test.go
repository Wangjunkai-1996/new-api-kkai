package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantTransitionLegacyQuotaCachePreservesCreditsAndRefunds(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	ctx := context.Background()
	key := getUserCacheKey(user.Id)
	// Schema 11 (55744a4ed) writes these fields without TenantID. Its quota
	// reserve/delta Lua is unchanged, but always passes cache schema 2.
	require.NoError(t, common.RDB.HSet(ctx, key, map[string]any{
		"Id": user.Id, "Group": user.Group, "Status": user.Status,
		"Role": user.Role, "Username": user.Username, "Quota": 100,
		"AuthVersion": user.AuthVersion, "CacheSchema": 2,
	}).Err())
	_, err := cacheGetUserBase(user.Id)
	require.Error(t, err, "a missing tenant must force an authoritative lookup")
	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, PlatformTenantID, loaded.TenantID)

	require.NoError(t, DB.Model(&user).Update("quota", gorm.Expr("quota + ?", 15)).Error)
	legacyCredit, err := common.RDB.Eval(ctx, userQuotaDeltaScript, []string{key}, 15, user.Id, 2).Int()
	require.NoError(t, err)
	require.Equal(t, 1, legacyCredit, "old credits must not silently skip a new hash")
	legacyReserved, err := common.RDB.Eval(ctx, userQuotaReserveScript, []string{key}, 20, user.Id, 2).Int()
	require.NoError(t, err)
	require.Equal(t, 1, legacyReserved, "old binaries must accept the new hash")
	require.NoError(t, DB.Model(&user).Update("quota", gorm.Expr("quota - ?", 20)).Error)
	reserved, err := TryReserveUserQuota(user.Id, 30)
	require.NoError(t, err)
	require.True(t, reserved)

	// A legacy fill must keep the new ownership field and both in-flight
	// debits. The old writer uses HSET, and only initializes missing Quota.
	require.NoError(t, common.RDB.HSet(ctx, key, "CacheSchema", 2).Err())
	_, err = cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&user).Update("quota", gorm.Expr("quota + ?", 10)).Error)
	require.NoError(t, cacheIncrUserQuota(user.Id, 10))
	loaded, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(75), loaded.Quota)
	assert.Equal(t, int64(75), getUserQuotaFromDB(t, user.Id))
}

func TestTenantTransitionLegacyCacheCannotBypassAuthVersionFence(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	nextVersion := user.AuthVersion + 1
	require.NoError(t, SetUserAuthVersionFence(user.Id, nextVersion))
	// Simulate the last old snapshot arriving before the pending fence.
	require.NoError(t, common.RDB.HSet(context.Background(), getUserCacheKey(user.Id), "CacheSchema", 2).Err())
	_, err := cacheGetUserBase(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending)
	require.ErrorIs(t, populateUserCache(user), ErrUserAuthCachePending)
	require.NoError(t, DB.Model(&user).Update("auth_version", nextVersion).Error)
	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, nextVersion, loaded.AuthVersion)
	assert.Equal(t, PlatformTenantID, loaded.TenantID)
}

func TestTenantTransitionLegacySessionCacheFallsBackAndRetainsDenyFence(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	session := UserSession{SID: "transition-legacy-session", UserID: user.Id,
		Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: "refresh-hash", LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, CreateUserSession(&session))
	t.Cleanup(func() { DB.Where("sid = ?", session.SID).Delete(&UserSession{}) })
	ctx := context.Background()
	key := userSessionCacheKey(session.SID)
	require.NoError(t, common.RDB.HDel(ctx, key, "TenantID").Err())
	require.NoError(t, common.RDB.HSet(ctx, key, "CacheSchema", 1).Err())
	loaded, err := GetUserSessionCached(session.SID)
	require.NoError(t, err)
	assert.Equal(t, PlatformTenantID, loaded.TenantID)
	assert.Equal(t, session.Version, loaded.Version)

	// A legacy revoke fence precedes its database commit. Although the new
	// reader reloads the old-format hash from DB, it must not reauthorize it.
	require.NoError(t, common.RDB.HSet(ctx, key, "CacheSchema", 1, "Status", UserSessionStatusRevoking).Err())
	require.NoError(t, common.RDB.HDel(ctx, key, "TenantID").Err())
	_, err = GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, ErrUserSessionInactive)
}

func TestTenantTransitionLegacyGORMWritesKeepNewSnapshots(t *testing.T) {
	truncateTables(t)
	// Frozen schema-11 write projections: these omit every new tenant/cash
	// field and therefore exercise database defaults, not application hooks.
	legacyUser := struct {
		Id          int
		Username    string
		Password    string
		Email       string
		Quota       int64
		UsedQuota   int64
		AuthVersion int64
	}{Username: "legacy-transition", Password: "hash", Email: "legacy@example.com", Quota: 100, UsedQuota: 20, AuthVersion: 1}
	require.NoError(t, DB.Table("users").Create(&legacyUser).Error)
	require.NoError(t, DB.Table("users").Where("id = ?", legacyUser.Id).Updates(map[string]any{"quota": 80, "used_quota": 40}).Error)
	var user User
	require.NoError(t, DB.First(&user, legacyUser.Id).Error)
	assert.Equal(t, PlatformTenantID, user.TenantID)
	assert.Nil(t, user.NormalizedUsername)
	assert.Equal(t, int64(80), user.Quota)
	assert.Equal(t, int64(40), user.UsedQuota)
	legacyTopUp := struct {
		Id      int
		UserId  int
		Amount  int64
		TradeNo string
		Status  string
	}{UserId: user.Id, Amount: 100, TradeNo: "legacy-transition-order", Status: "pending"}
	require.NoError(t, DB.Table("top_ups").Create(&legacyTopUp).Error)
	var topup TopUp
	require.NoError(t, DB.First(&topup, legacyTopUp.Id).Error)
	assert.Equal(t, PlatformTenantID, topup.TenantID)
	assert.Zero(t, topup.PaidMinor)
	assert.Empty(t, topup.Currency)
	require.NoError(t, DB.Table("users").First(&legacyUser, user.Id).Error)
	assert.Equal(t, int64(80), legacyUser.Quota, "extra tenant columns must not break old reads")
}
