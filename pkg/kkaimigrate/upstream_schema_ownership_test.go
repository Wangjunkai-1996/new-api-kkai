package kkaimigrate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpstreamSchemaOwnershipCatalogMatchesCanonicalModels(t *testing.T) {
	digest, err := model.UpstreamSchemaDigest()
	require.NoError(t, err)
	require.Equal(t, upstreamSchemaBaselineDigestV1, digest)
	require.NoError(t, validateMigrationCatalog(migrationSet()))

	contract, err := UpstreamSchemaCompatibility("0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)
	require.Equal(t, 1, contract.CatalogVersion)
	require.Equal(t, digest, contract.ModelSchemaDigest)
	require.Equal(t, digest, contract.BeforeModelSchemaDigest)
	require.Equal(t, digest, contract.AfterModelSchemaDigest)
	require.Zero(t, contract.MigrationVersion)
	require.Equal(t, "baseline", contract.MigrationKind)
	require.Equal(t, "upstream_schema_baseline_v1", contract.OwnershipImplementationID)
}

func TestUpstreamSchemaOwnershipRejectsDuplicateImplementationID(t *testing.T) {
	original := append([]upstreamSchemaOwnershipEntry(nil), upstreamSchemaOwnershipCatalog...)
	t.Cleanup(func() { upstreamSchemaOwnershipCatalog = original })
	upstreamSchemaOwnershipCatalog = append(upstreamSchemaOwnershipCatalog, upstreamSchemaOwnershipEntry{
		Version:                 2,
		MigrationVersion:        5,
		Kind:                    MigrationKindExpand,
		ImplementationID:        original[0].ImplementationID,
		BeforeModelSchemaDigest: upstreamSchemaBaselineDigestV1,
		AfterModelSchemaDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	migrations := append(migrationSet(), migration{
		Version: 5, Kind: MigrationKindExpand, UpstreamSchemaVersion: 2,
	})
	require.ErrorIs(t, validateUpstreamSchemaOwnership(migrations), ErrUnsafeMigration)
}

func TestUpstreamSchemaOwnershipRejectsDigestDrift(t *testing.T) {
	original := upstreamSchemaOwnershipCatalog[0].AfterModelSchemaDigest
	upstreamSchemaOwnershipCatalog[0].AfterModelSchemaDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	t.Cleanup(func() {
		upstreamSchemaOwnershipCatalog[0].AfterModelSchemaDigest = original
	})
	require.ErrorIs(t, validateMigrationCatalog(migrationSet()), ErrUnsafeMigration)
}

func TestUpstreamSchemaOwnershipRejectsUnknownMigrationLink(t *testing.T) {
	migrations := migrationSet()
	migrations[len(migrations)-1].UpstreamSchemaVersion = 2
	require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
}

func TestUpstreamSchemaOwnershipBindsSemanticTransitionAndMigrationKind(t *testing.T) {
	original := append([]upstreamSchemaOwnershipEntry(nil), upstreamSchemaOwnershipCatalog...)
	t.Cleanup(func() { upstreamSchemaOwnershipCatalog = original })
	upstreamSchemaOwnershipCatalog = append(upstreamSchemaOwnershipCatalog, upstreamSchemaOwnershipEntry{
		Version:                 2,
		MigrationVersion:        5,
		Kind:                    MigrationKindExpand,
		ImplementationID:        "upstream_schema_expand_v2",
		BeforeModelSchemaDigest: upstreamSchemaBaselineDigestV1,
		AfterModelSchemaDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	migrations := append(migrationSet(), migration{
		Version: 5, Kind: MigrationKindContract, UpstreamSchemaVersion: 2,
	})
	require.ErrorIs(t, validateUpstreamSchemaOwnership(migrations), ErrUnsafeMigration)

	migrations[len(migrations)-1].Kind = MigrationKindExpand
	upstreamSchemaOwnershipCatalog[1].BeforeModelSchemaDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	require.ErrorIs(t, validateUpstreamSchemaOwnership(migrations), ErrUnsafeMigration)
}

func TestMigrationContractChecksumBindsUpstreamBeforeAndAfterDigests(t *testing.T) {
	original := append([]upstreamSchemaOwnershipEntry(nil), upstreamSchemaOwnershipCatalog...)
	t.Cleanup(func() { upstreamSchemaOwnershipCatalog = original })
	entry := upstreamSchemaOwnershipEntry{
		Version:                 2,
		MigrationVersion:        5,
		Kind:                    MigrationKindExpand,
		ImplementationID:        "upstream_schema_expand_v2",
		BeforeModelSchemaDigest: upstreamSchemaBaselineDigestV1,
		AfterModelSchemaDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	upstreamSchemaOwnershipCatalog = append(upstreamSchemaOwnershipCatalog, entry)
	item := migration{Version: 5, Name: "upstream", Kind: MigrationKindExpand, UpstreamSchemaVersion: 2}
	baseline := migrationContractChecksum(item)

	upstreamSchemaOwnershipCatalog[1].AfterModelSchemaDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	require.NotEqual(t, baseline, migrationContractChecksum(item))

	upstreamSchemaOwnershipCatalog[1] = entry
	upstreamSchemaOwnershipCatalog[1].ImplementationID = "upstream_schema_expand_v2_revised"
	require.NotEqual(t, baseline, migrationContractChecksum(item))
}

func TestObserveUpstreamSchemaProducesReadOnlyAdoptionEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-adoption-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, model.BootstrapEmptyUpstreamSchema(context.Background(), db))

	adoption, err := ObserveUpstreamSchema(
		context.Background(),
		db,
		"0123456789abcdef0123456789abcdef01234567",
	)
	require.NoError(t, err)
	require.True(t, adoption.Ready)
	require.Equal(t, adoption.ModelSchemaDigest, adoption.BeforeModelSchemaDigest)
	require.Equal(t, adoption.ModelSchemaDigest, adoption.AfterModelSchemaDigest)
	require.Equal(t, "baseline", adoption.MigrationKind)
	require.Empty(t, adoption.MissingTables)
	require.Empty(t, adoption.MissingColumns)
	encoded, err := adoption.CanonicalJSON()
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
}
