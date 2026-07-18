package kkaimigrate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const upstreamSchemaBaselineDigestV1 = "sha256:b0a50c47c87852d26e0d6c063557e64864e30967ef24f2400ed2bc0b58a7b1a7"

var upstreamSchemaDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type upstreamSchemaOwnershipEntry struct {
	Version                 int
	MigrationVersion        int64
	Kind                    string
	ImplementationID        string
	BeforeModelSchemaDigest string
	AfterModelSchemaDigest  string
}

var upstreamSchemaOwnershipCatalog = []upstreamSchemaOwnershipEntry{
	{
		Version:                 1,
		MigrationVersion:        0,
		Kind:                    "baseline",
		ImplementationID:        "upstream_schema_baseline_v1",
		BeforeModelSchemaDigest: upstreamSchemaBaselineDigestV1,
		AfterModelSchemaDigest:  upstreamSchemaBaselineDigestV1,
	},
}

type UpstreamSchemaContract struct {
	Schema                    int    `json:"schema"`
	SourceRevision            string `json:"source_revision"`
	CatalogVersion            int    `json:"catalog_version"`
	ModelSchemaDigest         string `json:"model_schema_digest"`
	BeforeModelSchemaDigest   string `json:"before_model_schema_digest"`
	AfterModelSchemaDigest    string `json:"after_model_schema_digest"`
	MigrationVersion          int64  `json:"migration_version"`
	MigrationKind             string `json:"migration_kind"`
	OwnershipImplementationID string `json:"ownership_implementation_id"`
}

type UpstreamSchemaAdoption struct {
	Schema                    int                              `json:"schema"`
	SourceRevision            string                           `json:"source_revision"`
	CatalogVersion            int                              `json:"catalog_version"`
	ModelSchemaDigest         string                           `json:"model_schema_digest"`
	BeforeModelSchemaDigest   string                           `json:"before_model_schema_digest"`
	AfterModelSchemaDigest    string                           `json:"after_model_schema_digest"`
	MigrationVersion          int64                            `json:"migration_version"`
	MigrationKind             string                           `json:"migration_kind"`
	OwnershipImplementationID string                           `json:"ownership_implementation_id"`
	Dialect                   string                           `json:"dialect"`
	Ready                     bool                             `json:"ready"`
	MissingTables             []string                         `json:"missing_tables"`
	MissingColumns            []string                         `json:"missing_columns"`
	Differences               []model.UpstreamSchemaDifference `json:"differences"`
}

func UpstreamSchemaCompatibility(sourceRevision string) (UpstreamSchemaContract, error) {
	if !sourceRevisionPattern.MatchString(sourceRevision) {
		return UpstreamSchemaContract{}, fmt.Errorf("invalid source revision")
	}
	if err := validateMigrationCatalog(migrationSet()); err != nil {
		return UpstreamSchemaContract{}, err
	}
	latest := upstreamSchemaOwnershipCatalog[len(upstreamSchemaOwnershipCatalog)-1]
	return UpstreamSchemaContract{
		Schema:                    1,
		SourceRevision:            sourceRevision,
		CatalogVersion:            latest.Version,
		ModelSchemaDigest:         latest.AfterModelSchemaDigest,
		BeforeModelSchemaDigest:   latest.BeforeModelSchemaDigest,
		AfterModelSchemaDigest:    latest.AfterModelSchemaDigest,
		MigrationVersion:          latest.MigrationVersion,
		MigrationKind:             latest.Kind,
		OwnershipImplementationID: latest.ImplementationID,
	}, nil
}

