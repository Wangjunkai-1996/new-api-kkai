package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const TenantOnlineSchemaVersion int64 = 14

// This ledger is deliberately separate: an expanded database still has the
// original 1..11 schema history, and remains readable by the old application.
type tenantOnlineReceipt struct {
	Step         string `gorm:"type:varchar(128);primaryKey"`
	PlanChecksum string `gorm:"type:char(64);not null"`
	SourceDigest string `gorm:"type:varchar(71);not null"`
	Checksum     string `gorm:"type:char(64);not null"`
	State        string `gorm:"type:varchar(16);not null"`
	CreatedAt    int64  `gorm:"not null"`
	CompletedAt  int64  `gorm:"not null"`
}

func (tenantOnlineReceipt) TableName() string { return "kkai_tenant_online_steps" }

type TenantOnlineExpansionStatus struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	PlanChecksum   string `json:"plan_checksum"`
	State          string `json:"state"`
	CompletedSteps int    `json:"completed_steps"`
	TotalSteps     int    `json:"total_steps"`
	SourceDigest   string `json:"source_digest"`
	DatabaseName   string `json:"database_name"`
	DatabaseSchema string `json:"database_schema"`
}

func tenantOnlineSchemaSpec() string {
	return "v14: independent resumable additive expansion receipts over immutable 1..11 ledger; PostgreSQL >=12 bounded metadata DDL with NOWAIT table locks and concurrent indexes; retain all global username keys until guarded contract; production mutation requires installed root-controller proof tenant-online-v1, tenant routes disabled, main-database logs, and proof revalidation before each mutation; contract additionally requires tenant-aware active/rollback/writer runtimes; final ledger 1..11,14 never 12/13; no canonicalization or cash backfill; explicit blocking isolated compatibility fallback for SQLite/MySQL/older PostgreSQL; owned contract receipt permits MySQL DDL resume; final shape follows frozen v13 except reseller_wallets.exact_profit_carry and customer_payment_reversals.reversed_profit_minor_exact are NOT NULL TEXT (MySQL LONGTEXT), without defaults, for aggregate rational amounts; single-quote and ledger amount bounds remain unchanged\n" + tenantCompatibilitySchemaSpec()
}

// TenantOnlinePlanChecksum binds controller evidence and resumable receipts to
// the exact compiled expansion/contract plan, independently from its DB ledger.
func TenantOnlinePlanChecksum() string { return tenantOnlineHash(tenantOnlineSchemaSpec()) }

func tenantOnlineHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// This catalog hook must never perform the contract through generic Apply.
func rejectTenantOnlineAutomaticApply(*gorm.DB) error {
	return fmt.Errorf("%w: v14 requires the explicit online expansion and guarded contract workflow", ErrUnsafeMigration)
}

// PlanTenantOnlineExpansion is read-only, including on a pristine v11 source.
func PlanTenantOnlineExpansion(ctx context.Context, db *gorm.DB) (*TenantOnlineExpansionStatus, error) {
	return planTenantOnlineExpansion(ctx, db, false)
}

// PlanTenantOfflineExpansion describes the explicitly blocking compatibility path.
func PlanTenantOfflineExpansion(ctx context.Context, db *gorm.DB) (*TenantOnlineExpansionStatus, error) {
	return planTenantOnlineExpansion(ctx, db, true)
}

func planTenantOnlineExpansion(ctx context.Context, db *gorm.DB, offline bool) (*TenantOnlineExpansionStatus, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := tenantOnlineSupportedDatabase(db.WithContext(ctx), offline); err != nil {
		return nil, err
	}
	if db.Migrator().HasTable(&tenantOnlineReceipt{}) {
		var count int64
		if err := db.Model(&tenantOnlineReceipt{}).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 0 {
			return ObserveTenantOnlineExpansion(ctx, db)
		}
		if err := (tenantOnlineStep{key: "table:kkai_tenant_online_steps", table: "kkai_tenant_online_steps", model: &tenantOnlineReceipt{}}).validate(db); err != nil {
			return nil, err
		}
	}
	report, err := PrecheckTenantCompatibilitySchema(ctx, db)
	if err != nil {
		return nil, err
	}
	if !report.Safe {
		return nil, fmt.Errorf("%w: invalid legacy identities", ErrSchemaNotReady)
	}
	keys, err := tenantGlobalUsernameUniqueKeys(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: missing legacy global username protection", ErrSchemaNotReady)
	}
	if err := validateTenantOnlineLegacyProtection(db); err != nil {
		return nil, err
	}
	status := &TenantOnlineExpansionStatus{Schema: "kkai_tenant_online_v1", CurrentVersion: 11, PlanChecksum: TenantOnlinePlanChecksum(), State: "pristine", TotalSteps: len(tenantOnlineSteps())}
	if err := populateTenantOnlineIdentity(ctx, db, status); err != nil {
		return nil, err
	}
	return status, nil
}

