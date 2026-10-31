package kkaimigrate

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// CheckImageAsyncRuntimeSchema checks the image admission dependencies without
// auditing unrelated business tables or tenant data on a request path.
func CheckImageAsyncRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	db = db.WithContext(ctx)
	applied, err := loadApplied(db)
	if err != nil {
		return err
	}
	if err := validateAppliedAgainstMigrationSet(applied, dialect, KnownCatalogVersion, migrationSet()); err != nil {
		return err
	}
	if _, ok := applied[ImageAsyncSchemaVersion]; !ok {
		return fmt.Errorf("%w: image async migration receipt is missing", ErrSchemaNotReady)
	}

	requirements := []runtimeSchemaRequirement{
		{Table: "kkai_job_leases", Columns: []string{"lease_name", "holder", "lease_until", "fence", "updated_at"}},
		{Table: "kkai_outbox", Columns: []string{
			"id", "event_key", "topic", "aggregate_id", "payload", "status", "attempts",
			"available_at", "locked_at", "locked_by", "last_error", "created_at", "delivered_at",
		}},
	}
	requirements = append(requirements, imageStudioRuntimeSchemaRequirements...)
	requirements = append(requirements, imageAsyncRuntimeSchemaRequirements...)
	for _, requirement := range requirements {
		if !db.Migrator().HasTable(requirement.Table) {
			return fmt.Errorf("%w: missing runtime table %s", ErrSchemaNotReady, requirement.Table)
		}
		columns, err := db.Migrator().ColumnTypes(requirement.Table)
		if err != nil {
			return fmt.Errorf("inspect runtime table %s: %w", requirement.Table, err)
		}
		actual := make(map[string]bool, len(columns))
		for _, column := range columns {
			actual[column.Name()] = true
		}
		for _, column := range requirement.Columns {
			if !actual[column] {
				return fmt.Errorf("%w: missing runtime column %s.%s", ErrSchemaNotReady, requirement.Table, column)
			}
		}
		if requirement.Table == "kkai_outbox" {
			switch dialect {
			case DialectPostgres:
				if err := validatePostgresOutboxEventKey(db, columns, ImageAsyncSchemaVersion); err != nil {
					return err
				}
			case DialectMySQL:
				if err := validateOutboxEventKeyShape(columns, ImageAsyncSchemaVersion); err != nil {
					return err
				}
			}
			if dialect == DialectMySQL {
				if err := validateRuntimeUniqueIndex(db, dialect, runtimeIndexRequirement{
					Table: "kkai_outbox", Name: "event_key", Columns: []string{"event_key"},
				}); err != nil {
					return err
				}
			}
			if dialect == DialectSQLite {
				var indexes []struct {
					Name            string
					Unique, Partial int
				}
				if err := db.Raw(`SELECT name,"unique",partial FROM pragma_index_list('kkai_outbox')`).Scan(&indexes).Error; err != nil {
					return err
				}
				uniqueEventKey := false
				for _, index := range indexes {
					if index.Unique == 1 && index.Partial == 0 &&
						validateTenantOnlineIndex(db, "kkai_outbox", index.Name, []string{"event_key"}, true) == nil {
						uniqueEventKey = true
						break
					}
				}
				if !uniqueEventKey {
					return fmt.Errorf("%w: kkai_outbox.event_key must be unique", ErrSchemaNotReady)
				}
			}
		}
	}
	for _, index := range []runtimeIndexRequirement{
		{Table: "kkai_image_model_profiles", Name: "ux_kkai_image_model_profiles_model", Columns: []string{"model"}},
		{Table: "kkai_image_generations", Name: "ux_kkai_image_generations_request", Columns: []string{"request_id"}},
		{Table: "kkai_image_assets", Name: "ux_kkai_image_assets_object_key", Columns: []string{"object_key"}},
		{Table: "kkai_image_assets", Name: "ux_kkai_image_assets_generation_position", Columns: []string{"generation_id", "position"}},
		{Table: "kkai_image_reference_uploads", Name: "ux_image_reference_upload_key", Columns: []string{"owner_user_id", "upload_key"}},
	} {
		if err := validateRuntimeUniqueIndex(db, dialect, index); err != nil {
			return err
		}
	}
	return ctx.Err()
}
