package kkaimigrate

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageAsyncExpandPreservesV9AndIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, RC41ArchitectureSchemaVersion, ImageAsyncSchemaVersion)
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	_, err = ApplyImageAsyncExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasTable("kkai_image_reference_uploads"))
	_, err = ApplyImageAsyncExpand(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckImageAsyncSchema(ctx, db))
	var preserved []AppliedMigration
	require.NoError(t, db.Where("version <= ?", RC41ArchitectureSchemaVersion).Order("version").Find(&preserved).Error)
	assert.Equal(t, before, preserved)
	upload := model.KKAIImageReferenceUpload{AssetID: 1, OwnerUserID: 7, UploadKey: "one-intent", UploadID: "multipart-one", UploadPartSize: 5 * 1024 * 1024, UploadExpiresAt: 100, CleanupAt: 100, UploadManifest: "[]", CreatedAt: 1, UpdatedAt: 1}
	require.NoError(t, db.Create(&upload).Error)
	duplicate := upload
	duplicate.AssetID = 2
	require.Error(t, db.Create(&duplicate).Error)
	_, err = ApplyImageAsyncExpand(ctx, db, Options{})
	require.NoError(t, err)
	var restored model.KKAIImageReferenceUpload
	require.NoError(t, db.First(&restored, 1).Error)
	assert.Equal(t, upload, restored)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, CheckImageAsyncSchema(ctx, db))
}

func TestImageAsyncExpandRequiresV9AndCompletePhysicalSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, AuthenticationSchemaVersion, ImageAsyncSchemaVersion)
	require.NoError(t, err)
	_, err = ApplyImageAsyncExpand(ctx, db, Options{})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	assert.False(t, db.Migrator().HasTable("kkai_image_executions"))
	_, err = applyThroughVersion(ctx, db, Options{}, ImageAsyncSchemaVersion, ImageAsyncSchemaVersion)
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropIndex("kkai_image_reference_uploads", "ux_image_reference_upload_key"))
	require.ErrorIs(t, CheckImageAsyncSchema(ctx, db), ErrSchemaNotReady)
}
