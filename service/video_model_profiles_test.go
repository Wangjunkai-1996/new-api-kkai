package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListVideoModelCandidatesReturnsEnabledSeedanceAbilities(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	priority := int64(0)
	abilities := []model.Ability{
		{Group: VideoStudioTokenGroup, Model: "z-model", ChannelId: 1, Enabled: true, Priority: &priority},
		{Group: VideoStudioTokenGroup, Model: "z-model", ChannelId: 2, Enabled: true, Priority: &priority},
		{Group: VideoStudioTokenGroup, Model: "a-model", ChannelId: 3, Enabled: true, Priority: &priority},
		{Group: VideoStudioTokenGroup, Model: "disabled-model", ChannelId: 4, Enabled: false, Priority: &priority},
		{Group: "default", Model: "wrong-group-model", ChannelId: 5, Enabled: true, Priority: &priority},
	}
	for index := range abilities {
		require.NoError(t, db.Create(&abilities[index]).Error)
	}
	require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
		Model: "z-model", DisplayName: "Already configured", SpecificationVersion: 1,
		Specification:     `{"version":1,"modes":["text_to_video"],"parameters":[]}`,
		DefaultParameters: `{}`, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)

	candidates, err := ListVideoModelCandidates(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, []string{"a-model", "z-model"}, candidates)

	emptyDB := newVideoModelProfileTestDB(t)
	empty, err := ListVideoModelCandidates(context.Background(), emptyDB)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestResolveVideoModelProfilePrefersPersistedVideoReferenceProfile(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	specification := VideoModelSpec{Version: 7, Modes: []string{VideoModeTextToVideo}}
	encoded, err := common.Marshal(specification)
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "sd_2.0_special_1080p_with_video_ref", DisplayName: "Persisted override",
		SpecificationVersion: specification.Version, Specification: string(encoded),
		DefaultParameters: `{}`, Enabled: true, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&profile).Error)

	profileID, version, resolvedModel, resolvedSpec, defaults, err := resolveVideoModelProfile(
		context.Background(), db, profile.Model,
	)
	require.NoError(t, err)
	require.Equal(t, profile.ID, profileID)
	require.Equal(t, specification.Version, version)
	require.Equal(t, profile.Model, resolvedModel)
	require.Equal(t, specification, resolvedSpec)
	require.Empty(t, defaults)

	require.NoError(t, db.Model(&profile).Update("enabled", false).Error)
	_, _, _, _, _, err = resolveVideoModelProfile(context.Background(), db, profile.Model)
	require.ErrorIs(t, err, ErrVideoModelProfileNotFound)
	_, _, _, _, _, err = resolveVideoModelProfile(context.Background(), db, "missing-model")
	require.ErrorIs(t, err, ErrVideoModelProfileNotFound)
}

func TestCreateVideoModelProfileRequiresAbilityAndMapsDuplicate(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	input := VideoModelProfileInput{
		Model: "candidate-model", DisplayName: "Candidate", Specification: VideoModelSpec{
			Version: 1, Modes: []string{VideoModeTextToVideo},
		}, DefaultParameters: map[string]any{},
	}

	_, err := CreateVideoModelProfile(context.Background(), db, input)
	require.ErrorIs(t, err, ErrVideoModelAbilityUnavailable)
	priority := int64(0)
	ability := model.Ability{
		Group: VideoStudioTokenGroup, Model: input.Model, ChannelId: 1, Enabled: false, Priority: &priority,
	}
	require.NoError(t, db.Create(&ability).Error)
	_, err = CreateVideoModelProfile(context.Background(), db, input)
	require.ErrorIs(t, err, ErrVideoModelAbilityUnavailable)
	require.NoError(t, db.Model(&ability).Update("enabled", true).Error)

	created, err := CreateVideoModelProfile(context.Background(), db, input)
	require.NoError(t, err)
	require.Equal(t, input.Model, created.Model)
	require.NotNil(t, created.HasPublishedSample)
	require.False(t, *created.HasPublishedSample)
	_, err = CreateVideoModelProfile(context.Background(), db, input)
	require.ErrorIs(t, err, ErrVideoModelProfileDuplicate)
}

