package kkaimigrate

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoMediaMetadataExpansionRequiresExplicitReceipt(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectSQLite)

	status, err := PlanVideoMediaMetadataExpansion(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "pristine", status.State)
	require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT").Error)
	_, err = PlanVideoMediaMetadataExpansion(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}

func TestApplyVideoMediaMetadataExpansionSQLiteIsAuthorizedAndIdempotent(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectSQLite)
	var ledgerBefore int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&ledgerBefore).Error)

	_, err := ApplyVideoMediaMetadataExpand(context.Background(), db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	status, err := ApplyVideoMediaMetadataExpand(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, "pristine", status.State)
	require.False(t, db.Migrator().HasColumn("kkai_video_assets", "media_metadata"))

	status, err = ApplyVideoMediaMetadataExpand(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.Equal(t, "ready", status.State)
	require.NoError(t, CheckVideoMediaMetadataExpansion(context.Background(), db))

	status, err = ApplyVideoMediaMetadataExpand(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.Equal(t, "ready", status.State)
	var ledgerAfter int64
	require.NoError(t, db.Model(&AppliedMigration{}).Count(&ledgerAfter).Error)
	require.Equal(t, ledgerBefore, ledgerAfter)
}

func TestVideoMediaMetadataExpansionResumesAfterReceiptTableCreation(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectSQLite)
	require.NoError(t, db.Exec(videoMediaMetadataReceiptStatements[DialectSQLite]).Error)

	status, err := PlanVideoMediaMetadataExpansion(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "receipt_missing", status.State)
	require.True(t, status.Safe)
	require.False(t, status.ColumnPresent)

	status, err = ApplyVideoMediaMetadataExpand(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.Equal(t, "ready", status.State)
	require.NoError(t, CheckVideoMediaMetadataExpansion(context.Background(), db))
}

func TestVideoMediaMetadataExpansionNeverAdoptsAnUnreceiptedColumn(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectSQLite)
	require.NoError(t, db.Exec(videoMediaMetadataReceiptStatements[DialectSQLite]).Error)
	require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT").Error)

	_, err := PlanVideoMediaMetadataExpansion(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
	_, err = ApplyVideoMediaMetadataExpand(context.Background(), db, Options{Offline: true})
	require.ErrorIs(t, err, ErrSchemaNotReady)
}

func TestVideoMediaMetadataExpansionDatabaseCompatibility(t *testing.T) {
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			db := newVideoMediaMetadataExpansionDB(t, dialect)
			_, err := ApplyVideoMediaMetadataExpand(context.Background(), db, Options{Offline: true})
			require.NoError(t, err)
			require.NoError(t, CheckVideoMediaMetadataExpansion(context.Background(), db))
		})
	}
}

func TestVideoMediaMetadataPostgresPreservesOldWritesAndNeverQueuesTraffic(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectPostgres)
	ctx := context.Background()
	_, err := ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	require.NoError(t, db.Table("kkai_video_assets").Create(map[string]any{
		"id": 9001, "scope": "user", "kind": "reference", "state": "ready",
		"object_key": "local-media-expansion/old.png", "archive_source_url": "",
		"mime_type": "image/png", "created_at": 1, "updated_at": 1,
	}).Error)
	holder := db.Begin()
	require.NoError(t, holder.Error)
	t.Cleanup(func() { _ = holder.Rollback().Error })
	require.NoError(t, holder.Exec("LOCK TABLE kkai_video_assets IN ACCESS SHARE MODE").Error)
	_, err = ApplyVideoMediaMetadataExpand(ctx, db, Options{Offline: true})
	var lockErr *pgconn.PgError
	require.ErrorAs(t, err, &lockErr)
	assert.Equal(t, "55P03", lockErr.Code, "NOWAIT rejects contention without queuing an exclusive lock")
	require.NoError(t, holder.Rollback().Error)
	assert.False(t, db.Migrator().HasColumn("kkai_video_assets", "media_metadata"))
	_, err = ApplyVideoMediaMetadataExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))
	var metadata *string
	require.NoError(t, db.Table("kkai_video_assets").Select("media_metadata").Where("id = ?", 9001).Scan(&metadata).Error)
	assert.Nil(t, metadata, "existing assets do not acquire fabricated inspection evidence")
	require.NoError(t, db.Table("kkai_video_assets").Where("id = ?", 9001).Update("media_metadata", `{"version":1}`).Error)
	require.NoError(t, db.Table("kkai_video_assets").Where("id = ?", 9001).Update("updated_at", 2).Error)
	require.NoError(t, db.Table("kkai_video_assets").Select("media_metadata").Where("id = ?", 9001).Scan(&metadata).Error)
	require.NotNil(t, metadata)
	assert.Equal(t, `{"version":1}`, *metadata, "old-column updates preserve new inspection evidence")
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	assert.Equal(t, before, after)
}

