package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

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

	page, err := ListVideoSamples(context.Background(), db, "", "", 24, false, []string{"allowed-model"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "allowed-model", page.Items[0].Model)

	_, err = GetVideoSample(context.Background(), db, samples[1].ID, false, []string{"allowed-model"})
	require.ErrorIs(t, err, ErrVideoSampleNotFound)

	empty, err := ListVideoSamples(context.Background(), db, "", "", 24, false, []string{})
	require.NoError(t, err)
	require.Empty(t, empty.Items)
}