func TestAdminVideoModelViewsReportPublishedSampleAvailability(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	now := time.Now().Unix()
	specification := `{"version":1,"modes":["text_to_video"],"parameters":[]}`
	profiles := []model.KKAIVideoModelProfile{
		{Model: "draft-only-model", DisplayName: "Draft only", SpecificationVersion: 1, Specification: specification, DefaultParameters: `{}`, CreatedAt: now, UpdatedAt: now},
		{Model: "published-model", DisplayName: "Published", SpecificationVersion: 1, Specification: specification, DefaultParameters: `{}`, CreatedAt: now, UpdatedAt: now},
	}
	for index := range profiles {
		require.NoError(t, db.Create(&profiles[index]).Error)
	}
	samples := []model.KKAIVideoSample{
		{ModelProfileID: profiles[0].ID, Title: "Draft", Prompt: "draft", Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: 1, AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now},
		{ModelProfileID: profiles[1].ID, Title: "Published", Prompt: "published", Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: 2, AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now},
	}
	for index := range samples {
		require.NoError(t, db.Create(&samples[index]).Error)
	}

	views, err := ListVideoModelProfiles(context.Background(), db, true)
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.NotNil(t, views[0].HasPublishedSample)
	require.False(t, *views[0].HasPublishedSample)
	require.NotNil(t, views[1].HasPublishedSample)
	require.True(t, *views[1].HasPublishedSample)

	published, err := GetVideoModelProfileByID(context.Background(), db, profiles[1].ID)
	require.NoError(t, err)
	require.NotNil(t, published.HasPublishedSample)
	require.True(t, *published.HasPublishedSample)
}

func TestCreateVideoModelProfilePersistsRequiredParameterDefault(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	input := VideoModelProfileInput{
		Model: "required-default-model", DisplayName: "Required default", Specification: VideoModelSpec{
			Version: 1, Modes: []string{VideoModeTextToVideo}, Parameters: []VideoParameterSpec{
				{
					Key: "resolution", Label: "Resolution", Control: VideoControlSelect, Required: true,
					Options: []VideoParameterOption{{Label: "720p", Value: "720p"}},
				},
			},
		}, DefaultParameters: map[string]any{},
	}
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: VideoStudioTokenGroup, Model: input.Model, ChannelId: 1, Enabled: true, Priority: &priority,
	}).Error)

	_, err := CreateVideoModelProfile(context.Background(), db, input)
	require.ErrorIs(t, err, ErrInvalidVideoModelSpec)

	input.DefaultParameters["resolution"] = "720p"
	created, err := CreateVideoModelProfile(context.Background(), db, input)
	require.NoError(t, err)
	persisted, err := GetVideoModelProfileByID(context.Background(), db, created.ID)
	require.NoError(t, err)
	require.Equal(t, input.Specification, persisted.Specification)
	require.Equal(t, map[string]any{"resolution": "720p"}, persisted.DefaultParameters)
	normalized, err := ValidateVideoParameters(
		persisted.Specification, VideoModeTextToVideo, persisted.DefaultParameters, true,
	)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"resolution": "720p"}, normalized)
}

func TestUpdateVideoModelProfileKeepsModelImmutableAndAllowsDisableWithoutAbility(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	now := time.Now().Unix()
	specification := VideoModelSpec{
		Version: 1, Modes: []string{VideoModeTextToVideo}, Parameters: []VideoParameterSpec{},
	}
	specificationJSON, err := common.Marshal(specification)
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "configured-model", DisplayName: "Configured", SpecificationVersion: 1,
		Specification:     string(specificationJSON),
		DefaultParameters: `{}`, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	base := VideoModelProfileInput{
		Model: profile.Model, DisplayName: profile.DisplayName,
		Specification:     specification,
		DefaultParameters: map[string]any{},
	}

	mutated := base
	mutated.Model = "other-model"
	_, err = UpdateVideoModelProfile(context.Background(), db, profile.ID, mutated)
	require.ErrorIs(t, err, ErrVideoModelProfileModelImmutable)

	enabled := base
	enabled.Enabled = true
	_, err = UpdateVideoModelProfile(context.Background(), db, profile.ID, enabled)
	require.ErrorIs(t, err, ErrVideoModelAbilityUnavailable)

	disabled, err := UpdateVideoModelProfile(context.Background(), db, profile.ID, base)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
}

