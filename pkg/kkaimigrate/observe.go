package kkaimigrate

import (
	"context"

	"gorm.io/gorm"
)

type Observation struct {
	Schema             int               `json:"schema"`
	CurrentVersion     int64             `json:"current_version"`
	MigrationSetDigest string            `json:"migration_set_digest"`
	Expansions         map[string]string `json:"expansions,omitempty"`
}

type validatedMigrationState struct {
	dialect        string
	currentVersion int64
	prefix         []migration
}

func loadValidatedState(ctx context.Context, db *gorm.DB, compatibleVersion int64) (*validatedMigrationState, error) {
	if db == nil || !db.Migrator().HasTable((AppliedMigration{}).TableName()) {
		return nil, ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}
	return loadValidatedStateFromMigrationSet(ctx, db, dialect, compatibleVersion, migrationSet())
}

func loadValidatedStateFromMigrationSet(
	ctx context.Context,
	db *gorm.DB,
	dialect string,
	compatibleVersion int64,
	migrations []migration,
	physicalMaxVersion ...int64,
) (*validatedMigrationState, error) {
	if db == nil || !db.Migrator().HasTable((AppliedMigration{}).TableName()) {
		return nil, ErrSchemaNotReady
	}
	if err := validateMigrationCatalog(migrations); err != nil {
		return nil, err
	}
	applied, err := loadApplied(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if err := validateAppliedAgainstMigrationSet(applied, dialect, compatibleVersion, migrations); err != nil {
		return nil, err
	}
	var current int64
	for version := range applied {
		if version > current {
			current = version
		}
	}
	if current == 0 {
		return nil, ErrSchemaNotReady
	}
	physicalVersion := current
	if len(physicalMaxVersion) > 0 && physicalVersion > physicalMaxVersion[0] {
		physicalVersion = physicalMaxVersion[0]
	}
	if err := validateRuntimeSchema(db.WithContext(ctx), dialect, physicalVersion); err != nil {
		return nil, err
	}
	items, ok := storedPrefixItemsForDialectFromSet(dialect, current, migrations)
	if !ok {
		return nil, ErrSchemaNotReady
	}
	for _, item := range items {
		if _, exists := applied[item.Version]; !exists {
			return nil, ErrMigrationHole
		}
	}
	return &validatedMigrationState{
		dialect:        dialect,
		currentVersion: current,
		prefix:         items,
	}, nil
}

func Observe(ctx context.Context, db *gorm.DB) (*Observation, error) {
	state, err := loadValidatedState(ctx, db, MaxCompatibleVersion)
	if err != nil {
		return nil, err
	}
	result := &Observation{
		Schema:             MigrationContractSchema,
		CurrentVersion:     state.currentVersion,
		MigrationSetDigest: migrationSetDigest(state.dialect, state.prefix),
	}
	if state.dialect == DialectPostgres && state.currentVersion == MembershipBillingRecoverySchemaVersion && db.Migrator().HasTable(&rc49OnlineReceipt{}) {
		if err := CheckRC49OnlineExpansion(ctx, db); err != nil {
			return nil, err
		}
		result.Expansions = map[string]string{RC49OnlineExpansion: RC49OnlineExpansionDigest()}
	}
	if db.Migrator().HasTable(&videoMediaMetadataReceipt{}) {
		if err := CheckVideoMediaMetadataExpansion(ctx, db); err != nil {
			return nil, err
		}
		if result.Expansions == nil {
			result.Expansions = map[string]string{}
		}
		result.Expansions[VideoMediaMetadataExpansion] = VideoMediaMetadataExpansionDigest()
	}
	return result, nil
}
