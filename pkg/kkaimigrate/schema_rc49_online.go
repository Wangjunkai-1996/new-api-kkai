package kkaimigrate

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"
)

const RC49OnlineExpansion = "rc49-online-v1"

const rc49ConcurrentAuditIndexSQL = `CREATE INDEX CONCURRENTLY idx_audit_time_event ON audit_logs (created_at, event_id)`

// This independent receipt is not canonical migration22. Its source remains
// canonical21 throughout expansion and rollback to the accepted old runtime.
type rc49OnlineReceipt struct {
	Step         string `gorm:"type:varchar(32);primaryKey;not null"`
	PlanChecksum string `gorm:"type:char(64);not null"`
	SourceDigest string `gorm:"type:varchar(71);not null"`
	State        string `gorm:"type:varchar(16);not null"`
	CreatedAt    int64  `gorm:"not null"`
	CompletedAt  int64  `gorm:"not null"`
}

func (rc49OnlineReceipt) TableName() string { return "kkai_rc49_online_steps" }

func RC49OnlinePlanChecksum() string {
	return tenantOnlineHash("rc49-online-v1; PostgreSQL>=12; canonical21 unchanged; columns and columns/done receipt atomic under NOWAIT locks; audit_index/building ownership precedes concurrent index; audit_index/done receipt requires exact valid ready nonunique btree(created_at,event_id); repair-index only owned invalid index; source digest and compiled plan bound to both receipts; root proof before every mutation; receipt fields step,plan_checksum,source_digest,state,created_at,completed_at\n" +
		rc49SchemaStatements[DialectPostgres][0].SQL + "\n" + rc49SchemaStatements[DialectPostgres][1].SQL + "\n" + rc49SchemaStatements[DialectPostgres][2].SQL + "\n" + rc49ConcurrentAuditIndexSQL)
}

func RC49OnlineExpansionDigest() string { return "sha256:" + RC49OnlinePlanChecksum() }

func RC49OnlineSourceDigest() string {
	return migrationSetDigest(DialectPostgres, planItemsForDialect(DialectPostgres, MembershipBillingRecoverySchemaVersion))
}

// ValidateRC49OnlineRuntimeContract accepts the exact already-deployed max21
// compatibility contract or this expansion-aware rc49 contract. It never
// accepts a version range without checking every canonical prefix.
func ValidateRC49OnlineRuntimeContract(contract SchemaContract) error {
	expected, err := ContractForDialect(DialectPostgres)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(contract, expected) {
		return nil
	}
	delete(expected.RequiredExpansions, VideoMediaMetadataExpansion)
	if reflect.DeepEqual(contract, expected) {
		return nil
	}
	prefixes, err := compatiblePrefixDigests(DialectPostgres, 14, 21)
	if err != nil {
		return err
	}
	legacy := SchemaContract{RuntimeMinVersion: 14, RuntimeMaxVersion: 21, MigrationTargetVersion: 14,
		MigrationKind: MigrationKindNone, MigrationSetDigest: prefixes["14"], CompatiblePrefixes: prefixes}
	if !reflect.DeepEqual(contract, legacy) {
		return fmt.Errorf("%w: runtime does not have an exact canonical21-compatible contract", ErrUnsafeMigration)
	}
	return nil
}

type RC49OnlineStatus struct {
	Schema          string `json:"schema"`
	CurrentVersion  int64  `json:"current_version"`
	TargetVersion   int64  `json:"target_version"`
	SourceDigest    string `json:"source_digest"`
	TargetDigest    string `json:"target_digest"`
	PlanChecksum    string `json:"plan_checksum"`
	ExpansionDigest string `json:"expansion_digest"`
	DatabaseName    string `json:"database_name"`
	DatabaseSchema  string `json:"database_schema"`
	State           string `json:"state"`
	Safe            bool   `json:"safe"`
}

