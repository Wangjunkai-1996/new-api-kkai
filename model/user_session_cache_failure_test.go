package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserSessionCacheFailureAfterConcurrentRevokeRejectsSnapshot(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	session := UserSession{
		SID: "session-concurrent-revoke-cache-failure", TenantID: user.TenantID, UserID: user.Id,
		Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: "refresh-hash", LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&session).Error)
	t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
	key := userSessionCacheKey(session.SID)
	require.NoError(t, common.RedisHSetObj(key, session.cacheEntry(), time.Minute))

	// Complete a revocation after this reader obtained its active SQL
	// snapshot, then make its cache guard unavailable. The old code returned
	// that stale active snapshot after merely logging the Redis error.
	revoked := false
	const callback = "test:session_revoke_before_cache_guard"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "user_sessions" || tx.Error != nil || revoked {
			return
		}
		revoked = true
		now := time.Now().Unix()
		require.NoError(t, writeUserSessionDenyFence(&session, UserSessionStatusRevoking, now, "test_revoke"))
		require.NoError(t, DB.Model(&UserSession{}).Where("sid = ?", session.SID).Updates(map[string]any{
			"status": UserSessionStatusRevoked, "revoked_at": now, "revoked_reason": "test_revoke",
		}).Error)
		server.SetError("ERR session cache temporarily unavailable")
	}))
	t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })

	loaded, err := GetUserSessionCached(session.SID)

	require.True(t, revoked)
	require.ErrorContains(t, err, "session cache temporarily unavailable")
	assert.Nil(t, loaded)
	assert.NotErrorIs(t, err, ErrUserSessionInactive, "unknown guard outcome must not force logout")
	server.SetError("")
	status, err := common.RDB.HGet(context.Background(), key, "Status").Result()
	require.NoError(t, err)
	assert.Equal(t, UserSessionStatusRevoking, status)
	stored, err := GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, UserSessionStatusRevoked, stored.Status)
}

func TestUserSessionCacheFailureRejectsUnconfirmedIssuance(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	for _, operation := range []string{"create", "advance_auth_version"} {
		t.Run(operation, func(t *testing.T) {
			truncateTables(t)
			server := useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			session := UserSession{
				SID: "session-cache-failure-" + operation, TenantID: user.TenantID, UserID: user.Id,
				Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
				RefreshHash: "refresh-hash", LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix(),
			}
			if operation != "create" {
				require.NoError(t, DB.Create(&session).Error)
			}
			t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
			server.SetError("ERR session cache temporarily unavailable")
			var err error
			switch operation {
			case "create":
				err = CreateUserSession(&session)
			case "advance_auth_version":
				var advanced *UserSession
				advanced, err = AdvanceUserSessionAuthVersion(user.Id, session.SID, session.Version, session.UserAuthVersion, session.UserAuthVersion+1)
				assert.Nil(t, advanced)
			}
			require.ErrorContains(t, err, "session cache temporarily unavailable")
			assert.NotErrorIs(t, err, ErrUserSessionInactive)
		})
	}
}

func TestUserSessionCacheExpiredObservationCannotBypassPendingRevoke(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	session := UserSession{
		SID: "session-expired-observation-pending-revoke", TenantID: user.TenantID, UserID: user.Id,
		Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: "refresh-hash", LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&session).Error)
	t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
	snapshot, err := GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	// Model an old writer's pre-commit tombstone: SQL is still active, but
	// publication must not fall back to SQL after its observation expires.
	key := userSessionCacheKey(session.SID)
	deny := session.cacheEntry()
	deny.Status, deny.RevokedAt = UserSessionStatusRevoking, time.Now().Unix()
	require.NoError(t, common.RedisHSetObj(key, deny, time.Minute))
	before, err := common.RDB.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	ttl := server.TTL(key)

	err = publishCreatedUserSession(snapshot, time.Now().Add(-time.Second))

	require.ErrorIs(t, err, errUserSessionCacheObservationStale)
	assert.NotErrorIs(t, err, ErrUserSessionInactive)
	stored, err := GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, UserSessionStatusActive, stored.Status)
	after, err := common.RDB.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, ttl, server.TTL(key))
}

