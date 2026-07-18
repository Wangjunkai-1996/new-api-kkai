package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordUpstreamSchemaBaselineIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-schema-baseline-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	require.NoError(t, recordUpstreamSchemaBaseline())
	require.NoError(t, recordUpstreamSchemaBaseline())

	var markers []upstreamSchemaBaseline
	require.NoError(t, db.Find(&markers).Error)
	require.Len(t, markers, 1)
	require.EqualValues(t, upstreamSchemaBaselineVersion, markers[0].Version)
}

func TestBootstrapEmptyUpstreamSchemaCreatesBaseline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-schema-bootstrap-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, BootstrapEmptyUpstreamSchema(context.Background(), db))
	require.True(t, db.Migrator().HasTable(&User{}))
	require.True(t, db.Migrator().HasTable(&upstreamSchemaBaseline{}))

	var marker upstreamSchemaBaseline
	require.NoError(t, db.First(&marker, upstreamSchemaBaselineVersion).Error)
	require.EqualValues(t, upstreamSchemaBaselineVersion, marker.Version)
}

func TestBootstrapEmptyUpstreamSchemaRejectsExistingDatabaseWithoutDDL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-schema-existing-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE existing_production_state (id INTEGER PRIMARY KEY)").Error)

	err = BootstrapEmptyUpstreamSchema(context.Background(), db)
	require.True(t, errors.Is(err, ErrUpstreamSchemaBootstrapRequiresEmptyDatabase))
	require.True(t, db.Migrator().HasTable("existing_production_state"))
	require.False(t, db.Migrator().HasTable(&User{}))
	require.False(t, db.Migrator().HasTable(&upstreamSchemaBaseline{}))
}

func TestProductionImageInitDBDoesNotRunDDLOnExistingDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "production-existing.db")
	seedDB, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, seedDB.Exec("CREATE TABLE existing_production_state (id INTEGER PRIMARY KEY)").Error)
	seedSQLDB, err := seedDB.DB()
	require.NoError(t, err)
	require.NoError(t, seedSQLDB.Close())

	restoreModelDatabaseTestState(t)
	common.SQLitePath = databasePath
	t.Setenv("SQL_DSN", "local")
	common.ProductionImageRuntime = "true"
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleLeader))
	t.Setenv("KKAI_UPSTREAM_SCHEMA_MIGRATION_MODE", "one-shot")
	t.Setenv("DISABLE_BACKGROUND_TASKS", "true")
	require.NoError(t, common.InitNodeRoleFromEnvironment())

	require.NoError(t, InitDB())
	require.True(t, DB.Migrator().HasTable("existing_production_state"))
	require.False(t, DB.Migrator().HasTable(&User{}))
	require.False(t, DB.Migrator().HasTable(&upstreamSchemaBaseline{}))
}

func TestDevelopmentInitDBStillCreatesUpstreamSchema(t *testing.T) {
	restoreModelDatabaseTestState(t)
	common.SQLitePath = filepath.Join(t.TempDir(), "development-empty.db")
	t.Setenv("SQL_DSN", "local")
	common.ProductionImageRuntime = "false"
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleLeader))
	t.Setenv("KKAI_UPSTREAM_SCHEMA_MIGRATION_MODE", "")
	t.Setenv("DISABLE_BACKGROUND_TASKS", "")
	require.NoError(t, common.InitNodeRoleFromEnvironment())

	require.NoError(t, InitDB())
	require.True(t, DB.Migrator().HasTable(&User{}))
	require.True(t, DB.Migrator().HasTable(&upstreamSchemaBaseline{}))
}

func restoreModelDatabaseTestState(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousLogDB := LOG_DB
	previousSQLitePath := common.SQLitePath
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousRole := common.CurrentNodeRole()
	previousWriteJobsDisabled := common.WriteBackgroundTasksDisabled()
	previousProductionImageRuntime := common.ProductionImageRuntime
	t.Cleanup(func() {
		if DB != nil && DB != previousDB {
			if sqlDB, err := DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		DB = previousDB
		LOG_DB = previousLogDB
		common.SQLitePath = previousSQLitePath
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.ProductionImageRuntime = previousProductionImageRuntime
		_ = os.Setenv(common.NodeRoleEnvironmentVariable, string(previousRole))
		_ = os.Setenv("DISABLE_BACKGROUND_TASKS", fmt.Sprintf("%t", previousWriteJobsDisabled))
		_ = common.InitNodeRoleFromEnvironment()
	})
}
