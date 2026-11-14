package kkaimigrate

import (
	"context"
	"testing"

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
