package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenCreateSnapshotsOwnerTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:token-tenant-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}))

	owner := User{Username: "tenant-token-owner", TenantID: 42, AuthVersion: 1}
	require.NoError(t, db.Create(&owner).Error)
	token := Token{UserId: owner.Id, Key: "tenant-token-key", Status: 1, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	assert.Equal(t, int64(42), token.TenantID)
}

func TestUpdateGroupInvalidatesTokenCache(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	token.Group = "default"
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("group", token.Group).Error)

	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	assert.Equal(t, "default", loaded.Group)
	require.True(t, server.Exists(getTokenCacheKey(token.Key)))

	retryDisabled := false
	require.NoError(t, loaded.UpdateGroup("vip", &retryDisabled))
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, "vip", fresh.Group)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, "vip", cached.Group)
}

func TestUpdateGroupDoesNotOverwriteStaleSnapshotFields(t *testing.T) {
	truncateTables(t)
	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"remain_quota":      42,
		"cross_group_retry": true,
	}).Error)
	require.NoError(t, loaded.UpdateGroup("vip", nil))

	stored := getTokenFromDB(t, token.Id)
	assert.Equal(t, "vip", stored.Group)
	assert.Equal(t, 42, stored.RemainQuota)
	assert.True(t, stored.CrossGroupRetry)
}

func TestUpdateGroupFailsClosedWhenCacheFenceCannotBeWritten(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 100)
	server.Close()

	retryDisabled := false
	err := token.UpdateGroup("vip", &retryDisabled)
	assert.Error(t, err)
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, "", stored.Group)
	assert.False(t, stored.CrossGroupRetry)
}
