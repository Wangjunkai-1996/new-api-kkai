package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sessionIntegrityFixture() UserSession {
	now := time.Now().Unix()
	return UserSession{
		SID: "session-integrity", TenantID: 42, UserID: 7,
		Version: 3, UserAuthVersion: 5, Status: UserSessionStatusActive,
		RefreshHash: "server-refresh-digest", LoginMethod: "password",
		IP: "127.0.0.1", UserAgent: "session-test",
		CreatedAt: now - 60, LastActiveAt: now, ExpiresAt: now + 3600,
	}
}

func TestUserSessionCacheIntegrityRejectsTamperedIdentityAndPermissions(t *testing.T) {
	for _, test := range []struct {
		field string
		value any
	}{
		{"TenantID", 99}, {"UserID", 8}, {"Version", 4}, {"UserAuthVersion", 6},
		{"Status", "inactive"}, {"RevokedAt", 1}, {"ExpiresAt", 1},
		{"CacheSchema", 1}, {"CacheMAC", "forged"},
		{"CacheExpiresAt", time.Now().Add(time.Hour).UnixMilli()},
	} {
		t.Run(test.field, func(t *testing.T) {
			useUserCacheMiniRedis(t)
			session := sessionIntegrityFixture()
			require.NoError(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()))
			cached, err := getUserSessionCache(session.SID)
			require.NoError(t, err)
			assert.Equal(t, session.TenantID, cached.TenantID)
			require.NoError(t, common.RDB.HSet(context.Background(), userSessionCacheKey(session.SID), test.field, test.value).Err())
			_, err = getUserSessionCache(session.SID)
			require.ErrorIs(t, err, errAuthCacheIntegrity)
			assert.NotErrorIs(t, err, ErrUserSessionInactive, "untrusted cache state is not proof that the login ended")
		})
	}
}

func TestUserSessionCacheIntegrityRejectsCopiedSession(t *testing.T) {
	useUserCacheMiniRedis(t)
	session := sessionIntegrityFixture()
	require.NoError(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()))
	fields, err := common.RDB.HGetAll(context.Background(), userSessionCacheKey(session.SID)).Result()
	require.NoError(t, err)
	fields["SID"] = "another-session"
	require.NoError(t, common.RDB.HSet(context.Background(), userSessionCacheKey(fields["SID"]), fields).Err())
	_, err = getUserSessionCache(fields["SID"])
	require.ErrorIs(t, err, errAuthCacheIntegrity)
}

func TestUserSessionCacheIntegrityPreservesDenyWithoutFalseLogout(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		legacy bool
		tamper bool
	}{
		{"legacy_revoking", UserSessionStatusRevoking, true, false},
		{"legacy_revoked", UserSessionStatusRevoked, true, false},
		{"tampered_revoking", UserSessionStatusRevoking, false, true},
		{"tampered_revoked", UserSessionStatusRevoked, false, true},
		{"signed_revoking", UserSessionStatusRevoking, false, false},
		{"signed_revoked", UserSessionStatusRevoked, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := useUserCacheMiniRedis(t)
			session := sessionIntegrityFixture()
			entry := session.cacheEntry()
			key := userSessionCacheKey(session.SID)
			if test.tamper {
				require.NoError(t, writeUserSessionCache(entry, userSessionCacheDeadline()))
				require.NoError(t, common.RDB.HSet(context.Background(), key, "Status", test.status).Err())
			} else {
				entry.Status, entry.RevokedAt = test.status, time.Now().Unix()
				require.NoError(t, writeUserSessionCache(entry, time.Time{}))
			}
			if test.legacy {
				require.NoError(t, common.RDB.HDel(context.Background(), key, "CacheMAC", "CacheExpiresAt", "TenantID").Err())
				require.NoError(t, common.RDB.HSet(context.Background(), key, "CacheSchema", 1).Err())
			}
			before, err := common.RDB.HGetAll(context.Background(), key).Result()
			require.NoError(t, err)
			ttl := server.TTL(key)
			_, err = GetUserSessionCached(session.SID)
			if !test.legacy && !test.tamper && test.status == UserSessionStatusRevoked {
				require.ErrorIs(t, err, ErrUserSessionInactive)
			} else {
				require.ErrorIs(t, err, errUserSessionCachePending)
				assert.NotErrorIs(t, err, ErrUserSessionInactive)
			}
			require.ErrorIs(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()), errUserSessionCachePending)
			after, err := common.RDB.HGetAll(context.Background(), key).Result()
			require.NoError(t, err)
			assert.Equal(t, before, after, "active repair must never replace a deny fence")
			assert.Equal(t, ttl, server.TTL(key), "refusal must not extend the tombstone")
		})
	}
}

