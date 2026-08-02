package service

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageSampleLifecycleUsesValidatedCatalogAsset(t *testing.T) {
	db := newImageLibraryTestDB(t)
	profile := createImageSampleProfile(t, db)
	store := &imagePipelineStore{objects: map[string][]byte{}}
	fetcher := NewHTTPImageArchiveFetcher(t.TempDir())
	fetcher.availableBytes = func(string) (uint64, error) { return math.MaxUint64, nil }
	pngBody := imageArchiveTestPNG(t, 4, 3)

	asset, err := CreateImageCatalogAsset(
		context.Background(), db, store, fetcher, 99, "sample.png", "image/png", bytes.NewReader(pngBody),
	)
	require.NoError(t, err)
	assert.Equal(t, model.ImageAssetStateReady, asset.State)
	require.Len(t, store.objects, 1)
	for _, stored := range store.objects {
		assert.Equal(t, pngBody, stored)
	}

	sample, err := CreateImageSample(context.Background(), db, ImageSampleInput{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "Lighthouse",
		Prompt: "A lighthouse at dusk", Parameters: map[string]any{"count": 1},
		Category: "architecture", Status: model.ImageSampleStatusPublished,
	})
	require.NoError(t, err)
	assert.Equal(t, profile.Model, sample.Model)
	assert.Equal(t, 1, sample.ModelVersion)
	assert.Equal(t, asset.ID, sample.Asset.ID)

	page, err := ListImageSamples(
		context.Background(), db, "", "architecture", "", 10, false, []string{profile.Model},
	)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, sample.ID, page.Items[0].ID)

	_, err = UpdateImageSample(context.Background(), db, sample.ID, ImageSampleInput{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID + 1, Title: sample.Title,
		Prompt: sample.Prompt, Parameters: sample.Parameters, Category: sample.Category, Status: sample.Status,
	})
	require.ErrorIs(t, err, ErrImageSampleImmutable)
}

func TestPublishingImageSampleRequiresEnabledProfile(t *testing.T) {
	db := newImageLibraryTestDB(t)
	profile := createImageSampleProfile(t, db)
	require.NoError(t, db.Model(&model.KKAIImageModelProfile{}).Where("id = ?", profile.ID).Update("enabled", false).Error)
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
		State: model.ImageAssetStateReady, ObjectKey: "catalog/disabled-profile",
		ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
		SizeBytes: 10, Width: 1, Height: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)

	_, err := CreateImageSample(context.Background(), db, ImageSampleInput{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "Sample", Prompt: "Prompt",
		Parameters: map[string]any{"count": 1}, Category: "general", Status: model.ImageSampleStatusPublished,
	})
	require.ErrorIs(t, err, ErrImageSampleNotPublishable)
}

func TestReconcileOrphanedImageCatalogAssetsDeletesOnlyOldUnreferencedUploads(t *testing.T) {
	db := newImageLibraryTestDB(t)
	profile := createImageSampleProfile(t, db)
	now := time.Now()
	assets := []model.KKAIImageAsset{
		{
			OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
			State: model.ImageAssetStateReady, ObjectKey: "catalog/orphan-old",
			ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
			CreatedAt: now.Add(-48 * time.Hour).Unix(), UpdatedAt: now.Add(-48 * time.Hour).Unix(),
		},
		{
			OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
			State: model.ImageAssetStateReady, ObjectKey: "catalog/referenced-old",
			ThumbnailState: model.ImageThumbnailStateReady, ThumbnailObjectKey: "catalog/referenced-old.thumbnail.jpg",
			MIMEType: "image/png", CreatedAt: now.Add(-48 * time.Hour).Unix(), UpdatedAt: now.Add(-48 * time.Hour).Unix(),
		},
		{
			OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
			State: model.ImageAssetStateReady, ObjectKey: "catalog/recent-unbound",
			ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
			CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
		},
	}
	require.NoError(t, db.Create(&assets).Error)
	require.NoError(t, db.Create(&model.KKAIImageSample{
		ModelProfileID: profile.ID, ImageAssetID: assets[1].ID, Title: "Referenced", Prompt: "Prompt",
		ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general",
		Status: model.ImageSampleStatusDraft, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}).Error)

	removed, err := ReconcileOrphanedImageCatalogAssets(
		context.Background(), db, now.Add(-ImageCatalogOrphanTTL), 100,
	)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	for index := range assets {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
	}
	require.Equal(t, model.ImageAssetStateDeleted, assets[0].State)
	require.NotZero(t, assets[0].DeletedAt)
	require.Equal(t, model.ImageAssetStateReady, assets[1].State)
	require.Zero(t, assets[1].DeletedAt)
	require.Equal(t, model.ImageAssetStateReady, assets[2].State)
	require.Zero(t, assets[2].DeletedAt)
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("aggregate_id = ?", assets[0].ID).First(&event).Error)
	require.Equal(t, ImageAssetDeleteTopic, event.Topic)
}

