package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const RequestTicketSchemaVersion int64 = 16

// RequestTicketPlanChecksum binds controller evidence to the frozen v16 DDL.
func RequestTicketPlanChecksum() string {
	return migrationChecksum(migrationSet()[RequestTicketSchemaVersion-1])
}

var requestTicketSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   requestTicketStatements("INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", ""),
	DialectMySQL:    requestTicketStatements("BIGINT AUTO_INCREMENT PRIMARY KEY", "BIGINT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"),
	DialectPostgres: requestTicketStatements("BIGSERIAL PRIMARY KEY", "BIGINT", ""),
}

// The v16 DDL and projections are frozen independently of live model changes.
func requestTicketStatements(identity, integer, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS request_tickets (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
title VARCHAR(80) NOT NULL,
description TEXT NOT NULL,
status VARCHAR(16) NOT NULL,
version ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_request_tickets_owner ON request_tickets (tenant_id, user_id, updated_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_request_tickets_status ON request_tickets (tenant_id, status, updated_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS request_ticket_events (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
ticket_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
actor_id ` + integer + ` NOT NULL,
status VARCHAR(16) NOT NULL,
feedback TEXT NOT NULL,
created_at ` + integer + ` NOT NULL,
read_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_request_ticket_events_timeline ON request_ticket_events (tenant_id, ticket_id, id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_request_ticket_events_unread ON request_ticket_events (tenant_id, user_id, read_at, id)`},
	}
}

type requestTicketV16 struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	TenantID    int64  `gorm:"not null"`
	UserID      int    `gorm:"not null"`
	Title       string `gorm:"type:varchar(80);not null"`
	Description string `gorm:"type:text;not null"`
	Status      string `gorm:"type:varchar(16);not null"`
	Version     int64  `gorm:"not null"`
	CreatedAt   int64  `gorm:"not null;autoCreateTime:false"`
	UpdatedAt   int64  `gorm:"not null;autoUpdateTime:false"`
}

func (requestTicketV16) TableName() string { return "request_tickets" }

type requestTicketEventV16 struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	TenantID  int64  `gorm:"not null"`
	TicketID  int64  `gorm:"not null"`
	UserID    int    `gorm:"not null"`
	ActorID   int    `gorm:"not null"`
	Status    string `gorm:"type:varchar(16);not null"`
	Feedback  string `gorm:"type:text;not null"`
	CreatedAt int64  `gorm:"not null;autoCreateTime:false"`
	ReadAt    int64  `gorm:"not null"`
}

func (requestTicketEventV16) TableName() string { return "request_ticket_events" }

var requestTicketIndexes = []runtimeIndexRequirement{
	{Table: "request_tickets", Name: "idx_request_tickets_owner", Columns: []string{"tenant_id", "user_id", "updated_at"}},
	{Table: "request_tickets", Name: "idx_request_tickets_status", Columns: []string{"tenant_id", "status", "updated_at"}},
	{Table: "request_ticket_events", Name: "idx_request_ticket_events_timeline", Columns: []string{"tenant_id", "ticket_id", "id"}},
	{Table: "request_ticket_events", Name: "idx_request_ticket_events_unread", Columns: []string{"tenant_id", "user_id", "read_at", "id"}},
}

// Partial validation permits resuming a MySQL DDL interruption, but never repairs
// mismatched existing columns or indexes. Complete validation requires all objects.
func validateRequestTicketSchema(db *gorm.DB, complete bool) error {
	for _, step := range []tenantOnlineStep{
		{table: "request_tickets", model: &requestTicketV16{}},
		{table: "request_ticket_events", model: &requestTicketEventV16{}},
	} {
		if !complete && !db.Migrator().HasTable(step.table) {
			continue
		}
		if err := step.validate(db); err != nil {
			return err
		}
	}
	for _, index := range requestTicketIndexes {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, false); err != nil {
			return err
		}
	}
	return nil
}

type RequestTicketExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	TicketsPresent bool   `json:"tickets_present"`
	EventsPresent  bool   `json:"events_present"`
	Safe           bool   `json:"safe"`
}

// PrecheckRequestTicketExpand only reads the canonical ledger and physical schema.
func PrecheckRequestTicketExpand(ctx context.Context, db *gorm.DB) (*RequestTicketExpandPrecheck, error) {
	state, err := loadValidatedState(ctx, db, RequestTicketSchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != ResellerApplicationSchemaVersion && state.currentVersion != RequestTicketSchemaVersion {
		return nil, fmt.Errorf("%w: request ticket expansion requires canonical schema 15 or 16", ErrSchemaNotReady)
	}
	if err := validateRequestTicketSchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	return &RequestTicketExpandPrecheck{
		Schema: "kkai_request_ticket_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: RequestTicketSchemaVersion,
		TicketsPresent: db.Migrator().HasTable("request_tickets"), EventsPresent: db.Migrator().HasTable("request_ticket_events"), Safe: true,
	}, nil
}

// CheckRequestTicketSchema requires the immutable v16 receipt and exact physical
// shape. Tables created manually do not enable request or inbox endpoints.
func CheckRequestTicketSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, RequestTicketSchemaVersion, RequestTicketSchemaVersion, ShopSchemaVersion)
}

// ApplyRequestTicketExpand is the explicit isolated-library/controller entry.
// Production mutation requires a separately installed, reviewed controller that
// supplies AuthorizeMutation for the exact database, plan and serving runtimes.
// The ordinary CLI deliberately exposes only precheck and dry-run for target 16.
func ApplyRequestTicketExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckRequestTicketExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[RequestTicketSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == RequestTicketSchemaVersion {
			return locked.Where("version = ?", RequestTicketSchemaVersion).Find(&result.Applied).Error
		}
		if options.DryRun {
			result.Pending = []AppliedMigration{entry}
			return nil
		}
		started := time.Now()
		apply := func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if err := executeMigrationSchema(tx, locked.Dialector.Name(), item); err != nil {
				return err
			}
			if err := validateRequestTicketSchema(tx, true); err != nil {
				return err
			}
			return importLegacyAndRecord(tx, locked.Dialector.Name(), item, entry.Checksum, started)
		}
		if locked.Dialector.Name() == DialectMySQL {
			err = apply(locked) // MySQL DDL commits implicitly; exact partial objects are resumable.
		} else {
			err = locked.Transaction(apply)
		}
		if err != nil {
			return err
		}
		return locked.Where("version = ?", RequestTicketSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