func TestVideoMediaMetadataPostgresRevalidatesSourceUnderLock(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectPostgres)
	ctx := t.Context()
	status, err := PlanVideoMediaMetadataExpansion(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "pristine", status.State)
	var original AppliedMigration
	require.NoError(t, db.First(&original, "version = ?", MembershipBillingRecoverySchemaVersion).Error)
	checksum := original.Checksum
	require.NoError(t, db.Model(&original).Where("version = ?", original.Version).Update("checksum", "invalid").Error)
	authorized := false
	options := Options{AuthorizeMutation: func(ctx context.Context, locked *gorm.DB) error {
		authorized = true
		var holdsLock bool
		require.NoError(t, locked.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND granted)").Scan(&holdsLock).Error)
		require.True(t, holdsLock)
		return nil
	}}
	_, err = ApplyVideoMediaMetadataExpand(ctx, db, options)
	require.ErrorIs(t, err, ErrChecksumMismatch)
	assert.False(t, authorized, "canonical drift after preflight must fail before proof authorization or mutation")
	assert.False(t, db.Migrator().HasTable(&videoMediaMetadataReceipt{}))
	require.NoError(t, db.Model(&original).Where("version = ?", original.Version).Update("checksum", checksum).Error)

	require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets ADD COLUMN media_metadata TEXT").Error)
	_, err = ApplyVideoMediaMetadataExpand(ctx, db, options)
	require.ErrorIs(t, err, ErrSchemaNotReady)
	assert.False(t, authorized, "physical drift after preflight must fail before proof authorization or receipt creation")
	assert.False(t, db.Migrator().HasTable(&videoMediaMetadataReceipt{}))
	require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets DROP COLUMN media_metadata").Error)

	status, err = ApplyVideoMediaMetadataExpand(ctx, db, options)
	require.NoError(t, err)
	assert.True(t, authorized)
	assert.Equal(t, "ready", status.State)
}

func TestVideoMediaMetadataObservationRequiresVerifiedReceiptAndPhysicalColumn(t *testing.T) {
	db := newVideoMediaMetadataExpansionDB(t, DialectSQLite)
	ctx := context.Background()
	before, err := Observe(ctx, db)
	require.NoError(t, err)
	assert.NotContains(t, before.Expansions, VideoMediaMetadataExpansion)
	_, err = ApplyVideoMediaMetadataExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	after, err := Observe(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, before.CurrentVersion, after.CurrentVersion)
	assert.Equal(t, before.MigrationSetDigest, after.MigrationSetDigest)
	assert.Equal(t, VideoMediaMetadataExpansionDigest(), after.Expansions[VideoMediaMetadataExpansion])
	require.NoError(t, db.Model(&videoMediaMetadataReceipt{}).Where("step = ?", "column").Update("plan_checksum", "tampered").Error)
	_, err = Observe(ctx, db)
	assert.ErrorIs(t, err, ErrChecksumMismatch)
	require.NoError(t, db.Model(&videoMediaMetadataReceipt{}).Where("step = ?", "column").Update("plan_checksum", VideoMediaMetadataPlanChecksum()).Error)
	require.NoError(t, db.Exec("ALTER TABLE kkai_video_assets DROP COLUMN media_metadata").Error)
	_, err = Observe(ctx, db)
	assert.ErrorIs(t, err, ErrSchemaNotReady)
}

func newVideoMediaMetadataExpansionDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	if dialect == DialectSQLite {
		db = newMigrationTestDB(t)
	} else {
		db = newImageAsyncDatabaseFixture(t, dialect)
	}
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	columnType := "BIGINT"
	if dialect == DialectSQLite {
		columnType = "INTEGER"
	}
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id " + columnType + " NULL", "ALTER TABLE top_ups ADD COLUMN create_time " + columnType + " NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	return db
}