func (contract UpstreamSchemaContract) CanonicalJSON() ([]byte, error) {
	if contract.Schema != 1 || !sourceRevisionPattern.MatchString(contract.SourceRevision) ||
		contract.CatalogVersion <= 0 || !upstreamSchemaDigestPattern.MatchString(contract.ModelSchemaDigest) ||
		!upstreamSchemaDigestPattern.MatchString(contract.BeforeModelSchemaDigest) ||
		!upstreamSchemaDigestPattern.MatchString(contract.AfterModelSchemaDigest) ||
		contract.ModelSchemaDigest != contract.AfterModelSchemaDigest ||
		contract.OwnershipImplementationID == "" ||
		contract.OwnershipImplementationID != strings.TrimSpace(contract.OwnershipImplementationID) {
		return nil, fmt.Errorf("invalid upstream schema compatibility contract")
	}
	if contract.CatalogVersion == 1 {
		if contract.MigrationVersion != 0 || contract.MigrationKind != "baseline" ||
			contract.BeforeModelSchemaDigest != contract.AfterModelSchemaDigest {
			return nil, fmt.Errorf("invalid upstream schema compatibility contract")
		}
	} else if contract.MigrationVersion <= 0 ||
		(contract.MigrationKind != MigrationKindExpand && contract.MigrationKind != MigrationKindContract) ||
		contract.BeforeModelSchemaDigest == contract.AfterModelSchemaDigest {
		return nil, fmt.Errorf("invalid upstream schema compatibility contract")
	}
	return common.Marshal(contract)
}

func ObserveUpstreamSchema(ctx context.Context, db *gorm.DB, sourceRevision string) (UpstreamSchemaAdoption, error) {
	contract, err := UpstreamSchemaCompatibility(sourceRevision)
	if err != nil {
		return UpstreamSchemaAdoption{}, err
	}
	observation, err := model.ObserveUpstreamSchema(ctx, db)
	if err != nil {
		return UpstreamSchemaAdoption{}, err
	}
	if observation.ModelSchemaDigest != contract.ModelSchemaDigest {
		return UpstreamSchemaAdoption{}, fmt.Errorf("upstream model schema digest does not match the ownership catalog")
	}
	return UpstreamSchemaAdoption{
		Schema:                    1,
		SourceRevision:            sourceRevision,
		CatalogVersion:            contract.CatalogVersion,
		ModelSchemaDigest:         contract.ModelSchemaDigest,
		BeforeModelSchemaDigest:   contract.BeforeModelSchemaDigest,
		AfterModelSchemaDigest:    contract.AfterModelSchemaDigest,
		MigrationVersion:          contract.MigrationVersion,
		MigrationKind:             contract.MigrationKind,
		OwnershipImplementationID: contract.OwnershipImplementationID,
		Dialect:                   observation.Dialect,
		Ready:                     observation.Ready,
		MissingTables:             observation.MissingTables,
		MissingColumns:            observation.MissingColumns,
		Differences:               observation.Differences,
	}, nil
}

func (adoption UpstreamSchemaAdoption) CanonicalJSON() ([]byte, error) {
	if adoption.Schema != 1 || !sourceRevisionPattern.MatchString(adoption.SourceRevision) ||
		adoption.CatalogVersion <= 0 || !upstreamSchemaDigestPattern.MatchString(adoption.ModelSchemaDigest) ||
		!upstreamSchemaDigestPattern.MatchString(adoption.BeforeModelSchemaDigest) ||
		!upstreamSchemaDigestPattern.MatchString(adoption.AfterModelSchemaDigest) ||
		adoption.ModelSchemaDigest != adoption.AfterModelSchemaDigest ||
		adoption.OwnershipImplementationID == "" ||
		adoption.OwnershipImplementationID != strings.TrimSpace(adoption.OwnershipImplementationID) ||
		adoption.Dialect == "" ||
		adoption.MissingTables == nil || adoption.MissingColumns == nil || adoption.Differences == nil {
		return nil, fmt.Errorf("invalid upstream schema adoption evidence")
	}
	if adoption.CatalogVersion == 1 {
		if adoption.MigrationVersion != 0 || adoption.MigrationKind != "baseline" ||
			adoption.BeforeModelSchemaDigest != adoption.AfterModelSchemaDigest {
			return nil, fmt.Errorf("invalid upstream schema adoption evidence")
		}
	} else if adoption.MigrationVersion <= 0 ||
		(adoption.MigrationKind != MigrationKindExpand && adoption.MigrationKind != MigrationKindContract) ||
		adoption.BeforeModelSchemaDigest == adoption.AfterModelSchemaDigest {
		return nil, fmt.Errorf("invalid upstream schema adoption evidence")
	}
	return common.Marshal(adoption)
}

