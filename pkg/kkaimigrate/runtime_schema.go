package kkaimigrate

import (
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
	for _, requirement := range runtimeSchemaRequirements {
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
			if err := validatePostgresOutboxEventKey(columnTypes, currentVersion); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePostgresOutboxEventKey(columnTypes []gorm.ColumnType, currentVersion int64) error {
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
	unique, hasUnique := eventKey.Unique()
	if !hasUnique || !unique {
		return fmt.Errorf("%w: PostgreSQL kkai_outbox.event_key must have a single-column unique constraint", ErrSchemaNotReady)
	}
	return nil
}