// PlanRC49OnlineExpansion does not create receipts or canonical ledger entries.
// Unowned objects and malformed receipts fail closed, including partial shapes.
func PlanRC49OnlineExpansion(ctx context.Context, db *gorm.DB) (*RC49OnlineStatus, error) {
	if ctx == nil || db == nil || db.Dialector.Name() != DialectPostgres {
		return nil, ErrUnsupportedDialect
	}
	db = db.WithContext(ctx)
	if err := tenantOnlineSupportedDatabase(db, false); err != nil {
		return nil, err
	}
	state, err := loadValidatedState(ctx, db, MembershipBillingRecoverySchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != MembershipBillingRecoverySchemaVersion {
		return nil, fmt.Errorf("%w: rc49 online expansion requires canonical21", ErrSchemaNotReady)
	}
	name, schema, err := TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	status := &RC49OnlineStatus{Schema: "kkai_rc49_expansion_status_v1", CurrentVersion: 21, TargetVersion: 21,
		SourceDigest: RC49OnlineSourceDigest(), TargetDigest: RC49OnlineSourceDigest(), PlanChecksum: RC49OnlinePlanChecksum(),
		ExpansionDigest: RC49OnlineExpansionDigest(), DatabaseName: name, DatabaseSchema: schema, State: "pristine", Safe: true}
	receipts, err := loadRC49OnlineReceipts(db)
	if err != nil {
		return nil, err
	}
	indexState, err := rc49AuditIndexState(db)
	if err != nil {
		return nil, err
	}
	if len(receipts) == 0 {
		for _, column := range []struct{ table, name string }{{"tokens", "allow_user_quota_query"}, {"quota_data", "cache_read_tokens"}, {"quota_data", "cache_creation_tokens"}} {
			if db.Migrator().HasColumn(column.table, column.name) {
				return nil, fmt.Errorf("%w: unreceipted rc49 column %s.%s", ErrSchemaNotReady, column.table, column.name)
			}
		}
		if indexState != "missing" {
			return nil, fmt.Errorf("%w: unowned rc49 audit index", ErrSchemaNotReady)
		}
		if !db.Migrator().HasTable("tokens") || !db.Migrator().HasTable("quota_data") || !db.Migrator().HasTable("audit_logs") {
			return nil, fmt.Errorf("%w: missing rc49 source tables", ErrSchemaNotReady)
		}
		return status, nil
	}
	if _, ok := receipts["columns"]; !ok {
		return nil, fmt.Errorf("%w: rc49 index receipt without columns", ErrSchemaNotReady)
	}
	if err := validateRC49Schema(db, true); err != nil {
		return nil, err
	}
	status.State = "columns_ready"
	indexReceipt, owned := receipts["audit_index"]
	if !owned {
		if indexState != "missing" {
			return nil, fmt.Errorf("%w: unowned rc49 audit index", ErrSchemaNotReady)
		}
		return status, nil
	}
	if indexReceipt.State == "done" {
		if indexState != "valid" {
			return nil, fmt.Errorf("%w: completed rc49 index is missing or invalid", ErrSchemaNotReady)
		}
		status.State = "ready"
		return status, nil
	}
	status.State = "index_pending"
	if indexState == "invalid" {
		status.State, status.Safe = "index_invalid", false
	}
	return status, nil
}

func loadRC49OnlineReceipts(db *gorm.DB) (map[string]rc49OnlineReceipt, error) {
	result := map[string]rc49OnlineReceipt{}
	if !db.Migrator().HasTable(&rc49OnlineReceipt{}) {
		return result, nil
	}
	if err := (tenantOnlineStep{key: "rc49_online_receipts", table: "kkai_rc49_online_steps", model: &rc49OnlineReceipt{}}).validate(db); err != nil {
		return nil, err
	}
	var rows []rc49OnlineReceipt
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: unowned empty rc49 receipt table", ErrSchemaNotReady)
	}
	for _, row := range rows {
		if row.PlanChecksum != RC49OnlinePlanChecksum() || row.SourceDigest != RC49OnlineSourceDigest() ||
			(row.Step != "columns" && row.Step != "audit_index") || result[row.Step].Step != "" ||
			(row.State != "done" && (row.Step != "audit_index" || row.State != "building")) ||
			row.CreatedAt <= 0 || (row.State == "done" && row.CompletedAt < row.CreatedAt) || (row.State == "building" && row.CompletedAt != 0) {
			return nil, fmt.Errorf("%w: invalid rc49 expansion receipt", ErrChecksumMismatch)
		}
		result[row.Step] = row
	}
	return result, nil
}

func rc49AuditIndexState(db *gorm.DB) (string, error) {
	var rows []struct {
		Valid, Ready, Shape bool
	}
	err := db.Raw(`SELECT i.indisvalid AS valid, i.indisready AS ready,
 (i.indrelid=to_regclass('audit_logs') AND NOT i.indisunique AND i.indpred IS NULL AND i.indexprs IS NULL
 AND i.indnatts=2 AND i.indnkeyatts=2 AND am.amname='btree'
 AND (SELECT array_agg(a.attname::text ORDER BY k.pos) FROM unnest(i.indkey) WITH ORDINALITY k(attnum,pos)
 JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum)=ARRAY['created_at','event_id']::text[]) AS shape
 FROM pg_class idx LEFT JOIN pg_index i ON i.indexrelid=idx.oid
 LEFT JOIN pg_am am ON am.oid=idx.relam WHERE idx.oid=to_regclass('idx_audit_time_event')`).Scan(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "missing", nil
	}
	if len(rows) != 1 || !rows[0].Shape {
		return "", fmt.Errorf("%w: rc49 audit index has an unexpected object or shape", ErrSchemaNotReady)
	}
	if !rows[0].Valid || !rows[0].Ready {
		return "invalid", nil
	}
	return "valid", nil
}

