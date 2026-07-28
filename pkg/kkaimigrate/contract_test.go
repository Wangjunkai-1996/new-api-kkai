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

func TestRuntimePlansStopBeforeMySQLOnlyMaintenanceMigration(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			plan, err := PlanForDialect(dialect)
			require.NoError(t, err)
			require.Len(t, plan, int(RequiredRuntimeVersion))
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
	require.NoError(t, validatePostgresOutboxEventKeyShape(columnTypes, JobLeaseSchemaVersion))

	err = validatePostgresOutboxEventKeyShape(columnTypes, OutboxEventKeySchemaVersion)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}
