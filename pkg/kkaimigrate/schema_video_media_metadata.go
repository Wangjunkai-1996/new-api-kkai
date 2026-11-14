package kkaimigrate

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
)

// VideoMediaMetadataExpansion is an additive receipt outside the canonical
// migration ledger. The application remains compatible with canonical v21;
// the multimodal reference workflow enables only after this receipt is done.
const VideoMediaMetadataExpansion = "video-media-metadata-v1"

var videoMediaMetadataSchemaStatements = map[string][]migrationStatement{
	DialectSQLite: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT`},
	},
	DialectMySQL: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT`},
	},
	DialectPostgres: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT`},
	},
}

var videoMediaMetadataReceiptStatements = map[string]string{
	DialectSQLite: `CREATE TABLE IF NOT EXISTS kkai_video_media_metadata_steps (
step VARCHAR(32) PRIMARY KEY NOT NULL,
plan_checksum CHAR(64) NOT NULL,
source_digest VARCHAR(71) NOT NULL,
state VARCHAR(16) NOT NULL,
created_at INTEGER NOT NULL,
completed_at INTEGER NOT NULL
)`,
	DialectMySQL: `CREATE TABLE IF NOT EXISTS kkai_video_media_metadata_steps (
step VARCHAR(32) PRIMARY KEY NOT NULL,
plan_checksum CHAR(64) NOT NULL,
source_digest VARCHAR(71) NOT NULL,
state VARCHAR(16) NOT NULL,
created_at BIGINT NOT NULL,
completed_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	DialectPostgres: `CREATE TABLE IF NOT EXISTS kkai_video_media_metadata_steps (
step VARCHAR(32) PRIMARY KEY NOT NULL,
plan_checksum CHAR(64) NOT NULL,
source_digest VARCHAR(71) NOT NULL,
state VARCHAR(16) NOT NULL,
created_at BIGINT NOT NULL,
completed_at BIGINT NOT NULL
)`,
}

type videoMediaMetadataReceipt struct {
	Step         string `gorm:"type:varchar(32);primaryKey;not null"`
	PlanChecksum string `gorm:"type:char(64);not null"`
	SourceDigest string `gorm:"type:varchar(71);not null"`
	State        string `gorm:"type:varchar(16);not null"`
	CreatedAt    int64  `gorm:"not null"`
	CompletedAt  int64  `gorm:"not null"`
}

func (videoMediaMetadataReceipt) TableName() string { return "kkai_video_media_metadata_steps" }

type VideoMediaMetadataExpansionStatus struct {
	Schema          string `json:"schema"`
	CurrentVersion  int64  `json:"current_version"`
	TargetVersion   int64  `json:"target_version"`
	DatabaseName    string `json:"database_name,omitempty"`
	DatabaseSchema  string `json:"database_schema,omitempty"`
	ExpansionDigest string `json:"expansion_digest"`
	Dialect         string `json:"dialect"`
	State           string `json:"state"`
	PlanChecksum    string `json:"plan_checksum"`
	SourceDigest    string `json:"source_digest"`
	ColumnPresent   bool   `json:"column_present"`
	ReceiptPresent  bool   `json:"receipt_present"`
	Safe            bool   `json:"safe"`
}

func VideoMediaMetadataPlanChecksum() string {
	return tenantOnlineHash(VideoMediaMetadataExpansion + "; canonical21 unchanged; additive media_metadata TEXT column and done receipt\n" +
		videoMediaMetadataSchemaStatements[DialectSQLite][0].SQL + "\n" + videoMediaMetadataReceiptStatements[DialectSQLite] + "\n" +
		videoMediaMetadataSchemaStatements[DialectMySQL][0].SQL + "\n" + videoMediaMetadataReceiptStatements[DialectMySQL] + "\n" +
		videoMediaMetadataSchemaStatements[DialectPostgres][0].SQL + "\n" + videoMediaMetadataReceiptStatements[DialectPostgres])
}

func VideoMediaMetadataExpansionDigest() string {
	return "sha256:" + VideoMediaMetadataPlanChecksum()
}

func videoMediaMetadataSourceDigest(dialect string) string {
	return migrationSetDigest(dialect, planItemsForDialect(dialect, MembershipBillingRecoverySchemaVersion))
}

