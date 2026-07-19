package kkaimigrate

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type namedTestDialector struct {
	gorm.Dialector
	name string
}

func (dialector namedTestDialector) Name() string { return dialector.name }

func TestContractForDialectUsesPostgresV3Runtime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			require.EqualValues(t, 3, contract.RuntimeMinVersion)
			require.EqualValues(t, 4, contract.RuntimeMaxVersion)
			require.EqualValues(t, 3, contract.MigrationTargetVersion)
			require.Equal(t, MigrationKindNone, contract.MigrationKind)
			require.Len(t, contract.CompatiblePrefixes, 2)
			require.Equal(t, contract.MigrationSetDigest, contract.CompatiblePrefixes[strconv.FormatInt(contract.MigrationTargetVersion, 10)])
			require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.CompatiblePrefixes["4"])
		})
	}
}

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

func TestObserveAcceptsPostgresV3AndRejectsMismatchedV4Shape(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	db.Dialector = namedTestDialector{Dialector: db.Dialector, name: DialectPostgres}

	observation, err := Observe(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, JobLeaseSchemaVersion, observation.CurrentVersion)

	v4 := migrationSet()[3]
	require.NoError(t, db.Create(&AppliedMigration{
		Version: v4.Version, Name: v4.Name, Checksum: storedMigrationChecksum(v4),
	}).Error)
	_, err = Observe(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}

func TestPostgresCompatibleV4PrefixRequiresCanonicalPhysicalShape(t *testing.T) {
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
	db.Dialector = namedTestDialector{Dialector: db.Dialector, name: DialectPostgres}

	observation, err := Observe(context.Background(), db)
	require.NoError(t, err)
	contract, err := ContractForDialect(DialectPostgres)
	require.NoError(t, err)
	require.Equal(t, contract.CompatiblePrefixes["4"], observation.MigrationSetDigest)
	require.NotEqual(t, contract.MigrationSetDigest, observation.MigrationSetDigest)
}