func CheckRC49OnlineExpansion(ctx context.Context, db *gorm.DB) error {
	status, err := PlanRC49OnlineExpansion(ctx, db)
	if err != nil {
		return err
	}
	if status.State != "ready" {
		return fmt.Errorf("%w: rc49 expansion is %s", ErrSchemaNotReady, status.State)
	}
	return nil
}

// ApplyRC49OnlineExpansion changes only additive objects and independent
// receipts. The canonical21 ledger is never written, even on completed replay.
func ApplyRC49OnlineExpansion(ctx context.Context, db *gorm.DB, options Options) (*RC49OnlineStatus, error) {
	if ctx == nil || db == nil || db.Dialector.Name() != DialectPostgres {
		return nil, ErrUnsupportedDialect
	}
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, ErrUnsafeMigration
	}
	var status *RC49OnlineStatus
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		var err error
		status, err = PlanRC49OnlineExpansion(ctx, locked)
		if err != nil || options.DryRun {
			return err
		}
		if !status.Safe {
			return fmt.Errorf("%w: owned invalid audit index requires a new plan and explicit repair-index", ErrSchemaNotReady)
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if status.State == "ready" {
			return nil
		}
		if status.State == "pristine" {
			if err := locked.Transaction(func(tx *gorm.DB) error {
				// NOWAIT cannot queue an access-exclusive waiter ahead of user traffic.
				if err := tx.Exec("LOCK TABLE tokens, quota_data IN ACCESS EXCLUSIVE MODE NOWAIT").Error; err != nil {
					return err
				}
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, tx); err != nil {
						return err
					}
				}
				if err := tx.Migrator().CreateTable(&rc49OnlineReceipt{}); err != nil {
					return err
				}
				if err := executeMigrationSchema(tx, DialectPostgres, migrationSet()[RC49SchemaVersion-1]); err != nil {
					return err
				}
				if err := validateRC49Schema(tx, true); err != nil {
					return err
				}
				if options.AuthorizeMutation != nil {
					if err := options.AuthorizeMutation(ctx, tx); err != nil {
						return err
					}
				}
				now := time.Now().Unix()
				return tx.Create(&rc49OnlineReceipt{Step: "columns", PlanChecksum: RC49OnlinePlanChecksum(), SourceDigest: RC49OnlineSourceDigest(), State: "done", CreatedAt: now, CompletedAt: now}).Error
			}); err != nil {
				return err
			}
		}
		receipts, err := loadRC49OnlineReceipts(locked)
		if err != nil {
			return err
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if _, owned := receipts["audit_index"]; !owned {
			if err := locked.Create(&rc49OnlineReceipt{Step: "audit_index", PlanChecksum: RC49OnlinePlanChecksum(), SourceDigest: RC49OnlineSourceDigest(), State: "building", CreatedAt: time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		indexState, err := rc49AuditIndexState(locked)
		if err != nil {
			return err
		}
		if indexState == "missing" {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, locked); err != nil {
					return err
				}
			}
			// PostgreSQL forbids CONCURRENTLY in a transaction. The committed
			// building receipt owns any invalid remnant if this command fails.
			if err := locked.Exec(rc49ConcurrentAuditIndexSQL).Error; err != nil {
				return err
			}
		}
		if err := validateRC49AuditIndex(locked); err != nil {
			return err
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if err := locked.Model(&rc49OnlineReceipt{}).Where("step = ? AND state = ?", "audit_index", "building").Updates(map[string]any{"state": "done", "completed_at": time.Now().Unix()}).Error; err != nil {
			return err
		}
		status, err = PlanRC49OnlineExpansion(ctx, locked)
		return err
	})
	return status, err
}

// RepairRC49OnlineIndex never drops a valid or unowned index. It removes only
// the exact invalid remnant of this plan, leaving columns and ledger intact.
func RepairRC49OnlineIndex(ctx context.Context, db *gorm.DB, options Options) (*RC49OnlineStatus, error) {
	if ctx == nil || db == nil || db.Dialector.Name() != DialectPostgres {
		return nil, ErrUnsupportedDialect
	}
	if options.DryRun || (!options.Offline && options.AuthorizeMutation == nil) {
		return nil, ErrUnsafeMigration
	}
	var status *RC49OnlineStatus
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		var err error
		status, err = PlanRC49OnlineExpansion(ctx, locked)
		if err != nil {
			return err
		}
		if status.State != "index_invalid" {
			return fmt.Errorf("%w: repair-index requires an owned invalid rc49 index", ErrUnsafeMigration)
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if err := locked.Exec("DROP INDEX CONCURRENTLY idx_audit_time_event").Error; err != nil {
			return err
		}
		// Keep the building receipt. A subsequent freshly authorized apply can
		// recreate the now-missing owned index, including after process loss.
		status, err = PlanRC49OnlineExpansion(ctx, locked)
		return err
	})
	return status, err
}
