package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLoadPasswordEncryptionRequiresProvisionedKeyWithoutWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&LoginEncryptionKey{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.ErrorIs(t, LoadPasswordEncryption(), gorm.ErrRecordNotFound)
	var count int64
	require.NoError(t, db.Model(&LoginEncryptionKey{}).Count(&count).Error)
	assert.Zero(t, count)

	require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error)
	require.NoError(t, InitPasswordEncryption())
	keyID, publicKey := common.PasswordEncryptionPublicKey()
	require.NotEmpty(t, keyID)
	require.NotEmpty(t, publicKey)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, LoadPasswordEncryption())
	loadedID, loadedKey := common.PasswordEncryptionPublicKey()
	assert.Equal(t, keyID, loadedID)
	assert.Equal(t, publicKey, loadedKey)
}
