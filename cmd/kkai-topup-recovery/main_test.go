package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestOpenDatabaseKeepsMachineOutputFreeOfGORMLogs(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "recovery.db")
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	require.Same(t, logger.Discard, db.Config.Logger)
}
