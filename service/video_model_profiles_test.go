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

func TestCreateVideoModelProfileRequiresAbilityAllowsEnableWithoutSampleAndMapsDuplicate(t *testing.T) {
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

	input.Enabled = true
	created, err := CreateVideoModelProfile(context.Background(), db, input)
	require.NoError(t, err)
	require.Equal(t, input.Model, created.Model)
	require.True(t, created.Enabled)
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
		{ModelProfileID: profiles[0].ID, Title: "Draft", Prompt: "draft", Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `{"reference_assets":[]}`, VideoAssetID: 1, AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now},
		{ModelProfileID: profiles[1].ID, Title: "Published", Prompt: "published", Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `{"reference_assets":[]}`, VideoAssetID: 2, AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now},
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

func TestUpdateVideoModelProfileIgnoresHistoricalPublishedSampleReferenceSchema(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	old := VideoModelSpec{Version: 1, Modes: []string{"text_to_video"}}
	encoded, err := common.Marshal(old)
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{Model: "historical", DisplayName: "Historical", SpecificationVersion: 1, Specification: string(encoded), DefaultParameters: `{}`, Enabled: false, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&profile).Error)
	require.NoError(t, db.Create(&model.KKAIVideoSample{ModelProfileID: profile.ID, Title: "old", Prompt: "old", Mode: "text_to_video", ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: 1, AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now}).Error)
	next := old
	next.Version = 2
	_, err = UpdateVideoModelProfile(context.Background(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: profile.DisplayName, Specification: next, DefaultParameters: map[string]any{}})
	require.NoError(t, err)
}

func TestUpdateVideoModelProfileRequiresVerifiedReferencePolicyWhenEnabled(t *testing.T) {
	db := newVideoModelProfileTestDB(t)
	specification := VideoModelSpec{Version: 1, Modes: []string{VideoModeReferenceToVideo}}
	encoded, err := common.Marshal(specification)
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{Model: "policy-required", DisplayName: "Policy required", SpecificationVersion: 1, Specification: string(encoded), DefaultParameters: `{}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&profile).Error)
	_, err = UpdateVideoModelProfile(context.Background(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: profile.DisplayName, Specification: specification, DefaultParameters: map[string]any{}, Enabled: true})
	require.ErrorIs(t, err, ErrInvalidVideoModelSpec)
}

func TestVideoModelProfileEnablesWithOnlyPerMediaLimits(t *testing.T) {
	for _, test := range []struct {
		name        string
		legacyTotal *int
	}{{"no-aggregate", nil}, {"old-positive-aggregate", intPointer(1)}, {"old-zero-aggregate", intPointer(0)}} {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoModelProfileTestDB(t)
			policy := videoTestReferencePolicy()
			policy.MaxTotalCount = test.legacyTotal
			input := VideoModelProfileInput{
				Model: test.name, DisplayName: test.name, Enabled: true,
				Specification:     VideoModelSpec{Version: 1, Modes: []string{VideoModeReferenceToVideo}, ReferencePolicy: policy},
				DefaultParameters: map[string]any{},
			}
			require.NoError(t, db.Create(&model.Ability{Group: VideoStudioTokenGroup, Model: input.Model, ChannelId: 1, Enabled: true}).Error)
			created, err := CreateVideoModelProfile(context.Background(), db, input)
			require.NoError(t, err)
			assert.True(t, created.Enabled)
			listed, err := ListVideoModelProfiles(context.Background(), db, false)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, created.ID, listed[0].ID)
			_, _, _, _, _, err = resolveVideoModelProfile(context.Background(), db, input.Model)
			require.NoError(t, err)
		})
	}
}

func TestCreateVideoSamplePersistsReferenceMappingSnapshot(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	specification := VideoModelSpec{
		Version: 1, Modes: []string{VideoModeReferenceToVideo}, ReferencePolicy: &VideoReferencePolicy{MaxTotalCount: intPointer(2), RequiresVisual: true, Image: &VideoReferenceMediaPolicy{AllowedMIMETypes: []string{"image/png"}, MaxCount: 2, MaxSizeBytes: 2_000_000, MinWidth: 1, MaxWidth: 8192, MinHeight: 1, MaxHeight: 8192, MinAspectRatioExclusive: .1, MaxAspectRatioExclusive: 10}},
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
		if assets[index].Kind == model.VideoAssetKindReference {
			assets[index].Width, assets[index].Height, assets[index].SizeBytes, assets[index].MediaMetadata = 1280, 720, 100, `{"inspection_version":1,"container":"png"}`
		}
		require.NoError(t, db.Create(&assets[index]).Error)
	}

	created, err := CreateVideoSample(context.Background(), db, 7, VideoSampleInput{
		ModelProfileID: profile.ID, Title: "Snapshot", Prompt: "prompt", Mode: VideoModeReferenceToVideo,
		Parameters: map[string]any{}, ReferenceAssets: []VideoStudioReferenceAssetInput{{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleReferenceImage, Position: intPointer(0)}, {AssetID: assets[2].ID, Role: model.VideoTaskAssetRoleReferenceImage, Position: intPointer(1)}},
		VideoAssetID: assets[0].ID, AspectRatio: 16.0 / 9.0, Status: model.VideoSampleStatusDraft,
	})
	require.NoError(t, err)

	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	var document videoSampleReferenceDocument
	require.NoError(t, common.UnmarshalJsonStr(persisted.ReferenceAssetIDs, &document))
	require.Equal(t, []VideoSampleReferenceSnapshot{{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleReferenceImage, Position: 0}, {AssetID: assets[2].ID, Role: model.VideoTaskAssetRoleReferenceImage, Position: 1}}, document.ReferenceAssets)
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