func TestUpdateVideoModelProfileProtectsPublishedSampleReferenceSchema(t *testing.T) {
	tests := []struct {
		name       string
		references []int64
		newInputs  []VideoReferenceInputSpec
		wantError  bool
	}{
		{
			name:       "reference count",
			references: []int64{1},
			newInputs:  firstLastReferenceInputs(),
			wantError:  true,
		},
		{
			name:       "reference role order",
			references: []int64{1, 2},
			newInputs: []VideoReferenceInputSpec{
				{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
				{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
			},
			wantError: false,
		},
		{
			name:       "reference request key",
			references: []int64{1, 2},
			newInputs: []VideoReferenceInputSpec{
				{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_image", Required: true},
				{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoModelProfileTestDB(t)
			oldSpec := VideoModelSpec{Version: 1, Modes: []string{VideoModeFirstLastFrame}, ReferenceInputs: firstLastReferenceInputs()}
			encodedSpec, err := common.Marshal(oldSpec)
			require.NoError(t, err)
			now := time.Now().Unix()
			profile := model.KKAIVideoModelProfile{
				Model: "profile-model", DisplayName: "Profile", SpecificationVersion: 1,
				Specification: string(encodedSpec), DefaultParameters: `{}`, CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&profile).Error)
			referenceJSON, err := common.Marshal(test.references)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.KKAIVideoSample{
				ModelProfileID: profile.ID, Title: "Published", Prompt: "prompt", Mode: VideoModeFirstLastFrame,
				ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: string(referenceJSON), VideoAssetID: 99,
				AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
			}).Error)

			_, err = UpdateVideoModelProfile(context.Background(), db, profile.ID, VideoModelProfileInput{
				Model: profile.Model, DisplayName: profile.DisplayName,
				Specification:     VideoModelSpec{Version: 2, Modes: []string{VideoModeFirstLastFrame}, ReferenceInputs: test.newInputs},
				DefaultParameters: map[string]any{},
			})
			if test.wantError {
				require.ErrorIs(t, err, ErrInvalidVideoModelSpec)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateVideoModelProfileAllowsOptionalAudioForPublishedSeedance25Sample(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	minimum, maximum, step := float64(4), float64(30), float64(1)
	oldRatios := []VideoParameterOption{
		{Label: "16:9", Value: "16:9"},
		{Label: "9:16", Value: "9:16"},
		{Label: "1:1", Value: "1:1"},
	}
	oldSpecification := VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo, VideoModeImageToVideo},
		Parameters: []VideoParameterSpec{
			{
				Key: "duration", Label: "Duration", Control: VideoControlNumber, Required: true,
				Default: float64(5), Min: &minimum, Max: &maximum, Step: &step,
			},
			{Key: "ratio", Label: "Ratio", Control: VideoControlSelect, Required: true, Default: "16:9", Options: oldRatios},
			{
				Key: "resolution", Label: "Resolution", Control: VideoControlSelect, Required: true,
				Default: "720p", Options: []VideoParameterOption{{Label: "720p", Value: "720p"}},
			},
		},
		ReferenceInputs: []VideoReferenceInputSpec{{
			Role: model.VideoTaskAssetRoleReference, RequestKey: "reference_image", Required: true,
		}},
	}
	oldSpecificationJSON, err := common.Marshal(oldSpecification)
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: "seedance-2.5", DisplayName: "Seedance 2.5 720p", SpecificationVersion: oldSpecification.Version,
		Specification:     string(oldSpecificationJSON),
		DefaultParameters: `{"duration":5,"ratio":"16:9","resolution":"720p"}`,
		Enabled:           true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: VideoStudioTokenGroup, Model: profile.Model, ChannelId: 1, Enabled: true, Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Published without audio", Prompt: "A wide establishing shot",
		Mode: VideoModeTextToVideo, ModelVersion: oldSpecification.Version,
		Parameters: `{"duration":5,"ratio":"16:9","resolution":"720p"}`, ReferenceAssetIDs: `[]`,
		VideoAssetID: 99, AspectRatio: 16.0 / 9.0, Status: model.VideoSampleStatusPublished,
		CreatedAt: now, UpdatedAt: now,
	}).Error)

	nextRatios := append([]VideoParameterOption{}, oldRatios...)
	nextRatios = append(nextRatios,
		VideoParameterOption{Label: "4:3", Value: "4:3"},
		VideoParameterOption{Label: "3:4", Value: "3:4"},
		VideoParameterOption{Label: "21:9", Value: "21:9"},
		VideoParameterOption{Label: "Adaptive", Value: "adaptive"},
	)
	nextSpecification := oldSpecification
	nextSpecification.Version = 2
	nextSpecification.Parameters = append([]VideoParameterSpec{}, oldSpecification.Parameters...)
	nextSpecification.Parameters[1].Options = nextRatios
	nextSpecification.Parameters = append(nextSpecification.Parameters, VideoParameterSpec{
		Key: "generate_audio", Label: "Generate audio", Control: VideoControlSwitch, Default: true,
	})
	defaults := map[string]any{
		"duration": float64(5), "ratio": "16:9", "resolution": "720p", "generate_audio": true,
	}

	updated, err := UpdateVideoModelProfile(context.Background(), db, profile.ID, VideoModelProfileInput{
		Model: profile.Model, DisplayName: profile.DisplayName, Specification: nextSpecification,
		DefaultParameters: defaults, Enabled: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.SpecificationVersion)
	assert.Equal(t, 2, updated.Specification.Version)
	assert.Equal(t, nextRatios, updated.Specification.Parameters[1].Options)
	assert.False(t, updated.Specification.Parameters[3].Required)
	assert.Equal(t, true, updated.Specification.Parameters[3].Default)
	assert.Equal(t, defaults, updated.DefaultParameters)
	assert.True(t, updated.Enabled)
	require.NotNil(t, updated.HasPublishedSample)
	assert.True(t, *updated.HasPublishedSample)
}

func TestCreateVideoSamplePersistsReferenceMappingSnapshot(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	specification := VideoModelSpec{
		Version: 1, Modes: []string{VideoModeFirstLastFrame}, ReferenceInputs: []VideoReferenceInputSpec{
			{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
			{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
		},
	}
	encodedSpec, err := common.Marshal(specification)
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "snapshot-model", DisplayName: "Snapshot", SpecificationVersion: 1,
		Specification: string(encodedSpec), DefaultParameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "sample.mp4", MIMEType: "video/mp4", Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "first.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "last.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}

	created, err := CreateVideoSample(context.Background(), db, 7, VideoSampleInput{
		ModelProfileID: profile.ID, Title: "Snapshot", Prompt: "prompt", Mode: VideoModeFirstLastFrame,
		Parameters: map[string]any{}, ReferenceAssetIDs: []int64{assets[1].ID, assets[2].ID},
		VideoAssetID: assets[0].ID, AspectRatio: 16.0 / 9.0, Status: model.VideoSampleStatusDraft,
	})
	require.NoError(t, err)

	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	var snapshots []struct {
		AssetID    int64  `json:"asset_id"`
		Role       string `json:"role"`
		RequestKey string `json:"request_key"`
	}
	require.NoError(t, common.UnmarshalJsonStr(persisted.ReferenceAssetIDs, &snapshots))
	require.Equal(t, []struct {
		AssetID    int64  `json:"asset_id"`
		Role       string `json:"role"`
		RequestKey string `json:"request_key"`
	}{
		{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame"},
		{AssetID: assets[2].ID, Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame"},
	}, snapshots)
}

func newVideoModelProfileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-profile-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KKAIVideoModelProfile{}, &model.KKAIVideoSample{}, &model.Ability{}))
	return db
}

func firstLastReferenceInputs() []VideoReferenceInputSpec {
	return []VideoReferenceInputSpec{
		{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
		{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
	}
}
