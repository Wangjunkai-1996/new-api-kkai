package model

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

var ErrMainSchemaNotReady = errors.New("main application schema is not ready")

func mainDatabaseAutoMigrateModels() []any {
	return []any{
		&Channel{},
		&Token{},
		&User{},
		&PasskeyCredential{},
		&Option{},
		&Redemption{},
		&Ability{},
		&Log{},
		&Midjourney{},
		&TopUp{},
		&QuotaData{},
		&Task{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Checkin{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&CasbinRule{},
		&AuthzRole{},
	}
}

func ValidateMainSchemaPrerequisites(db *gorm.DB) error {
	if db == nil {
		return ErrMainSchemaNotReady
	}
	models := append(mainDatabaseAutoMigrateModels(), &SubscriptionPlan{})
	for _, modelValue := range models {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(modelValue); err != nil {
			return fmt.Errorf("inspect application model %T: %w", modelValue, err)
		}
		table := statement.Schema.Table
		if !db.Migrator().HasTable(modelValue) {
			return fmt.Errorf("%w: missing table %s", ErrMainSchemaNotReady, table)
		}
		columnTypes, err := db.Migrator().ColumnTypes(modelValue)
		if err != nil {
			return fmt.Errorf("inspect application table %s: %w", table, err)
		}
		actualColumns := make(map[string]gorm.ColumnType, len(columnTypes))
		for _, columnType := range columnTypes {
			actualColumns[columnType.Name()] = columnType
		}
		for _, field := range statement.Schema.Fields {
			if field.IgnoreMigration || field.DBName == "" {
				continue
			}
			if _, ok := actualColumns[field.DBName]; !ok {
				return fmt.Errorf("%w: missing column %s.%s", ErrMainSchemaNotReady, table, field.DBName)
			}
		}
		if db.Dialector.Name() == "postgres" {
			if err := validatePostgresApplicationColumnTypes(table, actualColumns); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePostgresApplicationColumnTypes(table string, columns map[string]gorm.ColumnType) error {
	switch table {
	case "tokens":
		modelLimits, ok := columns["model_limits"]
		if !ok {
			return fmt.Errorf("%w: missing column tokens.model_limits", ErrMainSchemaNotReady)
		}
		actualType := strings.ToLower(modelLimits.DatabaseTypeName())
		if actualType != "text" {
			return fmt.Errorf("%w: PostgreSQL tokens.model_limits must be TEXT, found %s", ErrMainSchemaNotReady, actualType)
		}
	case "subscription_plans":
		priceAmount, ok := columns["price_amount"]
		if !ok {
			return fmt.Errorf("%w: missing column subscription_plans.price_amount", ErrMainSchemaNotReady)
		}
		actualType := strings.ToLower(priceAmount.DatabaseTypeName())
		if actualType != "numeric" {
			return fmt.Errorf(
				"%w: PostgreSQL subscription_plans.price_amount must be NUMERIC(10,6), found %s",
				ErrMainSchemaNotReady,
				actualType,
			)
		}
		precision, scale, ok := priceAmount.DecimalSize()
		if !ok {
			return fmt.Errorf(
				"%w: cannot determine PostgreSQL subscription_plans.price_amount precision and scale",
				ErrMainSchemaNotReady,
			)
		}
		if precision != 10 || scale != 6 {
			return fmt.Errorf(
				"%w: PostgreSQL subscription_plans.price_amount must be NUMERIC(10,6), found NUMERIC(%d,%d)",
				ErrMainSchemaNotReady,
				precision,
				scale,
			)
		}
	}
	return nil
}