func TestUserSessionCacheIntegrityAbsoluteDeadlineCannotBeExtended(t *testing.T) {
	useUserCacheMiniRedis(t)
	session := sessionIntegrityFixture()
	entry := session.cacheEntry()
	deadline := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	require.NoError(t, writeUserSessionCache(entry, deadline))
	assert.Equal(t, deadline.UnixMilli(), entry.CacheExpiresAt)

	// A valid but expired signature stays expired even when an old writer
	// extends the Redis TTL. No wall-clock sleep is needed to test this case.
	entry.CacheExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	mac, err := entry.cacheMAC()
	require.NoError(t, err)
	entry.CacheMAC = mac
	require.NoError(t, common.RedisHSetObj(userSessionCacheKey(session.SID), entry, time.Hour))
	_, err = getUserSessionCache(session.SID)
	require.ErrorIs(t, err, errAuthCacheIntegrity)
	assert.NotErrorIs(t, err, ErrUserSessionInactive)
}

func TestUserSessionLegacyDenyRecognizesCommittedRevocation(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	user := createReserveTestUser(t, 100)
	session := sessionIntegrityFixture()
	session.SID, session.UserID = "legacy-committed-revocation", user.Id
	session.TenantID, session.UserAuthVersion = user.TenantID, user.AuthVersion
	require.NoError(t, DB.Create(&session).Error)
	t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
	require.NoError(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()))
	key := userSessionCacheKey(session.SID)
	require.NoError(t, common.RDB.HSet(context.Background(), key, "Status", UserSessionStatusRevoked).Err())
	before, err := common.RDB.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	ttl := server.TTL(key)

	_, err = GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, errUserSessionCachePending, "an unsigned deny cannot prove that an active database session ended")
	require.NoError(t, DB.Model(&UserSession{}).Where("sid = ?", session.SID).Updates(map[string]any{
		"status": UserSessionStatusRevoked, "revoked_at": time.Now().Unix(),
	}).Error)
	_, err = GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, ErrUserSessionInactive, "a committed legacy logout must return normal authentication rejection")

	const callback = "test:legacy_deny_lookup_failure"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "user_sessions" {
			tx.AddError(errors.New("synthetic session lookup failure"))
		}
	}))
	t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })
	_, err = GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, errUserSessionCachePending, "unavailable authoritative state must not clear the login")
	after, err := common.RDB.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, ttl, server.TTL(key), "confirmation must not alter or renew the deny fence")
}

func TestUserSessionCacheIntegrityDeadlineIsBoundedBySessionExpiry(t *testing.T) {
	useUserCacheMiniRedis(t)
	session := sessionIntegrityFixture()
	session.ExpiresAt = time.Now().Add(time.Minute).Unix()
	entry := session.cacheEntry()
	require.NoError(t, writeUserSessionCache(entry, time.Now().Add(time.Hour)))
	assert.Equal(t, session.ExpiresAt*1000, entry.CacheExpiresAt)
	cached, err := getUserSessionCache(session.SID)
	require.NoError(t, err)
	assert.Equal(t, entry.CacheExpiresAt, cached.CacheExpiresAt)
}

func TestUserSessionCacheIntegrityRejectsDelayedWrites(t *testing.T) {
	server := useUserCacheMiniRedis(t)
	session := sessionIntegrityFixture()
	key := userSessionCacheKey(session.SID)
	entry := session.cacheEntry()
	require.NoError(t, writeUserSessionCache(entry, userSessionCacheDeadline()))
	// A legacy refresh can advance LastActiveAt without changing Version or
	// CacheMAC. A delayed snapshot must not overwrite it or renew that hash.
	require.NoError(t, common.RDB.HSet(context.Background(), key, "LastActiveAt", session.LastActiveAt+1).Err())
	ttl := server.TTL(key)
	require.ErrorIs(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()), errUserSessionCacheObservationStale)
	assert.Equal(t, ttl, server.TTL(key))
	lastActive, err := common.RDB.HGet(context.Background(), key, "LastActiveAt").Int64()
	require.NoError(t, err)
	assert.Equal(t, session.LastActiveAt+1, lastActive)

	require.NoError(t, common.RDB.HSet(context.Background(), key, "Version", session.Version+1).Err())
	require.ErrorIs(t, writeUserSessionCache(session.cacheEntry(), userSessionCacheDeadline()), errUserSessionCachePending)
	assert.Equal(t, ttl, server.TTL(key))

	server.FastForward(time.Hour)
	require.ErrorIs(t, writeUserSessionCache(session.cacheEntry(), time.Now().Add(-time.Second)), errUserSessionCacheObservationStale)
	assert.False(t, server.Exists(key), "a delayed fill must not regain a fresh observation window")
}

