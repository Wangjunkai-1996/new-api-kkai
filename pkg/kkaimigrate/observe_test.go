package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserveIsReadOnlyOnValidRuntimeSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)

	var beforeLedgerRows int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&beforeLedgerRows).Error)
	var beforeOutboxRows int64
	require.NoError(t, db.Table("kkai_outbox").Count(&beforeOutboxRows).Error)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)

	observation, err := Observe(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, RequiredRuntimeVersion, observation.CurrentVersion)

	var afterLedgerRows int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&afterLedgerRows).Error)
	var afterOutboxRows int64
	require.NoError(t, db.Table("kkai_outbox").Count(&afterOutboxRows).Error)
	require.Equal(t, beforeLedgerRows, afterLedgerRows)
	require.Equal(t, beforeOutboxRows, afterOutboxRows)
}

func TestObserveRejectsMissingRuntimeTable(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropTable("kkai_job_leases"))

	_, err = Observe(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}

func TestObserveRejectsMissingRuntimeColumn(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropTable("kkai_outbox"))
	require.NoError(t, db.Exec("CREATE TABLE kkai_outbox (id INTEGER PRIMARY KEY)").Error)

	_, err = Observe(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}
