package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListImageGenerationsIsUserScopedAndReturnsMediaURLs(t *testing.T) {
	db := newImageLibraryTestDB(t)
	first := createImageLibraryGeneration(t, db, 7, "first")
	second := createImageLibraryGeneration(t, db, 7, "second")
	_ = createImageLibraryGeneration(t, db, 8, "other-user")
	asset := model.KKAIImageAsset{
		GenerationID: &second.ID, OwnerUserID: 7, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		Position: 0, ObjectKey: "image/original", ThumbnailObjectKey: "image/thumbnail",
		ThumbnailState: model.ImageThumbnailStateReady, MIMEType: "image/png",
		SizeBytes: 100, Width: 10, Height: 10, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)

	page, err := ListImageGenerations(context.Background(), db, 7, ImageGenerationFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, second.ID, page.Items[0].ID)
	assert.Equal(t, fmt.Sprintf("%d", second.ID), page.NextCursor)
	require.Len(t, page.Items[0].Assets, 1)
	assert.Equal(t, "/api/image-studio/assets/1/content", page.Items[0].Assets[0].ContentURL)
	assert.Equal(t, "/api/image-studio/assets/1/content?variant=thumbnail", page.Items[0].Assets[0].ThumbnailURL)

	next, err := ListImageGenerations(context.Background(), db, 7, ImageGenerationFilter{Limit: 1, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	assert.Equal(t, first.ID, next.Items[0].ID)
}

func TestDeleteImageGenerationSoftDeletesAndEnqueuesPhysicalDeletion(t *testing.T) {
	db := newImageLibraryTestDB(t)
	generation := createImageLibraryGeneration(t, db, 7, "delete")
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 7, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image/delete-original", ThumbnailObjectKey: "image/delete-thumbnail",
		ThumbnailState: model.ImageThumbnailStateReady, MIMEType: "image/png",
		SizeBytes: 10, Width: 1, Height: 1, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)

	require.NoError(t, DeleteImageGeneration(context.Background(), db, 7, generation.ID))
	require.ErrorIs(t, func() error {
		_, err := GetOwnedImageGeneration(context.Background(), db, 7, generation.ID)
		return err
	}(), ErrImageGenerationNotFound)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	assert.Equal(t, model.ImageAssetStateDeleted, asset.State)
	assert.NotZero(t, asset.DeletedAt)
	var event model.KKAIOutboxEvent
	require.NoError(t, db.First(&event).Error)
	assert.Equal(t, ImageAssetDeleteTopic, event.Topic)
	assert.Contains(t, event.Payload, "image/delete-original")
}

func TestImageCatalogAssetRequiresPublishedEnabledSample(t *testing.T) {
	db := newImageLibraryTestDB(t)
	now := time.Now().Unix()
	profile := model.KKAIImageModelProfile{
		Model: "gpt-image-1", DisplayName: "Image", SpecificationVersion: 1,
		Specification: `{"version":1,"parameters":[]}`, DefaultParameters: `{}`,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	asset := model.KKAIImageAsset{
		OwnerUserID: 1, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
		State: model.ImageAssetStateReady, ObjectKey: "catalog/image", ThumbnailState: model.ImageThumbnailStatePending,
		MIMEType: "image/png", SizeBytes: 10, Width: 1, Height: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)

	_, err := GetAuthorizedImageAsset(context.Background(), db, 7, false, asset.ID)
	require.ErrorIs(t, err, ErrImageAssetAccessDenied)
	require.NoError(t, db.Create(&model.KKAIImageSample{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "Sample", Prompt: "Prompt",
		ModelVersion: 1, Parameters: `{}`, Category: "general", Status: model.ImageSampleStatusPublished,
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	authorized, err := GetAuthorizedImageAsset(context.Background(), db, 7, false, asset.ID)
	require.NoError(t, err)
	assert.Equal(t, asset.ID, authorized.ID)
}

func newImageLibraryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:image-library-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIOutboxEvent{},
	))
	return db
}

func createImageLibraryGeneration(t *testing.T, db *gorm.DB, userID int, prompt string) model.KKAIImageGeneration {
	t.Helper()
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: userID, TokenID: 1, ModelProfileID: 1, SpecificationVersion: 1,
		Model: "gpt-image-1", Prompt: prompt, Parameters: `{}`,
		RequestHash: fmt.Sprintf("%064d", time.Now().UnixNano()),
		RequestID:   fmt.Sprintf("request-%d", time.Now().UnixNano()), Status: model.ImageGenerationStatusSucceeded,
		RequestedCount: 1, SucceededCount: 1, BillingState: model.ImageGenerationBillingStateSettled,
		StartedAt: now, FinishedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return generation
}
