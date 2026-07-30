package kkaimigrate

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type runtimeSchemaRequirement struct {
	Table   string
	Columns []string
}

var runtimeSchemaRequirements = []runtimeSchemaRequirement{
	{
		Table: "kkai_policy_incidents",
		Columns: []string{
			"id", "event_id", "input_sha256", "source", "occurred_at", "request_id",
			"user_id", "token_id", "channel_id", "model_name", "rule_version",
			"evidence_sha256", "token_fingerprint", "upstream_key_fingerprint",
			"decision", "metadata", "action_taken", "action_result", "token_disabled",
			"user_disabled", "user_disable_skipped", "channel_disabled", "created_at", "updated_at",
		},
	},
	{
		Table: "kkai_outbox",
		Columns: []string{
			"id", "event_key", "topic", "aggregate_id", "payload", "status", "attempts",
			"available_at", "locked_at", "locked_by", "last_error", "created_at", "delivered_at",
		},
	},
	{
		Table: "kkai_internal_balance_adjustments",
		Columns: []string{
			"id", "operation_id", "user_id", "delta", "reason", "metadata", "payload_sha256",
			"original_operation_id", "balance_before", "balance_after", "created_at",
		},
	},
	{
		Table:   "kkai_job_leases",
		Columns: []string{"lease_name", "holder", "lease_until", "fence", "updated_at"},
	},
}

func validateRuntimeSchema(db *gorm.DB, dialect string, currentVersion int64) error {
	requirements := runtimeSchemaRequirements
	if currentVersion >= VideoStudioSchemaVersion {
		requirements = append(requirements, videoStudioRuntimeSchemaRequirements...)
	}
	if currentVersion >= VideoSampleCategorySchemaVersion {
		requirements = append(requirements, videoSampleCategoryRuntimeSchemaRequirements...)
	}
	for _, requirement := range requirements {
		if !db.Migrator().HasTable(requirement.Table) {
			return fmt.Errorf("%w: missing runtime table %s", ErrSchemaNotReady, requirement.Table)
		}
		columnTypes, err := db.Migrator().ColumnTypes(requirement.Table)
		if err != nil {
			return fmt.Errorf("inspect runtime table %s: %w", requirement.Table, err)
		}
		actualColumns := make(map[string]struct{}, len(columnTypes))
		for _, columnType := range columnTypes {
			actualColumns[columnType.Name()] = struct{}{}
		}
		for _, column := range requirement.Columns {
			if _, ok := actualColumns[column]; !ok {
				return fmt.Errorf("%w: missing runtime column %s.%s", ErrSchemaNotReady, requirement.Table, column)
			}
		}
		if requirement.Table == "kkai_outbox" && dialect == DialectPostgres {
			if err := validatePostgresOutboxEventKey(db, columnTypes, currentVersion); err != nil {
				return err
			}
		}
		if requirement.Table == "kkai_video_samples" && len(requirement.Columns) == 1 && requirement.Columns[0] == "category" {
			if err := validateVideoSampleCategoryColumn(db, dialect); err != nil {
				return err
			}
		}
	}
	return nil
}

var videoStudioRuntimeSchemaRequirements = []runtimeSchemaRequirement{
	{Table: "kkai_video_model_profiles", Columns: []string{
		"id", "model", "display_name", "description", "provider_label", "specification_version",
		"specification", "default_parameters", "enabled", "sort_order", "created_at", "updated_at",
	}},
	{Table: "kkai_video_samples", Columns: []string{
		"id", "model_profile_id", "title", "prompt", "mode", "model_version", "parameters",
		"reference_asset_ids", "video_asset_id", "aspect_ratio", "status", "sort_order", "created_at", "updated_at",
	}},
	{Table: "kkai_video_generations", Columns: []string{
		"id", "user_id", "task_id", "model_profile_id", "sample_id", "model", "mode", "prompt",
		"parameters", "created_at", "updated_at", "deleted_at",
	}},
	{Table: "kkai_video_assets", Columns: []string{
		"id", "owner_user_id", "scope", "kind", "state", "object_key", "poster_object_key",
		"preview_object_key", "archive_source_url", "original_filename", "mime_type", "size_bytes",
		"width", "height", "duration_seconds", "codec", "sha256", "failure_reason", "upload_mode",
		"multipart_upload_id", "upload_part_size", "upload_expires_at",
		"created_at", "updated_at", "deleted_at",
	}},
	{Table: "kkai_video_task_assets", Columns: []string{
		"id", "task_id", "asset_id", "role", "position", "created_at",
	}},
	{Table: "kkai_idempotency_keys", Columns: []string{
		"id", "user_id", "operation", "key", "request_hash", "resource_type", "resource_id", "created_at", "expires_at",
	}},
}

var videoSampleCategoryRuntimeSchemaRequirements = []runtimeSchemaRequirement{
	{Table: "kkai_video_samples", Columns: []string{"category"}},
}

func validateVideoSampleCategoryColumn(db *gorm.DB, dialect string) error {
	if dialect == DialectSQLite {
		return validateSQLiteVideoSampleCategoryColumn(db)
	}
	columnTypes, err := db.Migrator().ColumnTypes("kkai_video_samples")
	if err != nil {
		return fmt.Errorf("inspect runtime table kkai_video_samples: %w", err)
	}
	return validateVideoSampleCategoryColumnShape(columnTypes, dialect)
}

