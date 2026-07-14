package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSyncChannelCacheOncePreservesLastGoodCacheOnDatabaseFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:channel-cache-preserve-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	previousDB := DB
	previousMemoryCache := common.MemoryCacheEnabled
	DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	channel := Channel{
		Type:   1,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "last-good",
		Models: "gpt-cache-preserve",
		Group:  "default",
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: channel.Models, ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, SyncChannelCacheOnce())

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	require.Error(t, SyncChannelCacheOnce())

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, "last-good", cached.Name)
}
