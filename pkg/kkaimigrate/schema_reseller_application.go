package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const ResellerApplicationSchemaVersion int64 = 15

var resellerApplicationSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   {{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix VARCHAR(63) NULL"}},
	DialectMySQL:    {{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix VARCHAR(63) NULL"}},
	DialectPostgres: {{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix VARCHAR(63) NULL"}},
}

// This frozen projection belongs to v15. It must not acquire live model fields.
type resellerApplicationV15Prefix struct {
	DesiredDomainPrefix *string `gorm:"type:varchar(63)"`
}

func (resellerApplicationV15Prefix) TableName() string { return "reseller_applications" }

// These explicit canonical online prefixes exclude the retired v12/v13 branch.
// Unknown future versions must never inherit this compatibility implicitly.
func isTenantOnlineLedger(version int64) bool {
	return version == TenantOnlineSchemaVersion || version == ResellerApplicationSchemaVersion || version == RequestTicketSchemaVersion || version == InvoiceSchemaVersion
}

type ResellerApplicationExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	ColumnPresent  bool   `json:"column_present"`
	Safe           bool   `json:"safe"`
}

// PrecheckResellerApplicationExpand never mutates the schema or migration ledger.
func PrecheckResellerApplicationExpand(ctx context.Context, db *gorm.DB) (*ResellerApplicationExpandPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, ResellerApplicationSchemaVersion)
	if err != nil {
		return nil, err
	}
	if !isTenantOnlineLedger(state.currentVersion) {
		return nil, fmt.Errorf("%w: application prefix expand requires canonical schema 14 or 15", ErrSchemaNotReady)
	}
	present, err := migrationColumnExists(db.WithContext(ctx), "reseller_applications", "desired_domain_prefix")
	if err != nil {
		return nil, err
	}
	if present {
		if err := validateResellerApplicationPrefixSchema(db.WithContext(ctx)); err != nil {
			return nil, err
		}
	}
	return &ResellerApplicationExpandPrecheck{
		Schema: "kkai_reseller_application_expand_v1", CurrentVersion: state.currentVersion,
		TargetVersion: ResellerApplicationSchemaVersion, ColumnPresent: present, Safe: true,
	}, nil
}

// ResellerApplicationPlanChecksum binds the independent controller proof to the
// frozen v15 DDL, without changing the historical tenant-online-v1 plan.
func ResellerApplicationPlanChecksum() string {
	return migrationChecksum(migrationSet()[ResellerApplicationSchemaVersion-1])
}

func validateResellerApplicationPrefixSchema(db *gorm.DB) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&resellerApplicationV15Prefix{}); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(statement.Schema.Table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() == "desired_domain_prefix" {
			return validateTenantOnlineColumn(db, statement.Schema.Table, statement.Schema.LookUpField(column.Name()), column)
		}
	}
	return fmt.Errorf("%w: missing column reseller_applications.desired_domain_prefix", ErrSchemaNotReady)
}

// CheckResellerApplicationSchema is the read-only enablement gate. A nullable
// column alone does not enable the feature before its canonical receipt exists.
func CheckResellerApplicationSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, ResellerApplicationSchemaVersion, ResellerApplicationSchemaVersion, InvoiceSchemaVersion)
}

// ApplyResellerApplicationExpand adds only the v15 nullable column and receipt
// after validating the complete canonical v14 contract under the shared lock.
// Isolated rehearsals may invoke it directly. Production CLI mutation requires
// the installed controller proof that every serving and rollback runtime accepts
// v15. Offline enables only isolated MySQL
// and older PostgreSQL compatibility exercises, never a serving-process fallback.
func ApplyResellerApplicationExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckResellerApplicationExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[ResellerApplicationSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == ResellerApplicationSchemaVersion {
			return locked.Where("version = ?", ResellerApplicationSchemaVersion).Find(&result.Applied).Error
		}
		if options.DryRun {
			result.Pending = []AppliedMigration{entry}
			return nil
		}
		started := time.Now()
		dialect, err := dialectName(locked)
		if err != nil {
			return err
		}
		apply := func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if dialect == DialectPostgres {
				if err := tx.Exec("LOCK TABLE reseller_applications IN ACCESS EXCLUSIVE MODE NOWAIT").Error; err != nil {
					return err
				}
			}
			if err := executeMigrationSchema(tx, dialect, item); err != nil {
				return err
			}
			if err := validateResellerApplicationPrefixSchema(tx); err != nil {
				return err
			}
			return importLegacyAndRecord(tx, dialect, item, entry.Checksum, started)
		}
		if dialect == DialectMySQL {
			// MySQL DDL commits implicitly. A rerun validates the exact existing
			// column before appending the missing receipt; it never alters drift.
			err = apply(locked)
		} else {
			err = locked.Transaction(apply)
		}
		if err != nil {
			return err
		}
		return locked.Where("version = ?", ResellerApplicationSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
