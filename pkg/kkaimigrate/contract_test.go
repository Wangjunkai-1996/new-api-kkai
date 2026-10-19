package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractRejectsUnsupportedDialect(t *testing.T) {
	_, err := ContractForDialect("oracle")
	require.ErrorIs(t, err, ErrUnsupportedDialect)
}

func TestRuntimePlansUseReviewedTenantCompatibilityPath(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			plan, err := PlanForDialect(dialect)
			require.NoError(t, err)
			expectedLength := int(MigrationTargetVersion)
			if MigrationTargetVersion == TenantOnlineSchemaVersion {
				expectedLength -= 2 // Historical v12/v13 are separate paths.
			}
			require.Len(t, plan, expectedLength)
			for _, item := range plan {
				require.NotEqual(t, TenantSchemaVersion, item.Version)
				require.NotEqual(t, TenantCompatibilitySchemaVersion, item.Version)
			}
			require.Equal(t, RequiredRuntimeVersion, plan[len(plan)-1].Version)
		})
	}
}

func TestPostgresV3OutboxShapeRejectsMismatchedV4Version(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(context.Background(), db, Options{}, JobLeaseSchemaVersion, MaxCompatibleVersion)
	require.NoError(t, err)
	columnTypes, err := db.Migrator().ColumnTypes("kkai_outbox")
	require.NoError(t, err)
	require.NoError(t, validateOutboxEventKeyShape(columnTypes, JobLeaseSchemaVersion))

	err = validateOutboxEventKeyShape(columnTypes, OutboxEventKeySchemaVersion)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}
