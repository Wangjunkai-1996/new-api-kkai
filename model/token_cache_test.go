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

func TestTokenCacheStaleSnapshotsCannotRestoreRevokedToken(t *testing.T) {
	for _, deleted := range []bool{true, false} {
		name := "disabled"
		if deleted {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			token := createReserveTestToken(t, 100)
			generation, err := beginTokenCacheRead(token.Key)
			require.NoError(t, err)
			stale := token
			if deleted {
				require.NoError(t, DeleteTokenById(token.Id, token.UserId))
			} else {
				token.Status = common.TokenStatusDisabled
				require.NoError(t, token.SelectUpdate())
			}

			// Slow readers remain invalid even after the old mutation fence expires.
			server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
			require.ErrorIs(t, cacheSetToken(stale, generation), errTokenCacheSnapshotStale)
			code, err := cacheInitToken(stale, generation)
			require.NoError(t, err)
			assert.Zero(t, code)
			_, err = ValidateUserToken(token.Key)
			require.ErrorIs(t, err, ErrTokenInvalid)

			// A disabled token may now have a fresh hash. Settlement must not
			// replace its authorization fields with the enabled snapshot.
			require.ErrorIs(t, cacheSetToken(stale, generation), errTokenCacheSnapshotStale)
			_, err = ValidateUserToken(token.Key)
			assert.ErrorIs(t, err, ErrTokenInvalid)
		})
	}
}

func TestTokenCacheColdReadCannotOverwriteSettledQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	stale := token

	token.RemainQuota, token.UsedQuota = 70, 30
	require.NoError(t, DB.Model(&token).Select("remain_quota", "used_quota").Updates(&token).Error)
	require.NoError(t, cacheSetToken(token, generation))
	code, err := cacheInitToken(stale, generation)
	require.NoError(t, err)
	assert.Zero(t, code, "a cold reader from before settlement must not publish old quota")

	loaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 70, loaded.RemainQuota)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 70, cached.RemainQuota)
	assert.Equal(t, 30, cached.UsedQuota)

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -20)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 50, cached.RemainQuota, "cold initialization must preserve an existing atomic reservation")
	assert.Equal(t, 50, cached.UsedQuota)
}

func TestTokenCacheExpiredReadGenerationCannotPublishSnapshot(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	freshGeneration, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	code, err := cacheInitToken(token, generation)
	require.NoError(t, err)
	assert.Zero(t, code)
	code, err = cacheInitToken(token, freshGeneration)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
}

func TestTaskBillingCacheReconcileUpdatesWarmTokenQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	task := &Task{UserId: token.UserId}
	task.PrivateData.TokenId = token.Id
	insertTask(t, task)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"remain_quota": 40,
		"used_quota":   60,
	}).Error)
	require.NoError(t, ReconcileTaskBillingQuotaCache(context.Background(), task.ID))
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 40, cached.RemainQuota)
	assert.Equal(t, 60, cached.UsedQuota)
	assert.Equal(t, common.TokenStatusEnabled, cached.Status)
}

func TestTokenCacheGenerationFailureFallsBackToDatabase(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	server.Close()
	loaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, token.Id, loaded.Id)
}

func TestTokenCacheStaleReconciliationCannotRestoreEditedLimit(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	stale := token

	token.RemainQuota = 0
	require.NoError(t, token.Update())
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	_, err = ValidateUserToken(token.Key)
	require.ErrorIs(t, err, ErrTokenInvalid)
	require.ErrorIs(t, cacheSetToken(stale, generation), errTokenCacheSnapshotStale)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Zero(t, cached.RemainQuota)
	_, err = ValidateUserToken(token.Key)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestTaskBillingCacheReconcileRetriesConflictingSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	generation, err := beginTokenCacheRead(token.Key)
	require.NoError(t, err)
	stale := token
	token.RemainQuota, token.UsedQuota = 40, 60
	require.NoError(t, DB.Model(&token).Select("remain_quota", "used_quota").Updates(&token).Error)
	require.NoError(t, cacheSetToken(stale, generation))
	// Both reconciliations read the same generation. The second must request
	// retry instead of silently dropping its newer committed quota.
	require.ErrorIs(t, cacheSetToken(token, generation), errTokenCacheSnapshotStale)
	task := &Task{UserId: token.UserId}
	task.PrivateData.TokenId = token.Id
	insertTask(t, task)
	require.NoError(t, ReconcileTaskBillingQuotaCache(context.Background(), task.ID))
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 40, cached.RemainQuota)
	assert.Equal(t, 60, cached.UsedQuota)
}

func TestTokenCacheMutationCommitRevokesReadersAfterFenceExpires(t *testing.T) {
	for _, action := range []string{"delete", "batch-delete", "disable", "limit", "group"} {
		t.Run(action, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			token := createReserveTestToken(t, 100)
			stale := token
			_, err := GetTokenByKey(token.Key, false)
			require.NoError(t, err)
			var lateGeneration string
			hook := func(tx *gorm.DB) {
				if tx.Statement.Table != "tokens" {
					return
				}
				// Simulate a metadata write that outlives its fence. A reader
				// can see the old DB state until the write commits.
				server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
				lateGeneration, err = beginTokenCacheRead(token.Key)
				require.NoError(t, err)
				code, cacheErr := cacheInitToken(stale, lateGeneration)
				require.NoError(t, cacheErr)
				require.Equal(t, 1, code)
			}
			const callbackName = "test:token_cache_slow_metadata_write"
			if action == "delete" || action == "batch-delete" {
				require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(callbackName, hook))
				t.Cleanup(func() { _ = DB.Callback().Delete().Remove(callbackName) })
			} else {
				require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, hook))
				t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })
			}
			switch action {
			case "delete":
				require.NoError(t, token.Delete())
			case "batch-delete":
				count, deleteErr := BatchDeleteTokens([]int{token.Id}, token.UserId)
				require.NoError(t, deleteErr)
				assert.Equal(t, 1, count)
			case "disable":
				token.Status = common.TokenStatusDisabled
				require.NoError(t, token.SelectUpdate())
			case "limit":
				token.RemainQuota = 0
				require.NoError(t, token.Update())
			case "group":
				require.NoError(t, token.UpdateGroup("new-group", nil))
			}
			_, err = cacheGetTokenByKey(token.Key)
			require.Error(t, err, "commit must evict the snapshot published after the pre-write fence expired")
			code, err := cacheInitToken(stale, lateGeneration)
			require.NoError(t, err)
			assert.Zero(t, code)
			if action == "group" {
				fresh, readErr := GetTokenByKey(token.Key, false)
				require.NoError(t, readErr)
				assert.Equal(t, "new-group", fresh.Group)
			} else {
				_, authErr := ValidateUserToken(token.Key)
				assert.ErrorIs(t, authErr, ErrTokenInvalid)
			}
		})
	}
}

func TestTokenCachePostCommitFailureReportsCommittedMutation(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	const callbackName = "test:token_cache_post_commit_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			server.Close()
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })
	token.Status = common.TokenStatusDisabled
	err := token.SelectUpdate()
	require.ErrorContains(t, err, "database mutation committed but cache invalidation failed")
	stored := getTokenFromDB(t, token.Id)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
}
