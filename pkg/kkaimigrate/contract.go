package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	// RuntimeMinVersion is the minimum KKAI schema version required by this runtime.
	RuntimeMinVersion int64 = 4
	// RuntimeMaxVersion is the highest already-applied schema this runtime accepts.
	RuntimeMaxVersion int64 = 4
	// MigrationTargetVersion is the schema version reached by this image's migration set.
	MigrationTargetVersion int64 = 4

	MigrationKindNone     = "none"
	MigrationKindExpand   = "expand"
	MigrationKindContract = "contract"
)

// SchemaContract is embedded in the production image and copied into release evidence.
// Its fixed field order makes the JSON representation suitable for signature-bound labels.
type SchemaContract struct {
	Schema                 int    `json:"schema"`
	SourceRevision         string `json:"source_revision"`
	RuntimeMinVersion      int64  `json:"runtime_min_version"`
	RuntimeMaxVersion      int64  `json:"runtime_max_version"`
	MigrationTargetVersion int64  `json:"migration_target_version"`
	MigrationKind          string `json:"migration_kind"`
	MigrationSetDigest     string `json:"migration_set_digest"`
}

// SchemaObservation is a read-only, validated description of the database's
// KKAI-managed migration history. It is used by the deployer before it decides
// whether an automatic migration is allowed.
type SchemaObservation struct {
	Schema             int    `json:"schema"`
	CurrentVersion     int64  `json:"current_version"`
	MigrationSetDigest string `json:"migration_set_digest"`
}

var sourceRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func SchemaCompatibility(sourceRevision string) SchemaContract {
	migrationKind, err := migrationKindForRange(RuntimeMinVersion, MigrationTargetVersion, migrationSet())
	if err != nil {
		panic("invalid KKAI migration catalog: " + err.Error())
	}
	return SchemaContract{
		Schema:                 1,
		SourceRevision:         sourceRevision,
		RuntimeMinVersion:      RuntimeMinVersion,
		RuntimeMaxVersion:      RuntimeMaxVersion,
		MigrationTargetVersion: MigrationTargetVersion,
		MigrationKind:          migrationKind,
		MigrationSetDigest:     "sha256:" + migrationSetSHA256(contractPlanThroughVersion(RuntimeMaxVersion)),
	}
}

func (contract SchemaContract) CanonicalJSON() ([]byte, error) {
	if contract.Schema != 1 || !sourceRevisionPattern.MatchString(contract.SourceRevision) || contract.RuntimeMinVersion <= 0 ||
		contract.RuntimeMinVersion > contract.RuntimeMaxVersion || contract.RuntimeMaxVersion > latestKnownVersion() ||
		!regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(contract.MigrationSetDigest) {
		return nil, fmt.Errorf("invalid KKAI schema compatibility contract")
	}
	switch contract.MigrationKind {
	case MigrationKindNone:
		if contract.MigrationTargetVersion != contract.RuntimeMinVersion {
			return nil, fmt.Errorf("invalid KKAI schema compatibility contract")
		}
	case MigrationKindExpand, MigrationKindContract:
		if contract.MigrationTargetVersion <= contract.RuntimeMinVersion ||
			contract.MigrationTargetVersion > contract.RuntimeMaxVersion {
			return nil, fmt.Errorf("invalid KKAI schema compatibility contract")
		}
	default:
		return nil, fmt.Errorf("invalid KKAI schema compatibility contract")
	}
	return common.Marshal(contract)
}

func migrationSetSHA256(plan []AppliedMigration) string {
	type canonicalMigration struct {
		Version  int64  `json:"version"`
		Name     string `json:"name"`
		Checksum string `json:"checksum"`
	}
	canonical := make([]canonicalMigration, 0, len(plan))
	for _, item := range plan {
		canonical = append(canonical, canonicalMigration{
			Version: item.Version, Name: item.Name, Checksum: item.Checksum,
		})
	}
	encoded, err := common.Marshal(canonical)
	if err != nil {
		panic("marshal canonical KKAI migration set: " + err.Error())
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func Observe(ctx context.Context, db *gorm.DB) (SchemaObservation, error) {
	if err := validateMigrationCatalog(migrationSet()); err != nil {
		return SchemaObservation{}, err
	}
	if db == nil || !db.Migrator().HasTable((AppliedMigration{}).TableName()) {
		return SchemaObservation{}, ErrSchemaNotReady
	}
	applied, err := loadApplied(db.WithContext(ctx))
	if err != nil {
		return SchemaObservation{}, err
	}
	if len(applied) == 0 {
		return SchemaObservation{}, ErrSchemaNotReady
	}
	if err := validateApplied(applied, latestKnownVersion()); err != nil {
		return SchemaObservation{}, err
	}
	plan := Plan()
	observedVersion := int64(0)
	for _, item := range plan {
		stored, ok := applied[item.Version]
		if !ok {
			if int(observedVersion) != len(applied) {
				return SchemaObservation{}, ErrSchemaNotReady
			}
			break
		}
		observedVersion = stored.Version
	}
	if int(observedVersion) != len(applied) {
		return SchemaObservation{}, ErrSchemaNotReady
	}
	return SchemaObservation{
		Schema:             1,
		CurrentVersion:     observedVersion,
		MigrationSetDigest: "sha256:" + migrationSetSHA256(contractPlanThroughVersion(observedVersion)),
	}, nil
}