func TestUserSessionCacheIntegrityPostgresRepairsActiveSnapshots(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN; PostgreSQL session fallback was not run")
	}
	require.NoError(t, DB.AutoMigrate(&UserSession{}))
	for _, scenario := range []string{"unsigned", "wrong_tenant", "expired_signature", "forged_inactive", "forged_expiry"} {
		t.Run(scenario, func(t *testing.T) {
			truncateTables(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 100)
			session := sessionIntegrityFixture()
			session.SID, session.UserID = "session-integrity-"+scenario, user.Id
			session.TenantID, session.UserAuthVersion = user.TenantID, user.AuthVersion
			require.NoError(t, DB.Create(&session).Error)
			t.Cleanup(func() { assert.NoError(t, DB.Where("sid = ?", session.SID).Delete(&UserSession{}).Error) })
			entry := session.cacheEntry()
			require.NoError(t, writeUserSessionCache(entry, userSessionCacheDeadline()))
			key := userSessionCacheKey(session.SID)
			switch scenario {
			case "unsigned":
				require.NoError(t, common.RDB.HDel(context.Background(), key, "CacheMAC", "CacheExpiresAt").Err())
			case "wrong_tenant":
				require.NoError(t, common.RDB.HSet(context.Background(), key, "TenantID", 99).Err())
			case "expired_signature":
				entry.CacheExpiresAt = time.Now().Add(-time.Second).UnixMilli()
				mac, err := entry.cacheMAC()
				require.NoError(t, err)
				entry.CacheMAC = mac
				require.NoError(t, common.RedisHSetObj(key, entry, time.Hour))
			case "forged_inactive":
				require.NoError(t, common.RDB.HSet(context.Background(), key, "Status", "inactive").Err())
			case "forged_expiry":
				require.NoError(t, common.RDB.HSet(context.Background(), key, "ExpiresAt", 1).Err())
			}
			loaded, err := GetUserSessionCached(session.SID)
			require.NoError(t, err, "cache format or tampering must not end a valid database session")
			assert.Equal(t, session.TenantID, loaded.TenantID)
			assert.Equal(t, session.UserID, loaded.UserID)
			assert.Equal(t, session.ExpiresAt, loaded.ExpiresAt)
			cached, err := getUserSessionCache(session.SID)
			require.NoError(t, err, "repair must publish a verifiable database snapshot")
			assert.Equal(t, session.TenantID, cached.TenantID)
			assert.Equal(t, UserSessionStatusActive, cached.Status)
		})
	}
}

func TestUserSessionCacheIntegrityPostgresFailureDoesNotAcceptInvalidCache(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN; PostgreSQL session fallback was not run")
	}
	useUserCacheMiniRedis(t)
	session := sessionIntegrityFixture()
	key := userSessionCacheKey(session.SID)
	require.NoError(t, common.RedisHSetObj(key, session.cacheEntry(), time.Minute))
	databaseFailure := errors.New("session database unavailable")
	const callback = "test:session_integrity_database_failure"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "user_sessions" {
			tx.AddError(databaseFailure)
		}
	}))
	t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })
	loaded, err := GetUserSessionCached(session.SID)
	require.ErrorIs(t, err, databaseFailure)
	assert.Nil(t, loaded)
	assert.NotErrorIs(t, err, ErrUserSessionInactive)
	mac, err := common.RDB.HGet(context.Background(), key, "CacheMAC").Result()
	require.NoError(t, err)
	assert.Empty(t, mac, "failed authoritative lookup must not sign the invalid cache")
}