func ObserveTenantOnlineExpansion(ctx context.Context, db *gorm.DB) (*TenantOnlineExpansionStatus, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, KnownCatalogVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != MembershipSchemaVersion && !isTenantOnlineLedger(state.currentVersion) {
		return nil, fmt.Errorf("%w: online tenant source must be schema 11, 14, 15 or 16", ErrSchemaNotReady)
	}
	receipts, err := loadTenantOnlineReceipts(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	status := &TenantOnlineExpansionStatus{Schema: "kkai_tenant_online_v1", CurrentVersion: state.currentVersion, PlanChecksum: TenantOnlinePlanChecksum(), State: "expanding", TotalSteps: len(tenantOnlineSteps())}
	if err := populateTenantOnlineIdentity(ctx, db, status); err != nil {
		return nil, err
	}
	for _, step := range tenantOnlineSteps() {
		receipt, owned := receipts[step.key]
		exists := step.exists(db.WithContext(ctx))
		if !owned && exists {
			return nil, fmt.Errorf("%w: unowned partial tenant object %s", ErrSchemaNotReady, step.key)
		}
		if owned && (receipt.State == "done" || exists) {
			if err := step.validate(db.WithContext(ctx)); err != nil {
				return nil, err
			}
		}
		if owned && receipt.State == "done" {
			status.CompletedSteps++
		}
	}
	if status.CompletedSteps == status.TotalSteps {
		status.State = "expanded"
	}
	if isTenantOnlineLedger(state.currentVersion) {
		status.State = "finalized"
	}
	return status, nil
}

// ApplyTenantOnlineExpansion performs only additive steps. It never advances
// the historical schema ledger, removes legacy keys, or enables tenant traffic.
// Production callers must use the root-controller proof CLI; it never accepts an acknowledgement flag.
func ApplyTenantOnlineExpansion(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	result := &Result{}
	if options.DryRun {
		_, err := planTenantOnlineExpansion(ctx, db, options.Offline)
		return result, err
	}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		var count int64
		if locked.Migrator().HasTable(&tenantOnlineReceipt{}) {
			if err := locked.Model(&tenantOnlineReceipt{}).Count(&count).Error; err != nil {
				return err
			}
		}
		if count == 0 {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, locked); err != nil {
					return err
				}
			}
			if _, err := planTenantOnlineExpansion(ctx, locked, options.Offline); err != nil {
				return err
			}
			if err := locked.Transaction(func(tx *gorm.DB) error {
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, tx); err != nil {
						return err
					}
				}
				if !tx.Migrator().HasTable(&tenantOnlineReceipt{}) {
					if err := tx.Migrator().CreateTable(&tenantOnlineReceipt{}); err != nil {
						return err
					}
				}
				return tx.Create(tenantOnlineRootReceipt(tx)).Error
			}); err != nil {
				return err
			}
		}
		if err := checkThroughVersion(ctx, locked, MembershipSchemaVersion, MembershipSchemaVersion, MembershipSchemaVersion); err != nil {
			return err
		}
		receipts, err := loadTenantOnlineReceipts(locked)
		if err != nil {
			return err
		}
		for _, step := range tenantOnlineSteps() {
			receipt, exists := receipts[step.key]
			if exists && receipt.State == "done" {
				if err := step.validate(locked); err != nil {
					return err
				}
				continue
			}
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, locked); err != nil {
					return err
				}
			}
			if !exists {
				if step.exists(locked) {
					return fmt.Errorf("%w: unowned partial tenant object %s", ErrSchemaNotReady, step.key)
				}
				receipt = *tenantOnlineRootReceipt(locked)
				receipt.Step, receipt.Checksum, receipt.State, receipt.CompletedAt = step.key, step.checksum(), "started", 0
				if err := locked.Create(&receipt).Error; err != nil {
					return err
				}
			}
			if !step.exists(locked) {
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, locked); err != nil {
						return err
					}
				}
				if err := step.apply(locked); err != nil {
					return fmt.Errorf("tenant online step %s stopped: %w", step.key, err)
				}
			}
			if err := step.validate(locked); err != nil {
				return err
			}
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, locked); err != nil {
					return err
				}
			}
			if err := locked.Model(&tenantOnlineReceipt{}).Where("step = ? AND state = ?", step.key, "started").Updates(map[string]any{"state": "done", "completed_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		return CheckTenantOnlineExpansion(ctx, locked)
	})
	return result, err
}

func CheckTenantOnlineExpansion(ctx context.Context, db *gorm.DB) error {
	return checkTenantOnlineExpansion(ctx, db, true)
}

