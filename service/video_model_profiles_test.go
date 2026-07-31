package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
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
	_, err = CreateVideoModelProfile(context.Background(), db, input)
	require.ErrorIs(t, err, ErrVideoModelProfileDuplicate)
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
