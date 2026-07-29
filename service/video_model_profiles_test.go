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

func TestRuntimeVideoReferenceProfileDefinesSecondsContract(t *testing.T) {
	profile := runtimeVideoModelProfileView("sd_2.0_special_1080p_with_video_ref")

	require.Equal(t, 2, profile.SpecificationVersion)
	require.Equal(t, 2, profile.Specification.Version)
	require.Equal(t, []string{VideoModeImageToVideo}, profile.Specification.Modes)
	require.Empty(t, profile.DefaultParameters)
	require.Equal(t, []VideoReferenceInputSpec{{
		Role: model.VideoTaskAssetRoleReferenceVideo, RequestKey: "reference_video", Required: true,
	}}, profile.Specification.ReferenceInputs)
	require.Len(t, profile.Specification.Parameters, 1)
	duration := profile.Specification.Parameters[0]
	require.Equal(t, "duration", duration.Key)
	require.Equal(t, "seconds", duration.RequestKey)
	require.Equal(t, VideoControlNumber, duration.Control)
	require.True(t, duration.Required)
	require.Equal(t, float64(5), duration.Default)
	require.Equal(t, float64(4), *duration.Min)
	require.Equal(t, float64(15), *duration.Max)
	require.Equal(t, float64(1), *duration.Step)
	require.NoError(t, ValidateVideoModelSpec(profile.Specification, profile.DefaultParameters))

	ordinary := runtimeVideoModelProfileView("wan2.7-i2v")
	require.Equal(t, 1, ordinary.SpecificationVersion)
	require.Empty(t, ordinary.Specification.Parameters)
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
	require.NoError(t, db.AutoMigrate(&model.KKAIVideoModelProfile{}, &model.KKAIVideoSample{}))
	return db
}

func firstLastReferenceInputs() []VideoReferenceInputSpec {
	return []VideoReferenceInputSpec{
		{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
		{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
	}
}