func checkTenantOnlineExpansion(ctx context.Context, db *gorm.DB, requireLegacy bool) error {
	if err := checkThroughVersion(ctx, db, MembershipSchemaVersion, MembershipSchemaVersion, MembershipSchemaVersion); err != nil {
		return err
	}
	if err := validateTenantOnlineCompleted(db.WithContext(ctx)); err != nil {
		return err
	}
	keys, err := tenantGlobalUsernameUniqueKeys(db.WithContext(ctx))
	if err != nil {
		return err
	}
	if requireLegacy && len(keys) == 0 {
		return fmt.Errorf("%w: expanded v11 must retain global username protection", ErrSchemaNotReady)
	}
	if requireLegacy {
		if err := validateTenantOnlineLegacyProtection(db); err != nil {
			return err
		}
	}
	for _, table := range tenantV12SnapshotTables {
		var count int64
		if err := db.WithContext(ctx).Table(table).Where("tenant_id IS NULL OR tenant_id <> 0").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%w: tenant data exists before contract in %s", ErrSchemaNotReady, table)
		}
	}
	var invalid int64
	if err := db.WithContext(ctx).Table("users").Where("normalized_username IS NOT NULL OR normalized_email IS NOT NULL").Count(&invalid).Error; err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("%w: platform canonical identities must stay NULL", ErrSchemaNotReady)
	}
	for _, value := range tenantOnlineNewModels() {
		var count int64
		if err := db.WithContext(ctx).Model(value).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%w: tenant business tables must remain empty before contract", ErrSchemaNotReady)
		}
	}
	return nil
}

func CheckTenantOnlineSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, TenantOnlineSchemaVersion, TenantOnlineSchemaVersion, RequestTicketSchemaVersion)
}

// FinalizeTenantOnlineSchema is also usable for isolated rehearsal. Production
// callers must supply the controller authorization callback, which proves all
// active/rollback/writer binaries and disabled routes before each mutation.
func FinalizeTenantOnlineSchema(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		applied, err := loadApplied(locked)
		if err != nil {
			return err
		}
		receipts, err := loadTenantOnlineReceipts(locked)
		if err != nil {
			return err
		}
		contract, owned := receipts["contract"]
		if _, done := applied[TenantOnlineSchemaVersion]; done {
			if !owned {
				return fmt.Errorf("%w: missing owned contract receipt", ErrSchemaNotReady)
			}
			if err := validateTenantOnlineCompleted(locked); err != nil {
				return err
			}
			if err := validateTenantCompatibilityRuntimeSchema(locked); err != nil {
				return err
			}
			if !options.DryRun && contract.State != "done" {
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, locked); err != nil {
						return err
					}
				}
				if err := locked.Model(&tenantOnlineReceipt{}).Where("step=?", "contract").Updates(map[string]any{"state": "done", "completed_at": time.Now().Unix()}).Error; err != nil {
					return err
				}
			}
			return CheckTenantOnlineSchema(ctx, locked)
		}
		// All table/data scans happen before any ACCESS EXCLUSIVE contract lock.
		if err := checkTenantOnlineExpansion(ctx, locked, !owned); err != nil {
			return err
		}
		item := migrationSet()[TenantOnlineSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if options.DryRun {
			result.Pending = []AppliedMigration{entry}
			return nil
		}
		keys, err := tenantGlobalUsernameUniqueKeys(locked)
		if err != nil {
			return err
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if !owned {
			receipt := tenantOnlineRootReceipt(locked)
			receipt.Step, receipt.Checksum, receipt.State, receipt.CompletedAt = "contract", tenantOnlineHash("contract:"+tenantOnlineSchemaSpec()), "started", 0
			if err := locked.Create(receipt).Error; err != nil {
				return err
			}
		}
		started := time.Now()
		finish := func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if tx.Dialector.Name() == DialectPostgres {
				if err := tx.Exec("LOCK TABLE users IN ACCESS EXCLUSIVE MODE NOWAIT").Error; err != nil {
					return err
				}
			}
			if err := tenantOnlineDropLegacyKeys(tx, keys); err != nil {
				return err
			}
			// Metadata only under the exclusive lock; no account/table scans.
			remaining, err := tenantGlobalUsernameUniqueKeys(tx)
			if err != nil {
				return err
			}
			if len(remaining) != 0 {
				return fmt.Errorf("%w: global username keys remain", ErrSchemaNotReady)
			}
			entry.AppliedAt, entry.ExecutionMS = started.Unix(), time.Since(started).Milliseconds()
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			return tx.Model(&tenantOnlineReceipt{}).Where("step=?", "contract").Updates(map[string]any{"state": "done", "completed_at": time.Now().Unix()}).Error
		}
		if locked.Dialector.Name() == DialectMySQL {
			// MySQL DDL auto-commits. The owned receipt allows exact-shape resume
			// after partial key removal, or after the final ledger insert.
			err = finish(locked)
		} else {
			err = locked.Transaction(finish)
		}
		if err != nil {
			return err
		}
		result.Applied = []AppliedMigration{entry}
		return CheckTenantOnlineSchema(ctx, locked)
	})
	return result, err
}
