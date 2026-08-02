package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageStudioV7DefinesOnlyAdditiveObjectsForEveryDialect(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			statements := imageStudioSchemaStatements[dialect]
			require.Len(t, statements, 16)
			reconcileIndexFound := false
			billingIndexFound := false
			for _, statement := range statements {
				require.Contains(t, []string{migrationOperationCreateTable, migrationOperationCreateIndex}, statement.Operation)
				upper := strings.ToUpper(statement.SQL)
				require.NotContains(t, upper, "DROP ")
				require.NotContains(t, upper, "ALTER ")
				if strings.Contains(statement.SQL, "idx_kkai_image_generations_reconcile") {
					reconcileIndexFound = true
					require.Contains(t, statement.SQL, "(status, heartbeat_at, id)")
				}
				if strings.Contains(statement.SQL, "idx_kkai_image_generations_billing") {
					billingIndexFound = true
					require.Contains(t, statement.SQL, "(billing_state, id)")
				}
			}
			require.True(t, reconcileIndexFound)
			require.True(t, billingIndexFound)
		})
	}
}

func TestApplyImageStudioExpandUpgradesV6AndIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, VideoSampleCategorySchemaVersion, ImageStudioSchemaVersion,
	)
	require.NoError(t, err)
	require.False(t, db.Migrator().HasTable("kkai_image_model_profiles"))

	result, err := ApplyImageStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(ImageStudioSchemaVersion))
	for _, table := range []string{
		"kkai_image_model_profiles",
		"kkai_image_samples",
		"kkai_image_generations",
		"kkai_image_assets",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	require.NoError(t, Check(context.Background(), db, ImageStudioSchemaVersion))

	result, err = ApplyImageStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(ImageStudioSchemaVersion))
}

func TestApplyImageStudioExpandRequiresV6(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, VideoStudioSchemaVersion, ImageStudioSchemaVersion,
	)
	require.NoError(t, err)

	_, err = ApplyImageStudioExpand(context.Background(), db, Options{})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.False(t, db.Migrator().HasTable("kkai_image_model_profiles"))
}

func TestImageStudioRuntimeSchemaRejectsMissingTable(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, ImageStudioSchemaVersion, ImageStudioSchemaVersion,
	)
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropTable("kkai_image_assets"))

	err = Check(context.Background(), db, ImageStudioSchemaVersion)
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.ErrorContains(t, err, "kkai_image_assets")
}
