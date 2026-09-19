package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	require.NoError(t, loaded.UpdateGroup("vip", true))
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
	require.NoError(t, loaded.UpdateGroup("vip", false))

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

	err := token.UpdateGroup("vip", true)
	assert.Error(t, err)
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, "", stored.Group)
	assert.False(t, stored.CrossGroupRetry)
}
