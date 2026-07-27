package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestLockVideoRowsForUpdateEmitsDatabaseRowLock(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	var asset model.KKAIVideoAsset
	statement := lockVideoRowsForUpdate(db).Where("id = ?", 1).First(&asset).Statement.SQL.String()
	require.Contains(t, statement, "FOR UPDATE")
}
