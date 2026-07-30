package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoSampleCategoryV6AddsNullableColumnForEveryDialect(t *testing.T) {
	const expected = "ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32)"
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			statements := videoSampleCategorySchemaStatements[dialect]
			require.Equal(t, []migrationStatement{{
				Operation: migrationOperationAddNullableColumn,
				SQL:       expected,
			}}, statements)
			upper := strings.ToUpper(statements[0].SQL)
			require.NotContains(t, upper, "NOT NULL")
			require.NotContains(t, upper, "DEFAULT")
		})
	}
}

func TestApplyVideoSampleCategoryExpandUpgradesV5AndIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
	)
	require.NoError(t, err)
	require.False(t, db.Migrator().HasColumn("kkai_video_samples", "category"))
	require.NoError(t, checkThroughVersion(
		context.Background(), db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
	))

	result, err := ApplyVideoSampleCategoryExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(VideoSampleCategorySchemaVersion))
	require.True(t, db.Migrator().HasColumn("kkai_video_samples", "category"))
	require.NoError(t, Check(context.Background(), db, VideoSampleCategorySchemaVersion))

	result, err = ApplyVideoSampleCategoryExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, int(VideoSampleCategorySchemaVersion))
}

func TestApplyVideoSampleCategoryExpandResumesAfterColumnWasAdded(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
	)
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		"ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32)",
	).Error)

	_, err = ApplyVideoSampleCategoryExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, Check(context.Background(), db, VideoSampleCategorySchemaVersion))
}

func TestApplyVideoSampleCategoryExpandRejectsIncompatibleExistingColumn(t *testing.T) {
	tests := []struct {
		name string
		ddl  string
	}{
		{name: "wrong type", ddl: "ALTER TABLE kkai_video_samples ADD COLUMN category TEXT"},
		{name: "not nullable", ddl: "ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32) NOT NULL DEFAULT ''"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			_, err := applyThroughVersion(
				context.Background(), db, Options{}, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
			)
			require.NoError(t, err)
			require.NoError(t, db.Exec(test.ddl).Error)

			_, err = ApplyVideoSampleCategoryExpand(context.Background(), db, Options{})
			require.ErrorIs(t, err, ErrSchemaNotReady)
			var count int64
			require.NoError(t, db.Model(&AppliedMigration{}).Where(
				"version = ?", VideoSampleCategorySchemaVersion,
			).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestApplyVideoSampleCategoryExpandRequiresV5(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, OutboxEventKeySchemaVersion, VideoSampleCategorySchemaVersion,
	)
	require.NoError(t, err)

	_, err = ApplyVideoSampleCategoryExpand(context.Background(), db, Options{})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.False(t, db.Migrator().HasColumn("kkai_video_samples", "category"))
	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where(
		"version = ?", VideoSampleCategorySchemaVersion,
	).Count(&count).Error)
	require.Zero(t, count)
}

func TestVideoSampleCategoryRuntimeSchemaRequiresCategoryOnlyAtV6(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := applyThroughVersion(
		context.Background(), db, Options{}, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
	)
	require.NoError(t, err)
	require.NoError(t, checkThroughVersion(
		context.Background(), db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, VideoSampleCategorySchemaVersion,
	))

	v6 := migrationSet()[VideoSampleCategorySchemaVersion-1]
	require.NoError(t, db.Create(&AppliedMigration{
		Version: v6.Version, Name: v6.Name, Checksum: storedMigrationChecksum(v6),
	}).Error)
	err = Check(context.Background(), db, VideoSampleCategorySchemaVersion)
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.ErrorContains(t, err, "kkai_video_samples.category")
}

func TestVideoStudioV5ChecksumRemainsImmutableAfterV6(t *testing.T) {
	v5 := migrationSet()[VideoStudioSchemaVersion-1]
	require.Equal(t, "ca0fcda4889bcaa6d0d0dbf37fef1f61402bb3a04a89e14e86bf76cec32287d4", storedMigrationChecksum(v5))
}
