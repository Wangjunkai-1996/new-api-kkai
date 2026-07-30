package kkaimigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	KnownCatalogVersion     int64 = 6
	MigrationContractSchema       = 1

	MigrationKindNone     = "none"
	MigrationKindExpand   = "expand"
	MigrationKindContract = "contract"
)

type SchemaContract struct {
	RuntimeMinVersion      int64             `json:"runtime_min_version"`
	RuntimeMaxVersion      int64             `json:"runtime_max_version"`
	MigrationTargetVersion int64             `json:"migration_target_version"`
	MigrationKind          string            `json:"migration_kind"`
	MigrationSetDigest     string            `json:"migration_set_digest"`
	CompatiblePrefixes     map[string]string `json:"compatible_prefixes"`
}

func ContractForDialect(dialect string) (SchemaContract, error) {
	if err := validateMigrationCatalog(migrationSet()); err != nil {
		return SchemaContract{}, err
	}
	target, err := RequiredVersionForDialect(dialect)
	if err != nil {
		return SchemaContract{}, err
	}
	prefixes, err := compatiblePrefixDigests(dialect, target, MaxCompatibleVersion)
	if err != nil {
		return SchemaContract{}, err
	}
	return SchemaContract{
		RuntimeMinVersion:      target,
		RuntimeMaxVersion:      MaxCompatibleVersion,
		MigrationTargetVersion: target,
		MigrationKind:          MigrationKindNone,
		MigrationSetDigest:     migrationSetDigest(dialect, planItemsForDialect(dialect, target)),
		CompatiblePrefixes:     prefixes,
	}, nil
}

func RequiredVersionForDialect(dialect string) (int64, error) {
	switch dialect {
	case DialectSQLite, DialectMySQL, DialectPostgres:
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedDialect, dialect)
	}
	required := RequiredRuntimeVersion
	items := planItemsForDialect(dialect, required)
	if len(items) == 0 || items[len(items)-1].Version != required {
		return 0, fmt.Errorf("KKAI migration catalog does not provide required %s version %d", dialect, required)
	}
	return required, nil
}

func Catalog() []AppliedMigration {
	return planItems(migrationSet())
}

// Plan is kept as the historical name for the immutable full catalog.
// Runtime callers should use PlanForDialect so MySQL-only migrations never
// become part of a PostgreSQL release plan.
func Plan() []AppliedMigration {
	return Catalog()
}

func PlanForDialect(dialect string) ([]AppliedMigration, error) {
	target, err := RequiredVersionForDialect(dialect)
	if err != nil {
		return nil, err
	}
	return planItems(planItemsForDialect(dialect, target)), nil
}

func planItemsForDialect(dialect string, target int64) []migration {
	return planItemsForDialectFromSet(dialect, target, migrationSet())
}

func planItemsForDialectFromSet(dialect string, target int64, migrations []migration) []migration {
	items := make([]migration, 0, target)
	for _, item := range migrations {
		if item.Version > target {
			break
		}
		if item.appliesTo(dialect) {
			items = append(items, item)
		}
	}
	return items
}

func planItems(items []migration) []AppliedMigration {
	result := make([]AppliedMigration, 0, len(items))
	for _, item := range items {
		result = append(result, AppliedMigration{
			Version:  item.Version,
			Name:     item.Name,
			Checksum: migrationChecksum(item),
		})
	}
	return result
}

func migrationSetDigest(dialect string, items []migration) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "schema=%d\ndialect=%s\n", MigrationContractSchema, dialect)
	for _, item := range items {
		fmt.Fprintf(hash, "version=%d\nname=%s\nchecksum=%s\n", item.Version, item.Name, migrationChecksum(item))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func compatiblePrefixDigests(dialect string, minimum int64, maximum int64) (map[string]string, error) {
	result := make(map[string]string, maximum-minimum+1)
	for version := minimum; version <= maximum; version++ {
		items, ok := storedPrefixItemsForDialect(dialect, version)
		if !ok {
			return nil, fmt.Errorf("KKAI migration catalog has no canonical %s prefix at version %d", dialect, version)
		}
		result[strconv.FormatInt(version, 10)] = migrationSetDigest(dialect, items)
	}
	return result, nil
}

func storedPrefixItemsForDialect(dialect string, version int64) ([]migration, bool) {
	return storedPrefixItemsForDialectFromSet(dialect, version, migrationSet())
}

func storedPrefixItemsForDialectFromSet(dialect string, version int64, migrations []migration) ([]migration, bool) {
	items := make([]migration, 0, version)
	for _, item := range migrations {
		if item.Version > version {
			break
		}
		if !item.acceptsStoredDialect(dialect) {
			return nil, false
		}
		items = append(items, item)
	}
	if len(items) == 0 || items[len(items)-1].Version != version {
		return nil, false
	}
	return items, true
}

func (item migration) appliesTo(dialect string) bool {
	if len(item.ApplyDialects) == 0 {
		return true
	}
	return containsDialect(item.ApplyDialects, dialect)
}

func (item migration) acceptsStoredDialect(dialect string) bool {
	return item.appliesTo(dialect) || containsDialect(item.LegacyDialects, dialect)
}

func containsDialect(dialects []string, dialect string) bool {
	for _, candidate := range dialects {
		if candidate == dialect {
			return true
		}
	}
	return false
}
