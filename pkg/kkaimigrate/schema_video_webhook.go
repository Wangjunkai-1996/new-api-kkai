package kkaimigrate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const VideoWebhookExpansion = "video-webhook-v1"

func videoWebhookSchemaSteps() []tenantOnlineStep {
	return []tenantOnlineStep{
		{key: "video_webhook_endpoints", table: "video_webhook_endpoints", model: &model.VideoWebhookEndpoint{}},
		{key: "video_webhook_tasks", table: "video_webhook_tasks", model: &model.VideoWebhookTask{}},
	}
}

func CheckVideoWebhookSchema(db *gorm.DB) error {
	if db == nil {
		return ErrSchemaNotReady
	}
	for _, step := range videoWebhookSchemaSteps() {
		if err := step.validate(db); err != nil {
			return err
		}
	}
	return nil
}

// ApplyVideoWebhookExpansion only creates new tables. A pre-existing table
// must match the expected contract before any DDL is issued.
func ApplyVideoWebhookExpansion(ctx context.Context, db *gorm.DB, options Options) error {
	if ctx == nil || db == nil {
		return ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	if !options.DryRun && !options.Offline && (dialect != DialectPostgres || options.AuthorizeMutation == nil) {
		return ErrUnsafeMigration
	}
	return withMigrationLock(ctx, db, dialect, func(locked *gorm.DB) error {
		var missing []tenantOnlineStep
		for _, step := range videoWebhookSchemaSteps() {
			if !locked.Migrator().HasTable(step.model) {
				missing = append(missing, step)
			} else if err := step.validate(locked); err != nil {
				return err
			}
		}
		if options.DryRun {
			return nil
		}
		if !options.Offline {
			if err := CheckRequired(ctx, locked); err != nil {
				return err
			}
		}
		if len(missing) == 0 {
			return nil
		}
		create := func(tx *gorm.DB) error {
			for _, step := range missing {
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, tx); err != nil {
						return err
					}
				}
				ddl := tx
				if dialect == DialectMySQL {
					ddl = ddl.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
				}
				if err := ddl.Migrator().CreateTable(step.model); err != nil {
					return fmt.Errorf("create %s: %w", step.table, err)
				}
				if err := step.validate(tx); err != nil {
					return err
				}
			}
			return nil
		}
		if dialect == DialectMySQL {
			return create(locked)
		}
		return locked.Transaction(create)
	})
}

// PlanVideoWebhookExpansion reports existing tables only after validating
// their shape. It does not provision or repair tables.
func PlanVideoWebhookExpansion(db *gorm.DB) (string, error) {
	if db == nil {
		return "", ErrSchemaNotReady
	}
	present := 0
	for _, step := range videoWebhookSchemaSteps() {
		if !db.Migrator().HasTable(step.model) {
			continue
		}
		if err := step.validate(db); err != nil {
			return "", err
		}
		present++
	}
	if present == 0 {
		return "pristine", nil
	}
	if present == len(videoWebhookSchemaSteps()) {
		return "ready", nil
	}
	return "partial", nil
}

func VideoWebhookPlanChecksum(db *gorm.DB) (string, error) {
	spec := VideoWebhookExpansion + "\n" + db.Dialector.Name()
	for _, step := range videoWebhookSchemaSteps() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(step.model); err != nil {
			return "", err
		}
		spec += "\n" + step.table
		for _, field := range stmt.Schema.Fields {
			column := db.Migrator().FullDataTypeOf(field)
			spec += fmt.Sprintf("\n%s:%s;primary=%t", field.DBName, column.SQL, field.PrimaryKey)
		}
		var indexes []string
		for _, index := range stmt.Schema.ParseIndexes() {
			definition := index.Name + ":" + index.Class
			for _, field := range index.Fields {
				definition += ":" + field.DBName
			}
			indexes = append(indexes, definition)
		}
		slices.Sort(indexes)
		spec += "\n" + strings.Join(indexes, "\n")
	}
	return tenantOnlineHash(spec), nil
}