func validateUpstreamSchemaOwnership(migrations []migration) error {
	byVersion := make(map[int64]migration, len(migrations))
	for _, item := range migrations {
		byVersion[item.Version] = item
	}
	if len(upstreamSchemaOwnershipCatalog) == 0 {
		return unsafeMigrationCatalog("upstream schema ownership catalog is empty")
	}
	entryByVersion := make(map[int]upstreamSchemaOwnershipEntry, len(upstreamSchemaOwnershipCatalog))
	implementationIDs := make(map[string]struct{}, len(upstreamSchemaOwnershipCatalog))
	previousMigrationVersion := int64(0)
	previousSchemaDigest := ""
	for index, entry := range upstreamSchemaOwnershipCatalog {
		expectedVersion := index + 1
		if entry.Version != expectedVersion ||
			entry.ImplementationID == "" || entry.ImplementationID != strings.TrimSpace(entry.ImplementationID) ||
			!upstreamSchemaDigestPattern.MatchString(entry.BeforeModelSchemaDigest) ||
			!upstreamSchemaDigestPattern.MatchString(entry.AfterModelSchemaDigest) {
			return unsafeMigrationCatalog("upstream schema ownership entry %d is invalid", expectedVersion)
		}
		if _, duplicate := implementationIDs[entry.ImplementationID]; duplicate {
			return unsafeMigrationCatalog("upstream schema ownership implementation ID %q is duplicated", entry.ImplementationID)
		}
		implementationIDs[entry.ImplementationID] = struct{}{}
		if entry.Version == 1 {
			if entry.Kind != "baseline" || entry.MigrationVersion != 0 ||
				entry.BeforeModelSchemaDigest != entry.AfterModelSchemaDigest {
				return unsafeMigrationCatalog("upstream schema ownership baseline is invalid")
			}
		} else {
			if entry.Kind != MigrationKindExpand && entry.Kind != MigrationKindContract ||
				entry.MigrationVersion <= previousMigrationVersion ||
				entry.BeforeModelSchemaDigest != previousSchemaDigest ||
				entry.AfterModelSchemaDigest == entry.BeforeModelSchemaDigest {
				return unsafeMigrationCatalog("upstream schema ownership entry %d has an invalid semantic transition", entry.Version)
			}
			linked, exists := byVersion[entry.MigrationVersion]
			if !exists || linked.UpstreamSchemaVersion != entry.Version || linked.Kind != entry.Kind {
				return unsafeMigrationCatalog("upstream schema ownership entry %d has no matching migration", entry.Version)
			}
		}
		entryByVersion[entry.Version] = entry
		previousMigrationVersion = entry.MigrationVersion
		previousSchemaDigest = entry.AfterModelSchemaDigest
	}
	for _, item := range migrations {
		if item.UpstreamSchemaVersion == 0 {
			continue
		}
		entry, exists := entryByVersion[item.UpstreamSchemaVersion]
		if !exists || entry.MigrationVersion != item.Version {
			return unsafeMigrationCatalog("migration %d references unknown upstream schema version %d", item.Version, item.UpstreamSchemaVersion)
		}
	}
	latest := upstreamSchemaOwnershipCatalog[len(upstreamSchemaOwnershipCatalog)-1]
	actualDigest, err := model.UpstreamSchemaDigest()
	if err != nil {
		return unsafeMigrationCatalog("calculate upstream model schema digest: %v", err)
	}
	if actualDigest != latest.AfterModelSchemaDigest {
		return unsafeMigrationCatalog(
			"upstream model schema digest %s does not match catalog version %d digest %s",
			actualDigest, latest.Version, latest.AfterModelSchemaDigest,
		)
	}
	return nil
}

func upstreamSchemaDigestsForVersion(version int) (string, string) {
	if version <= 0 {
		return "", ""
	}
	for _, entry := range upstreamSchemaOwnershipCatalog {
		if entry.Version == version {
			return entry.BeforeModelSchemaDigest, entry.AfterModelSchemaDigest
		}
	}
	return "unknown", "unknown"
}

func upstreamSchemaImplementationIDForVersion(version int) string {
	if version <= 0 {
		return ""
	}
	for _, entry := range upstreamSchemaOwnershipCatalog {
		if entry.Version == version {
			return entry.ImplementationID
		}
	}
	return "unknown"
}