func PlanVideoMediaMetadataExpansion(ctx context.Context, db *gorm.DB) (*VideoMediaMetadataExpansionStatus, error) {
	if ctx == nil || db == nil {
		return nil, ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}
	if err := checkThroughVersion(ctx, db, MembershipBillingRecoverySchemaVersion, MembershipBillingRecoverySchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, err
	}
	columnPresent := db.Migrator().HasColumn("kkai_video_assets", "media_metadata")
	status := &VideoMediaMetadataExpansionStatus{
		Schema: "kkai_video_media_metadata_expansion_v1", CurrentVersion: MembershipBillingRecoverySchemaVersion, TargetVersion: MembershipBillingRecoverySchemaVersion, Dialect: dialect,
		State: "pristine", PlanChecksum: VideoMediaMetadataPlanChecksum(),
		SourceDigest: videoMediaMetadataSourceDigest(dialect), ColumnPresent: columnPresent,
		Safe: true, ExpansionDigest: VideoMediaMetadataExpansionDigest(),
	}
	if dialect == DialectPostgres {
		status.DatabaseName, status.DatabaseSchema, err = TenantOnlineDatabaseIdentity(ctx, db)
		if err != nil {
			return nil, err
		}
	}
	if !db.Migrator().HasTable(&videoMediaMetadataReceipt{}) {
		if columnPresent {
			return nil, fmt.Errorf("%w: unreceipted video media metadata column", ErrSchemaNotReady)
		}
		return status, nil
	}
	if err := (tenantOnlineStep{key: "video_media_metadata_receipts", table: "kkai_video_media_metadata_steps", model: &videoMediaMetadataReceipt{}}).validate(db); err != nil {
		return nil, err
	}
	var receipts []videoMediaMetadataReceipt
	if err := db.WithContext(ctx).Find(&receipts).Error; err != nil {
		return nil, err
	}
	if len(receipts) == 0 {
		if columnPresent {
			return nil, fmt.Errorf("%w: unreceipted video media metadata column", ErrSchemaNotReady)
		}
		status.State = "receipt_missing"
		return status, nil
	}
	if len(receipts) != 1 || receipts[0].Step != "column" {
		return nil, fmt.Errorf("%w: invalid video media metadata receipt", ErrSchemaNotReady)
	}
	receipt := receipts[0]
	status.ReceiptPresent = true
	status.State = receipt.State
	if receipt.PlanChecksum != status.PlanChecksum || receipt.SourceDigest != status.SourceDigest ||
		(receipt.State != "building" && receipt.State != "done") || receipt.CreatedAt <= 0 ||
		(receipt.State == "done" && receipt.CompletedAt < receipt.CreatedAt) ||
		(receipt.State == "building" && receipt.CompletedAt != 0) {
		return nil, fmt.Errorf("%w: invalid video media metadata receipt", ErrChecksumMismatch)
	}
	if receipt.State == "done" {
		if !columnPresent {
			return nil, fmt.Errorf("%w: completed video media metadata column is missing", ErrSchemaNotReady)
		}
		if err := validateVideoMediaMetadataColumn(db, dialect); err != nil {
			return nil, err
		}
		status.State = "ready"
		return status, nil
	}
	if columnPresent {
		if err := validateVideoMediaMetadataColumn(db, dialect); err != nil {
			return nil, err
		}
		status.State = "column_ready"
	}
	return status, nil
}

func CheckVideoMediaMetadataExpansion(ctx context.Context, db *gorm.DB) error {
	status, err := PlanVideoMediaMetadataExpansion(ctx, db)
	if err != nil {
		return err
	}
	if status.State != "ready" {
		return fmt.Errorf("%w: video media metadata expansion is %s", ErrSchemaNotReady, status.State)
	}
	return nil
}

