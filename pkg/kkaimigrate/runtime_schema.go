package kkaimigrate

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

type runtimeSchemaRequirement struct {
	Table   string
	Columns []string
}

type runtimeIndexRequirement struct {
	Table   string
	Name    string
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
	if currentVersion >= ImageStudioSchemaVersion {
		requirements = append(requirements, imageStudioRuntimeSchemaRequirements...)
	}
	if currentVersion >= AuthenticationSchemaVersion {
		requirements = append(requirements, authenticationRuntimeSchemaRequirements...)
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
	if currentVersion >= AuthenticationSchemaVersion {
		return validateAuthenticationRuntimeSchema(db, dialect)
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

var imageStudioRuntimeSchemaRequirements = []runtimeSchemaRequirement{
	{Table: "kkai_image_model_profiles", Columns: []string{
		"id", "model", "display_name", "description", "provider_label", "specification_version",
		"specification", "default_parameters", "enabled", "sort_order", "created_at", "updated_at",
	}},
	{Table: "kkai_image_samples", Columns: []string{
		"id", "model_profile_id", "image_asset_id", "title", "prompt", "model_version",
		"parameters", "category", "status", "sort_order", "created_at", "updated_at",
	}},
	{Table: "kkai_image_generations", Columns: []string{
		"id", "user_id", "token_id", "model_profile_id", "sample_id", "specification_version",
		"model", "prompt", "parameters", "request_hash", "request_id", "status", "requested_count",
		"succeeded_count", "billing_source", "billing_state", "reserved_quota", "final_quota",
		"subscription_id", "heartbeat_at", "failure_stage", "error_code", "error_message",
		"started_at", "finished_at", "created_at", "updated_at", "deleted_at",
	}},
	{Table: "kkai_image_assets", Columns: []string{
		"id", "generation_id", "owner_user_id", "scope", "kind", "state", "position", "object_key",
		"thumbnail_object_key", "thumbnail_state", "original_filename", "mime_type", "size_bytes",
		"width", "height", "sha256", "failure_reason", "created_at", "updated_at", "deleted_at",
	}},
}

var authenticationRuntimeSchemaRequirements = []runtimeSchemaRequirement{
	{Table: "users", Columns: []string{"auth_version"}},
	{Table: "tokens", Columns: []string{"auto_groups"}},
	{Table: "user_sessions", Columns: []string{
		"sid", "user_id", "version", "user_auth_version", "status", "refresh_hash",
		"previous_refresh_hash", "previous_valid_until", "login_method", "ip", "user_agent",
		"created_at", "last_active_at", "expires_at", "revoked_at", "revoked_reason",
	}},
	{Table: "auth_flows", Columns: []string{
		"id", "token_hash", "purpose", "provider", "intent", "user_id", "session_id",
		"payload", "created_at", "expires_at", "consumed_at",
	}},
	{Table: "external_identity_claims", Columns: []string{
		"id", "provider", "subject", "user_id", "created_at",
	}},
}

var authenticationRuntimeUniqueIndexes = []runtimeIndexRequirement{
	{Table: "auth_flows", Name: "idx_auth_flows_token_hash", Columns: []string{"token_hash"}},
	{Table: "external_identity_claims", Name: "idx_external_identity_subject", Columns: []string{"provider", "subject"}},
	{Table: "external_identity_claims", Name: "idx_external_identity_user", Columns: []string{"provider", "user_id"}},
}

func validateAuthenticationRuntimeSchema(db *gorm.DB, dialect string) error {
	for _, requirement := range authenticationRuntimeUniqueIndexes {
		if err := validateRuntimeUniqueIndex(db, dialect, requirement); err != nil {
			return err
		}
	}

	var invalidAuthVersions int64
	if err := db.Table("users").
		Where("auth_version IS NULL OR auth_version < ?", 1).
		Count(&invalidAuthVersions).Error; err != nil {
		return fmt.Errorf("validate users.auth_version backfill: %w", err)
	}
	if invalidAuthVersions != 0 {
		return fmt.Errorf("%w: users.auth_version backfill is incomplete", ErrSchemaNotReady)
	}

	var users []authenticationLegacyUser
	if err := db.Table("users").
		Select("id", "telegram_id").
		Where("telegram_id IS NOT NULL").
		Order("id ASC").
		Find(&users).Error; err != nil {
		return fmt.Errorf("validate legacy Telegram identity backfill: %w", err)
	}
	for _, user := range users {
		subject := strings.TrimSpace(user.TelegramID.String)
		if !user.TelegramID.Valid || subject == "" {
			continue
		}

		var subjectOwner struct {
			UserID int64 `gorm:"column:user_id"`
		}
		result := db.Table("external_identity_claims").
			Select("user_id").
			Where("provider = ? AND subject = ?", authenticationIdentityProviderTelegram, subject).
			Limit(1).
			Scan(&subjectOwner)
		if result.Error != nil {
			return fmt.Errorf("validate Telegram identity for user %d: %w", user.ID, result.Error)
		}
		if result.RowsAffected != 1 || subjectOwner.UserID != user.ID {
			return fmt.Errorf("%w: Telegram identity backfill is inconsistent for user %d", ErrSchemaNotReady, user.ID)
		}

		var userClaim struct {
			Subject string `gorm:"column:subject"`
		}
		result = db.Table("external_identity_claims").
			Select("subject").
			Where("provider = ? AND user_id = ?", authenticationIdentityProviderTelegram, user.ID).
			Limit(1).
			Scan(&userClaim)
		if result.Error != nil {
			return fmt.Errorf("validate Telegram identity for user %d: %w", user.ID, result.Error)
		}
		if result.RowsAffected != 1 || userClaim.Subject != subject {
			return fmt.Errorf("%w: Telegram identity backfill is inconsistent for user %d", ErrSchemaNotReady, user.ID)
		}
	}
	return nil
}

func validateRuntimeUniqueIndex(db *gorm.DB, dialect string, requirement runtimeIndexRequirement) error {
	if dialect == DialectSQLite {
		return validateSQLiteRuntimeUniqueIndex(db, requirement)
	}
	indexes, err := db.Migrator().GetIndexes(requirement.Table)
	if err != nil {
		return fmt.Errorf("inspect runtime indexes on %s: %w", requirement.Table, err)
	}
	for _, index := range indexes {
		if !strings.EqualFold(index.Name(), requirement.Name) {
			continue
		}
		unique, known := index.Unique()
		if !known || !unique || !equalRuntimeIndexColumns(index.Columns(), requirement.Columns) {
			return fmt.Errorf("%w: runtime index %s has an invalid unique shape", ErrSchemaNotReady, requirement.Name)
		}
		return nil
	}
	return fmt.Errorf("%w: missing runtime unique index %s", ErrSchemaNotReady, requirement.Name)
}

func validateSQLiteRuntimeUniqueIndex(db *gorm.DB, requirement runtimeIndexRequirement) error {
	if !isCanonicalSQLIdentifier(strings.ToUpper(requirement.Table)) ||
		!isCanonicalSQLIdentifier(strings.ToUpper(requirement.Name)) {
		return fmt.Errorf("%w: invalid runtime index requirement", ErrSchemaNotReady)
	}
	var indexes []struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	if err := db.Raw("PRAGMA index_list('" + requirement.Table + "')").Scan(&indexes).Error; err != nil {
		return fmt.Errorf("inspect SQLite runtime indexes on %s: %w", requirement.Table, err)
	}
	for _, index := range indexes {
		if !strings.EqualFold(index.Name, requirement.Name) {
			continue
		}
		if index.Unique != 1 {
			return fmt.Errorf("%w: runtime index %s must be unique", ErrSchemaNotReady, requirement.Name)
		}
		var columns []struct {
			Sequence int    `gorm:"column:seqno"`
			Name     string `gorm:"column:name"`
		}
		if err := db.Raw("PRAGMA index_info('" + requirement.Name + "')").Scan(&columns).Error; err != nil {
			return fmt.Errorf("inspect SQLite runtime index %s: %w", requirement.Name, err)
		}
		sort.Slice(columns, func(i, j int) bool { return columns[i].Sequence < columns[j].Sequence })
		actual := make([]string, 0, len(columns))
		for _, column := range columns {
			actual = append(actual, column.Name)
		}
		if !equalRuntimeIndexColumns(actual, requirement.Columns) {
			return fmt.Errorf("%w: runtime index %s has invalid columns", ErrSchemaNotReady, requirement.Name)
		}
		return nil
	}
	return fmt.Errorf("%w: missing runtime unique index %s", ErrSchemaNotReady, requirement.Name)
}

func equalRuntimeIndexColumns(actual []string, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if !strings.EqualFold(actual[index], expected[index]) {
			return false
		}
	}
	return true
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
