package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedVideoSampleCategoryTestCatalog(t *testing.T) (*gorm.DB, []model.KKAIVideoModelProfile, model.KKAIVideoAsset) {
	t.Helper()
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	specification, err := common.Marshal(VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo},
	})
	require.NoError(t, err)
	profiles := []model.KKAIVideoModelProfile{
		{
			Model: "category-model-a", DisplayName: "Category A", SpecificationVersion: 1,
			Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			Model: "category-model-b", DisplayName: "Category B", SpecificationVersion: 1,
			Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range profiles {
		require.NoError(t, db.Create(&profiles[index]).Error)
	}
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
		State: model.VideoAssetStateReady, ObjectKey: "category-sample.mp4", MIMEType: "video/mp4",
		Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	return db, profiles, asset
}

func TestVideoSamplesRespectEffectiveModelFilter(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	specification, err := common.Marshal(VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo},
	})
	require.NoError(t, err)

	profiles := []model.KKAIVideoModelProfile{
		{
			Model: "allowed-model", DisplayName: "Allowed", SpecificationVersion: 1,
			Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			Model: "blocked-model", DisplayName: "Blocked", SpecificationVersion: 1,
			Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range profiles {
		require.NoError(t, db.Create(&profiles[index]).Error)
	}

	assets := []model.KKAIVideoAsset{
		{
			Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
			State: model.VideoAssetStateReady, ObjectKey: "allowed.mp4", MIMEType: "video/mp4",
			CreatedAt: now, UpdatedAt: now,
		},
		{
			Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
			State: model.VideoAssetStateReady, ObjectKey: "blocked.mp4", MIMEType: "video/mp4",
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}

	samples := []model.KKAIVideoSample{
		{
			ModelProfileID: profiles[0].ID, Title: "Allowed", Prompt: "allowed",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: assets[0].ID, AspectRatio: 1, Status: model.VideoSampleStatusPublished,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ModelProfileID: profiles[1].ID, Title: "Blocked", Prompt: "blocked",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: assets[1].ID, AspectRatio: 1, Status: model.VideoSampleStatusPublished,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range samples {
		require.NoError(t, db.Create(&samples[index]).Error)
	}

	page, err := ListVideoSamples(context.Background(), db, "", "", "", 24, false, []string{"allowed-model"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "allowed-model", page.Items[0].Model)

	_, err = GetVideoSample(context.Background(), db, samples[1].ID, false, []string{"allowed-model"})
	require.ErrorIs(t, err, ErrVideoSampleNotFound)

	empty, err := ListVideoSamples(context.Background(), db, "", "", "", 24, false, []string{})
	require.NoError(t, err)
	require.Empty(t, empty.Items)
}

func TestVideoSamplesCombineModelAndCategoryFilters(t *testing.T) {
	if !videoSampleCategoryFeatureEnabled {
		t.Skip("category filtering is enabled only in the v6 feature build")
	}
	db, profiles, asset := seedVideoSampleCategoryTestCatalog(t)
	now := time.Now().Unix()
	samples := []model.KKAIVideoSample{
		{
			ModelProfileID: profiles[0].ID, Title: "A people", Prompt: "a people",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: model.VideoSampleCategoryPeople,
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
		{
			ModelProfileID: profiles[0].ID, Title: "A animals", Prompt: "a animals",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: model.VideoSampleCategoryAnimals,
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
		{
			ModelProfileID: profiles[1].ID, Title: "B people", Prompt: "b people",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: model.VideoSampleCategoryPeople,
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range samples {
		require.NoError(t, db.Create(&samples[index]).Error)
	}

	page, err := ListVideoSamples(
		context.Background(), db, profiles[0].Model, model.VideoSampleCategoryPeople, "", 24, false,
		[]string{profiles[0].Model, profiles[1].Model},
	)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "A people", page.Items[0].Title)
	require.Equal(t, model.VideoSampleCategoryPeople, page.Items[0].Category)

	empty, err := ListVideoSamples(
		context.Background(), db, profiles[0].Model, model.VideoSampleCategoryEffects, "", 24, false, nil,
	)
	require.NoError(t, err)
	require.Empty(t, empty.Items)
}

func TestVideoSampleOtherCategoryIncludesLegacyMissingValues(t *testing.T) {
	if !videoSampleCategoryFeatureEnabled {
		t.Skip("category projection is enabled only in the v6 feature build")
	}
	db, profiles, asset := seedVideoSampleCategoryTestCatalog(t)
	now := time.Now().Unix()
	samples := []model.KKAIVideoSample{
		{
			ModelProfileID: profiles[0].ID, Title: "Explicit other", Prompt: "other",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: model.VideoSampleCategoryOther,
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
		{
			ModelProfileID: profiles[0].ID, Title: "Legacy empty", Prompt: "empty",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: "",
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
		{
			ModelProfileID: profiles[0].ID, Title: "Legacy null", Prompt: "null",
			Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
			VideoAssetID: asset.ID, AspectRatio: 1, Category: model.VideoSampleCategoryOther,
			Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
		},
	}
	for index := range samples {
		require.NoError(t, db.Create(&samples[index]).Error)
	}
	require.NoError(t, db.Model(&samples[2]).UpdateColumn("category", nil).Error)

	page, err := ListVideoSamples(
		context.Background(), db, "", model.VideoSampleCategoryOther, "", 24, false, nil,
	)
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	for _, item := range page.Items {
		require.Equal(t, model.VideoSampleCategoryOther, item.Category)
	}
}

func TestVideoSampleCreateUpdateAndValidationPersistCategory(t *testing.T) {
	if !videoSampleCategoryFeatureEnabled {
		t.Skip("category persistence is enabled only in the v6 feature build")
	}
	db, profiles, asset := seedVideoSampleCategoryTestCatalog(t)
	input := VideoSampleInput{
		ModelProfileID: profiles[0].ID, Title: "Category", Prompt: "prompt", Mode: VideoModeTextToVideo,
		Parameters: map[string]any{}, ReferenceAssetIDs: []int64{}, VideoAssetID: asset.ID,
		AspectRatio: 16.0 / 9.0, Status: model.VideoSampleStatusDraft,
	}

	created, err := CreateVideoSample(context.Background(), db, 7, input)
	require.NoError(t, err)
	require.Equal(t, model.VideoSampleCategoryOther, created.Category)
	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.Equal(t, model.VideoSampleCategoryOther, persisted.Category)

	require.NoError(t, db.Model(&persisted).UpdateColumn("category", nil).Error)
	input.Category = model.VideoSampleCategoryArchitecture
	updated, err := UpdateVideoSample(context.Background(), db, created.ID, 7, input)
	require.NoError(t, err)
	require.Equal(t, model.VideoSampleCategoryArchitecture, updated.Category)
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.Equal(t, model.VideoSampleCategoryArchitecture, persisted.Category)

	input.Category = "not-a-category"
	_, err = UpdateVideoSample(context.Background(), db, created.ID, 7, input)
	require.ErrorIs(t, err, ErrInvalidVideoSample)

	_, err = ListVideoSamples(context.Background(), db, "", "not-a-category", "", 24, false, nil)
	require.ErrorIs(t, err, ErrInvalidVideoSample)
}

func TestVideoSampleListRejectsCorruptStoredCategory(t *testing.T) {
	if !videoSampleCategoryFeatureEnabled {
		t.Skip("stored category validation is enabled only in the v6 feature build")
	}
	db, profiles, asset := seedVideoSampleCategoryTestCatalog(t)
	now := time.Now().Unix()
	sample := model.KKAIVideoSample{
		ModelProfileID: profiles[0].ID, Title: "Corrupt", Prompt: "corrupt",
		Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
		VideoAssetID: asset.ID, AspectRatio: 1, Category: "invalid",
		Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&sample).Error)

	_, err := ListVideoSamples(context.Background(), db, "", "", "", 24, false, nil)
	require.ErrorIs(t, err, ErrVideoSampleDataCorrupt)
}

func TestVideoSampleCreateAndUpdateMergeModeScopedProfileDefaults(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	strengthMin, strengthMax, strengthStep := float64(0), float64(1), float64(0.1)
	specification, err := common.Marshal(VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo, VideoModeImageToVideo},
		Parameters: []VideoParameterSpec{
			{Key: "watermark", Label: "Watermark", Control: VideoControlSwitch, Default: false},
			{Key: "strength", Label: "Strength", Control: VideoControlNumber, Modes: []string{VideoModeImageToVideo}, Min: &strengthMin, Max: &strengthMax, Step: &strengthStep},
		},
	})
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: "sample-default-model", DisplayName: "Sample defaults", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: `{"watermark":true,"strength":0.8}`,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
		State: model.VideoAssetStateReady, ObjectKey: "sample-default.mp4", MIMEType: "video/mp4",
		Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	input := VideoSampleInput{
		ModelProfileID: profile.ID, Title: "Defaults", Prompt: "prompt", Mode: VideoModeTextToVideo,
		Parameters: map[string]any{}, ReferenceAssetIDs: []int64{}, VideoAssetID: asset.ID,
		AspectRatio: 16.0 / 9.0, Status: model.VideoSampleStatusDraft,
	}

	created, err := CreateVideoSample(context.Background(), db, 7, input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"watermark": true}, created.Parameters)
	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	storedParameters := map[string]any{}
	require.NoError(t, common.UnmarshalJsonStr(persisted.Parameters, &storedParameters))
	require.Equal(t, map[string]any{"watermark": true}, storedParameters)

	input.Parameters = map[string]any{"watermark": false}
	updated, err := UpdateVideoSample(context.Background(), db, created.ID, 7, input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"watermark": false}, updated.Parameters)
	require.NoError(t, db.First(&persisted, created.ID).Error)
	storedParameters = map[string]any{}
	require.NoError(t, common.UnmarshalJsonStr(persisted.Parameters, &storedParameters))
	require.Equal(t, map[string]any{"watermark": false}, storedParameters)
}

func TestUpdateVideoSamplePreservesPublishedSampleForEnabledModel(t *testing.T) {
	tests := []struct {
		name          string
		sourceEnabled bool
		moveToTarget  bool
		addRemaining  bool
		wantErr       bool
	}{
		{name: "rejects demoting the last published sample", sourceEnabled: true, wantErr: true},
		{name: "rejects moving the last published sample", sourceEnabled: true, moveToTarget: true, wantErr: true},
		{name: "allows demotion when another published sample remains", sourceEnabled: true, addRemaining: true},
		{name: "allows demoting the last sample of a disabled model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			specification, err := common.Marshal(VideoModelSpec{
				Version: 1,
				Modes:   []string{VideoModeTextToVideo},
			})
			require.NoError(t, err)
			now := time.Now().Unix()
			profiles := []model.KKAIVideoModelProfile{
				{
					Model: "sample-source-model", DisplayName: "Sample source", SpecificationVersion: 1,
					Specification: string(specification), DefaultParameters: `{}`, Enabled: tt.sourceEnabled,
					CreatedAt: now, UpdatedAt: now,
				},
				{
					Model: "sample-target-model", DisplayName: "Sample target", SpecificationVersion: 1,
					Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
					CreatedAt: now, UpdatedAt: now,
				},
			}
			for index := range profiles {
				require.NoError(t, db.Create(&profiles[index]).Error)
			}
			asset := model.KKAIVideoAsset{
				OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
				State: model.VideoAssetStateReady, ObjectKey: "sample-update-invariant.mp4", MIMEType: "video/mp4",
				PosterObjectKey: "sample-update-invariant.poster.jpg", PreviewObjectKey: "sample-update-invariant.preview.mp4",
				Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&asset).Error)
			sample := model.KKAIVideoSample{
				ModelProfileID: profiles[0].ID, Title: "Published", Prompt: "prompt",
				Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
				VideoAssetID: asset.ID, AspectRatio: 16.0 / 9.0, Category: model.VideoSampleCategoryOther,
				Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&sample).Error)
			if tt.addRemaining {
				remaining := sample
				remaining.ID = 0
				remaining.Title = "Remaining"
				require.NoError(t, db.Create(&remaining).Error)
			}

			targetProfileID := profiles[0].ID
			status := model.VideoSampleStatusDraft
			if tt.moveToTarget {
				targetProfileID = profiles[1].ID
				status = model.VideoSampleStatusPublished
			}
			_, err = UpdateVideoSample(context.Background(), db, sample.ID, 7, VideoSampleInput{
				ModelProfileID: targetProfileID, Title: sample.Title, Prompt: sample.Prompt, Mode: sample.Mode,
				Parameters: map[string]any{}, ReferenceAssetIDs: []int64{}, VideoAssetID: asset.ID,
				AspectRatio: sample.AspectRatio, Category: sample.Category, Status: status,
			})
			if tt.wantErr {
				require.ErrorIs(t, err, ErrVideoModelNeedsSample)
				var persisted model.KKAIVideoSample
				require.NoError(t, db.First(&persisted, sample.ID).Error)
				require.Equal(t, profiles[0].ID, persisted.ModelProfileID)
				require.Equal(t, model.VideoSampleStatusPublished, persisted.Status)
				return
			}
			require.NoError(t, err)
		})
	}
}
