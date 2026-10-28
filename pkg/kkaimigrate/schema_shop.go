package kkaimigrate

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"time"
)

const ShopSchemaVersion int64 = 19

func validateShopSchema(db *gorm.DB, complete bool) error {
	for _, value := range shopV19Models() {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			return err
		}
		if !complete && !db.Migrator().HasTable(statement.Schema.Table) {
			continue
		}
		if err := (tenantOnlineStep{table: statement.Schema.Table, model: value}).validate(db); err != nil {
			return err
		}
	}
	for _, index := range shopIndexes {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, index.Unique); err != nil {
			return err
		}
	}
	return nil
}

func ShopPlanChecksum() string { return migrationChecksum(migrationSet()[ShopSchemaVersion-1]) }
func ShopSchemaObjectsPresent(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, ErrSchemaNotReady
	}
	for _, value := range shopV19Models() {
		if db.Migrator().HasTable(value) {
			return true, nil
		}
	}
	return false, nil
}

type ShopExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	Safe           bool   `json:"safe"`
}

func PrecheckShopExpand(ctx context.Context, db *gorm.DB) (*ShopExpandPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, ShopSchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != WalletPaymentSchemaVersion && state.currentVersion != ShopSchemaVersion {
		return nil, fmt.Errorf("%w: shop expansion requires canonical schema 18 or 19", ErrSchemaNotReady)
	}
	if err := validateShopSchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	return &ShopExpandPrecheck{Schema: "kkai_shop_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: ShopSchemaVersion, Safe: true}, nil
}

// CheckShopSchema requires canonical receipts, not merely tables.
func CheckShopSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, ShopSchemaVersion, ShopSchemaVersion, ShopSchemaVersion)
}

// ApplyShopExpand is deliberately never called from application
// startup. Production authorization and controller adoption are separate work.
func ApplyShopExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, fmt.Errorf("%w: shop expansion requires explicit controller authorization or isolated offline mode", ErrUnsafeMigration)
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckShopExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[ShopSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == ShopSchemaVersion {
			return locked.Where("version = ?", ShopSchemaVersion).Find(&result.Applied).Error
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
			if err := validateShopSchema(tx, true); err != nil {
				return err
			}
			return importLegacyAndRecord(tx, locked.Dialector.Name(), item, entry.Checksum, started)
		}
		if locked.Dialector.Name() == DialectMySQL {
			err = apply(locked)
		} else {
			err = locked.Transaction(apply)
		}
		if err != nil {
			return err
		}
		return locked.Where("version = ?", ShopSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
