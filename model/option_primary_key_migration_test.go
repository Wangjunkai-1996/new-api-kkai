package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOptionPrimaryKeyRepairPreservesBackupAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, db.Exec("CREATE TABLE options (`key` TEXT, value TEXT)").Error)
	original := []Option{{Key: "SystemName", Value: "KKAI"}, {Key: "FAQ", Value: "[]"}, {Key: "FAQ", Value: "[]"}, {Key: "", Value: "legacy"}}
	require.NoError(t, db.Create(&original).Error)
	require.NoError(t, migrateOptionPrimaryKey(db))
	var actual []Option
	require.NoError(t, db.Order("`key`").Find(&actual).Error)
	assert.Equal(t, []Option{{Key: "FAQ", Value: "[]"}, {Key: "SystemName", Value: "KKAI"}}, actual)
	assert.Error(t, db.Create(&Option{Key: "FAQ", Value: "conflict"}).Error)
	var backupName string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'options_legacy_%'").Scan(&backupName).Error)
	require.NotEmpty(t, backupName)
	var backup []Option
	require.NoError(t, db.Table(backupName).Find(&backup).Error)
	assert.Equal(t, original, backup)
	recorder := &migrationSQLRecorder{}
	require.NoError(t, migrateOptionPrimaryKey(db.Session(&gorm.Session{Logger: recorder})))
	assert.Empty(t, recorder.schemaMutations())
}