func validateSQLiteVideoSampleCategoryColumn(db *gorm.DB) error {
	var columns []struct {
		Name         string         `gorm:"column:name"`
		Type         string         `gorm:"column:type"`
		NotNull      int            `gorm:"column:notnull"`
		DefaultValue sql.NullString `gorm:"column:dflt_value"`
	}
	if err := db.Raw("PRAGMA table_info(kkai_video_samples)").Scan(&columns).Error; err != nil {
		return fmt.Errorf("inspect SQLite kkai_video_samples.category: %w", err)
	}
	for _, column := range columns {
		if column.Name != "category" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(column.Type), "VARCHAR(32)") {
			return fmt.Errorf("%w: kkai_video_samples.category must be VARCHAR(32)", ErrSchemaNotReady)
		}
		if column.NotNull != 0 {
			return fmt.Errorf("%w: kkai_video_samples.category must be nullable", ErrSchemaNotReady)
		}
		if column.DefaultValue.Valid {
			return fmt.Errorf("%w: kkai_video_samples.category must not have a default", ErrSchemaNotReady)
		}
		return nil
	}
	return fmt.Errorf("%w: missing runtime column kkai_video_samples.category", ErrSchemaNotReady)
}

func validateVideoSampleCategoryColumnShape(columnTypes []gorm.ColumnType, dialect string) error {
	var category gorm.ColumnType
	for _, columnType := range columnTypes {
		if columnType.Name() == "category" {
			category = columnType
			break
		}
	}
	if category == nil {
		return fmt.Errorf("%w: missing runtime column kkai_video_samples.category", ErrSchemaNotReady)
	}
	typeName := strings.ToLower(strings.TrimSpace(category.DatabaseTypeName()))
	length, hasLength := category.Length()
	if (typeName != "varchar" && typeName != "character varying") || !hasLength || length != 32 {
		return fmt.Errorf("%w: kkai_video_samples.category must be VARCHAR(32)", ErrSchemaNotReady)
	}
	nullable, hasNullable := category.Nullable()
	if !hasNullable || !nullable {
		return fmt.Errorf("%w: kkai_video_samples.category must be nullable", ErrSchemaNotReady)
	}
	if defaultValue, hasDefault := category.DefaultValue(); hasDefault {
		// MySQL exposes an omitted default and DEFAULT NULL identically. The
		// reviewed migration checksum provides the syntax-level guarantee there.
		if dialect != DialectMySQL || !strings.EqualFold(strings.TrimSpace(defaultValue), "NULL") {
			return fmt.Errorf("%w: kkai_video_samples.category must not have a default", ErrSchemaNotReady)
		}
	}
	return nil
}

func validatePostgresOutboxEventKey(db *gorm.DB, columnTypes []gorm.ColumnType, currentVersion int64) error {
	if err := validatePostgresOutboxEventKeyShape(columnTypes, currentVersion); err != nil {
		return err
	}

	var hasSingleColumnUnique bool
	if err := db.Raw(`
SELECT EXISTS (
	SELECT 1
	FROM pg_catalog.pg_constraint AS constraint_record
	JOIN pg_catalog.pg_attribute AS column_record
		ON column_record.attrelid = constraint_record.conrelid
		AND column_record.attnum = constraint_record.conkey[1]
	WHERE constraint_record.conrelid = pg_catalog.to_regclass(?)
		AND constraint_record.contype = 'u'
		AND pg_catalog.array_length(constraint_record.conkey, 1) = 1
		AND column_record.attname = ?
)`, "kkai_outbox", "event_key").Scan(&hasSingleColumnUnique).Error; err != nil {
		return fmt.Errorf("inspect PostgreSQL kkai_outbox.event_key unique constraint: %w", err)
	}
	if !hasSingleColumnUnique {
		return fmt.Errorf("%w: PostgreSQL kkai_outbox.event_key must have a single-column unique constraint", ErrSchemaNotReady)
	}
	return nil
}

func validatePostgresOutboxEventKeyShape(columnTypes []gorm.ColumnType, currentVersion int64) error {
	var eventKey gorm.ColumnType
	for _, columnType := range columnTypes {
		if columnType.Name() == "event_key" {
			eventKey = columnType
			break
		}
	}
	if eventKey == nil {
		return fmt.Errorf("%w: missing runtime column kkai_outbox.event_key", ErrSchemaNotReady)
	}

	expectedLength := int64(192)
	if currentVersion >= OutboxEventKeySchemaVersion {
		expectedLength = 191
	}
	typeName := strings.ToLower(eventKey.DatabaseTypeName())
	length, hasLength := eventKey.Length()
	if (typeName != "varchar" && typeName != "character varying") || !hasLength || length != expectedLength {
		return fmt.Errorf(
			"%w: PostgreSQL kkai_outbox.event_key must be VARCHAR(%d) at schema version %d",
			ErrSchemaNotReady,
			expectedLength,
			currentVersion,
		)
	}
	nullable, hasNullable := eventKey.Nullable()
	if !hasNullable || nullable {
		return fmt.Errorf("%w: PostgreSQL kkai_outbox.event_key must be NOT NULL", ErrSchemaNotReady)
	}
	return nil
}