func TestListImageSamplesUsesStableCompoundCursor(t *testing.T) {
	db := newImageLibraryTestDB(t)
	profile := createImageSampleProfile(t, db)
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
		State: model.ImageAssetStateReady, ObjectKey: "catalog/paginated-samples",
		ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
		SizeBytes: 10, Width: 1, Height: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	samples := []model.KKAIImageSample{
		{ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "first", Prompt: "first", ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general", Status: model.ImageSampleStatusPublished, SortOrder: 0, CreatedAt: now, UpdatedAt: now},
		{ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "second", Prompt: "second", ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general", Status: model.ImageSampleStatusPublished, SortOrder: 0, CreatedAt: now, UpdatedAt: now},
		{ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "third", Prompt: "third", ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general", Status: model.ImageSampleStatusPublished, SortOrder: 10, CreatedAt: now, UpdatedAt: now},
		{ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "fourth", Prompt: "fourth", ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general", Status: model.ImageSampleStatusPublished, SortOrder: 10, CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&samples).Error)

	firstPage, err := ListImageSamples(context.Background(), db, "", "", "", 2, false, []string{profile.Model})
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	assert.Equal(t, []int64{samples[1].ID, samples[0].ID}, []int64{firstPage.Items[0].ID, firstPage.Items[1].ID})
	assert.Equal(t, formatImageSampleCursor(samples[0]), firstPage.NextCursor)

	secondPage, err := ListImageSamples(context.Background(), db, "", "", firstPage.NextCursor, 2, false, []string{profile.Model})
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 2)
	assert.Equal(t, []int64{samples[3].ID, samples[2].ID}, []int64{secondPage.Items[0].ID, secondPage.Items[1].ID})
	assert.Empty(t, secondPage.NextCursor)
}

func TestUserImageSamplesHideStaleSpecificationVersions(t *testing.T) {
	db := newImageLibraryTestDB(t)
	profile := createImageSampleProfile(t, db)
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: 99, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
		State: model.ImageAssetStateReady, ObjectKey: "catalog/stale-sample",
		ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
		SizeBytes: 10, Width: 1, Height: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	sample := model.KKAIImageSample{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: "stale", Prompt: "stale",
		ModelVersion: 1, Parameters: `{"count":1,"size":"1024x1024"}`, Category: "general",
		Status: model.ImageSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&sample).Error)
	require.NoError(t, db.Model(&model.KKAIImageModelProfile{}).Where("id = ?", profile.ID).
		Updates(map[string]any{"specification_version": 2, "specification": strings.Replace(profile.Specification, `"version":1`, `"version":2`, 1)}).Error)

	page, err := ListImageSamples(context.Background(), db, "", "", "", 10, false, []string{profile.Model})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	_, err = GetImageSample(context.Background(), db, sample.ID, false, []string{profile.Model})
	assert.ErrorIs(t, err, ErrImageSampleNotFound)
	_, err = GetAuthorizedImageAsset(context.Background(), db, 7, false, asset.ID)
	assert.ErrorIs(t, err, ErrImageAssetAccessDenied)

	adminSample, err := GetImageSample(context.Background(), db, sample.ID, true, nil)
	require.NoError(t, err)
	assert.Equal(t, sample.ID, adminSample.ID)
}

func createImageSampleProfile(t *testing.T, db *gorm.DB) model.KKAIImageModelProfile {
	t.Helper()
	specification, err := common.Marshal(imageSubmissionSpec())
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIImageModelProfile{
		Model: "gpt-image-1", DisplayName: "Image", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: `{"count":1,"size":"1024x1024"}`,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	return profile
}