func TestUserSessionRefreshCacheFailurePreservesRefreshSecret(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	now := time.Now().Unix()
	nextHash := common.GenerateHMAC("next-refresh-secret")
	session := UserSession{
		SID: "session-refresh-cache-failure", TenantID: user.TenantID, UserID: user.Id,
		Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: common.GenerateHMAC("current-refresh-secret"), PreviousRefreshHash: common.GenerateHMAC("older-refresh-secret"),
		PreviousValidUntil: now - 30, LastActiveAt: now - 60,
		LoginMethod: "password", ExpiresAt: now + 3600,
	}
	require.NoError(t, DB.Create(&session).Error)
	t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
	server.SetError("ERR session cache temporarily unavailable")

	rotated, err := RotateUserSessionRefresh(user.Id, session.SID, session.RefreshHash, nextHash, now, 30*time.Second)

	require.ErrorContains(t, err, "session cache temporarily unavailable")
	assert.Nil(t, rotated)
	assert.NotErrorIs(t, err, ErrUserSessionInactive)
	stored, err := GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, session.RefreshHash, stored.RefreshHash)
	assert.Equal(t, session.PreviousRefreshHash, stored.PreviousRefreshHash)
	assert.Equal(t, session.PreviousValidUntil, stored.PreviousValidUntil)
	assert.Equal(t, session.LastActiveAt, stored.LastActiveAt)

	server.SetError("")
	rotated, err = RotateUserSessionRefresh(user.Id, session.SID, session.RefreshHash, nextHash, now, 30*time.Second)

	require.NoError(t, err, "the unchanged current secret must remain retryable after cache recovery")
	require.NotNil(t, rotated)
	assert.Equal(t, nextHash, rotated.RefreshHash)
	stored, err = GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, nextHash, stored.RefreshHash)
	assert.Equal(t, session.RefreshHash, stored.PreviousRefreshHash)
	assert.Equal(t, UserSessionStatusActive, stored.Status)
}

func TestUserSessionRefreshConcurrentCurrentSecretHasSingleWinner(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	useUserCacheMiniRedis(t)
	common.SyncFrequency = 60
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	now := time.Now().Unix()
	nextHash := common.GenerateHMAC("concurrent-next-refresh-secret")
	session := UserSession{
		SID: "session-concurrent-refresh", TenantID: user.TenantID, UserID: user.Id,
		Version: 1, UserAuthVersion: user.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: common.GenerateHMAC("concurrent-current-refresh-secret"),
		LoginMethod: "password", LastActiveAt: now - 60, ExpiresAt: now + 3600,
	}
	require.NoError(t, DB.Create(&session).Error)
	t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })

	originalDB := DB
	pool, err := DB.DB()
	require.NoError(t, err)
	previousMaxOpen := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	DB = originalDB.WithContext(ctx)
	readHashes := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var snapshots atomic.Int32
	const callback = "test:concurrent_session_refresh_snapshot"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "user_sessions" || tx.Error != nil {
			return
		}
		snapshot, ok := tx.Statement.Dest.(*UserSession)
		if !ok || snapshot.SID != session.SID || snapshots.Add(1) > 2 {
			return
		}
		readHashes <- snapshot.RefreshHash
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}))
	type refreshResult struct {
		session *UserSession
		err     error
	}
	results := make(chan refreshResult, 2)
	finished := make(chan struct{}, 2)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		deadline := time.NewTimer(6 * time.Second)
		defer deadline.Stop()
	waitForRefresh:
		for range 2 {
			select {
			case <-finished:
			case <-deadline.C:
				t.Error("concurrent refresh did not stop after cancellation")
				break waitForRefresh
			}
		}
		assert.NoError(t, originalDB.Callback().Query().Remove(callback))
		DB = originalDB
		pool.SetMaxOpenConns(previousMaxOpen)
	})
	for range 2 {
		go func() {
			defer func() { finished <- struct{}{} }()
			rotated, rotateErr := RotateUserSessionRefresh(user.Id, session.SID, session.RefreshHash, nextHash, now, 30*time.Second)
			results <- refreshResult{rotated, rotateErr}
		}()
	}
	for range 2 {
		select {
		case hash := <-readHashes:
			require.Equal(t, session.RefreshHash, hash)
		case <-ctx.Done():
			t.Fatal("both refresh requests did not reach the original snapshot")
		}
	}
	releaseOnce.Do(func() { close(release) })
	winners, races := 0, 0
	for range 2 {
		select {
		case result := <-results:
			switch {
			case result.err == nil:
				winners++
			case errors.Is(result.err, ErrUserSessionRefreshRace):
				races++
			default:
				require.NoError(t, result.err)
			}
			require.NotNil(t, result.session)
			assert.Equal(t, nextHash, result.session.RefreshHash)
		case <-ctx.Done():
			t.Fatal("concurrent refresh did not complete")
		}
	}
	assert.Equal(t, 1, winners)
	assert.Equal(t, 1, races)
	stored, err := GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, nextHash, stored.RefreshHash)
	assert.Equal(t, session.RefreshHash, stored.PreviousRefreshHash)
	assert.Equal(t, UserSessionStatusActive, stored.Status)
	cached, err := getUserSessionCache(session.SID)
	require.NoError(t, err, "the winning rotation must publish a valid signed session")
	assert.Equal(t, UserSessionStatusActive, cached.Status)
	assert.Equal(t, stored.Version, cached.Version)
	assert.Equal(t, stored.LastActiveAt, cached.LastActiveAt)
}
