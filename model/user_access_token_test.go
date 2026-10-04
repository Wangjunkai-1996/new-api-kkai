package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLoadLegacyAccessTokenRetireAtDoesNotProvisionOrExtendDeadline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}))
	previousDB, previousRetireAt := DB, legacyAccessTokenRetireAt.Load()
	DB = db
	legacyAccessTokenRetireAt.Store(0)
	t.Cleanup(func() {
		DB = previousDB
		legacyAccessTokenRetireAt.Store(previousRetireAt)
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.ErrorIs(t, LoadLegacyAccessTokenRetireAt(), gorm.ErrRecordNotFound)
	assert.True(t, LegacyAccessTokensRetired(1))
	var count int64
	require.NoError(t, db.Model(&Option{}).Count(&count).Error)
	assert.Zero(t, count)

	require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error)
	require.NoError(t, db.Create(&Option{Key: legacyAccessTokenRetireAtKey, Value: "12345"}).Error)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, LoadLegacyAccessTokenRetireAt())
	assert.EqualValues(t, 12345, LegacyAccessTokenRetireAt())
	assert.False(t, LegacyAccessTokensRetired(12344))
	assert.True(t, LegacyAccessTokensRetired(12345))
	var stored Option
	require.NoError(t, db.First(&stored).Error)
	assert.Equal(t, "12345", stored.Value)
}
