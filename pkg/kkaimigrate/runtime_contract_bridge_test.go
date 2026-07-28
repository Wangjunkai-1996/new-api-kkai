//go:build kkai_bridge

package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesBridgeRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			require.EqualValues(t, 3, contract.RuntimeMinVersion)
			require.EqualValues(t, 5, contract.RuntimeMaxVersion)
			require.EqualValues(t, 3, contract.MigrationTargetVersion)
			require.Equal(t, MigrationKindNone, contract.MigrationKind)
			require.Len(t, contract.CompatiblePrefixes, 3)
			for _, version := range []string{"3", "4", "5"} {
				require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.CompatiblePrefixes[version])
			}
			require.Equal(t, contract.CompatiblePrefixes["3"], contract.MigrationSetDigest)
		})
	}
}

func TestBridgeContractRequiresCanonicalPostgresV4Shape(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("ALTER TABLE kkai_outbox RENAME TO kkai_outbox_old").Error)
	createOutbox := strings.Replace(
		riskSchemaStatements[DialectSQLite][1],
		"event_key VARCHAR(192) NOT NULL UNIQUE",
		"event_key VARCHAR(191) NOT NULL UNIQUE",
		1,
	)
	require.NoError(t, db.Exec(createOutbox).Error)
	require.NoError(t, db.Exec("DROP TABLE kkai_outbox_old").Error)
	v4 := migrationSet()[3]
	require.NoError(t, db.Create(&AppliedMigration{
		Version: v4.Version, Name: v4.Name, Checksum: storedMigrationChecksum(v4),
	}).Error)
	columnTypes, err := db.Migrator().ColumnTypes("kkai_outbox")
	require.NoError(t, err)
	require.NoError(t, validatePostgresOutboxEventKeyShape(columnTypes, OutboxEventKeySchemaVersion))

	contract, err := ContractForDialect(DialectPostgres)
	require.NoError(t, err)
	compatiblePrefix, ok := storedPrefixItemsForDialect(DialectPostgres, OutboxEventKeySchemaVersion)
	require.True(t, ok)
	compatibleDigest := migrationSetDigest(DialectPostgres, compatiblePrefix)
	require.Equal(t, contract.CompatiblePrefixes["4"], compatibleDigest)
	require.NotEqual(t, contract.MigrationSetDigest, compatibleDigest)
}