func ApplyVideoMediaMetadataExpand(ctx context.Context, db *gorm.DB, options Options) (*VideoMediaMetadataExpansionStatus, error) {
	if ctx == nil || db == nil {
		return nil, ErrSchemaNotReady
	}
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, ErrUnsafeMigration
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}
	if dialect != DialectPostgres && !options.Offline && !options.DryRun {
		return nil, fmt.Errorf("%w: non-PostgreSQL media metadata expansion requires offline mode", ErrUnsafeMigration)
	}
	var status *VideoMediaMetadataExpansionStatus
	err = withMigrationLock(ctx, db, dialect, func(locked *gorm.DB) error {
		var planErr error
		status, planErr = PlanVideoMediaMetadataExpansion(ctx, locked)
		if planErr != nil {
			return planErr
		}
		if options.DryRun {
			return nil
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if status.State == "ready" {
			return nil
		}
		if !locked.Migrator().HasTable(&videoMediaMetadataReceipt{}) {
			if err := locked.Exec(videoMediaMetadataReceiptStatements[dialect]).Error; err != nil {
				return err
			}
			if err := locked.Create(&videoMediaMetadataReceipt{
				Step: "column", PlanChecksum: status.PlanChecksum, SourceDigest: status.SourceDigest,
				State: "building", CreatedAt: time.Now().Unix(),
			}).Error; err != nil {
				return err
			}
			status.State, status.ReceiptPresent = "building", true
		} else if !status.ReceiptPresent || status.State == "receipt_missing" {
			if status.State != "receipt_missing" {
				return fmt.Errorf("%w: video media metadata receipt is missing", ErrSchemaNotReady)
			}
			if err := locked.Create(&videoMediaMetadataReceipt{
				Step: "column", PlanChecksum: status.PlanChecksum, SourceDigest: status.SourceDigest,
				State: "building", CreatedAt: time.Now().Unix(),
			}).Error; err != nil {
				return err
			}
			status.State, status.ReceiptPresent = "building", true
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if !locked.Migrator().HasColumn("kkai_video_assets", "media_metadata") {
			var ddlErr error
			if dialect == DialectPostgres {
				ddlErr = locked.Transaction(func(tx *gorm.DB) error {
					if err := tx.Exec("LOCK TABLE kkai_video_assets IN ACCESS EXCLUSIVE MODE NOWAIT").Error; err != nil {
						return err
					}
					return executeMigrationStatement(tx, dialect, videoMediaMetadataSchemaStatements[dialect][0])
				})
			} else {
				ddlErr = executeMigrationStatement(locked, dialect, videoMediaMetadataSchemaStatements[dialect][0])
			}
			if ddlErr != nil {
				return ddlErr
			}
		}
		if err := validateVideoMediaMetadataColumn(locked, dialect); err != nil {
			return err
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		if err := locked.Model(&videoMediaMetadataReceipt{}).Where("step = ?", "column").Updates(map[string]any{
			"state": "done", "completed_at": time.Now().Unix(),
		}).Error; err != nil {
			return err
		}
		status.State = "ready"
		status.ColumnPresent, status.ReceiptPresent, status.Safe = true, true, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func validateVideoMediaMetadataColumn(db *gorm.DB, dialect string) error {
	columnTypes, err := db.Migrator().ColumnTypes("kkai_video_assets")
	if err != nil {
		return err
	}
	for _, column := range columnTypes {
		if !strings.EqualFold(column.Name(), "media_metadata") {
			continue
		}
		typeName := strings.ToLower(column.DatabaseTypeName())
		if typeName != "text" {
			return fmt.Errorf("%w: %s media_metadata type is %s", ErrSchemaNotReady, dialect, typeName)
		}
		nullable, hasNullable := column.Nullable()
		if hasNullable && !nullable {
			return fmt.Errorf("%w: media_metadata must be nullable", ErrSchemaNotReady)
		}
		if value, hasDefault := column.DefaultValue(); hasDefault && !(dialect == DialectMySQL && strings.EqualFold(value, "NULL")) {
			return fmt.Errorf("%w: media_metadata must not have a default", ErrSchemaNotReady)
		}
		return nil
	}
	return fmt.Errorf("%w: missing kkai_video_assets.media_metadata", ErrSchemaNotReady)
}

// ValidateVideoMediaMetadataRuntimeContract accepts the exact application
// contracts before and after this nullable-column expansion.
func ValidateVideoMediaMetadataRuntimeContract(contract SchemaContract) error {
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
	return fmt.Errorf("%w: runtime does not have an exact video media metadata-compatible contract", ErrUnsafeMigration)
}
