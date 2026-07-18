package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const upstreamSchemaBaselineVersion int64 = 1

var ErrUpstreamSchemaBootstrapRequiresEmptyDatabase = errors.New("upstream schema baseline bootstrap requires an empty database")

// upstreamSchemaBaseline records the ownership boundary for the pre-existing
// upstream GORM schema. New production DDL must be represented by the explicit
// KKAI migration contract rather than silently extending this baseline.
type upstreamSchemaBaseline struct {
	Version   int64 `gorm:"primaryKey"`
	AppliedAt int64 `gorm:"not null"`
}

func (upstreamSchemaBaseline) TableName() string {
	return "kkai_upstream_schema_baselines"
}

func recordUpstreamSchemaBaseline() error {
	if err := DB.AutoMigrate(&upstreamSchemaBaseline{}); err != nil {
		return err
	}
	marker := upstreamSchemaBaseline{
		Version:   upstreamSchemaBaselineVersion,
		AppliedAt: time.Now().Unix(),
	}
	return DB.Where("version = ?", upstreamSchemaBaselineVersion).FirstOrCreate(&marker).Error
}

// BootstrapEmptyUpstreamSchema creates the frozen upstream baseline only for a
// database with no user schema objects. It is intended for disposable image
// smoke databases and new installations, never application startup or upgrades.
func BootstrapEmptyUpstreamSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrUpstreamSchemaBootstrapRequiresEmptyDatabase
	}
	boundDB := db.WithContext(ctx)
	objectCount, databaseType, err := upstreamSchemaObjectCount(boundDB)
	if err != nil {
		return err
	}
	if objectCount != 0 {
		return fmt.Errorf("%w: found %d user schema objects", ErrUpstreamSchemaBootstrapRequiresEmptyDatabase, objectCount)
	}

	previousDB := DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	defer func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		initCol()
	}()

	DB = boundDB
	common.SetDatabaseTypes(databaseType, databaseType)
	initCol()
	if err := migrateDB(); err != nil {
		return fmt.Errorf("bootstrap upstream schema baseline: %w", err)
	}
	return nil
}

func upstreamSchemaObjectCount(db *gorm.DB) (int64, common.DatabaseType, error) {
	var count int64
	switch db.Dialector.Name() {
	case "postgres":
		err := db.Raw(`
SELECT COUNT(*)
FROM pg_catalog.pg_class AS class
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = current_schema()
  AND class.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
`).Scan(&count).Error
		return count, common.DatabaseTypePostgreSQL, err
	case "mysql":
		err := db.Raw(`
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE()
`).Scan(&count).Error
		return count, common.DatabaseTypeMySQL, err
	case "sqlite":
		err := db.Raw(`
SELECT COUNT(*)
FROM sqlite_master
WHERE name NOT LIKE 'sqlite_%'
`).Scan(&count).Error
		return count, common.DatabaseTypeSQLite, err
	default:
		return 0, "", fmt.Errorf("unsupported upstream schema bootstrap dialect: %s", db.Dialector.Name())
	}
}
