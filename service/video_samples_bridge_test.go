//go:build kkai_bridge

package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

func TestVideoSampleBridgeSupportsPhysicalV5Schema(t *testing.T) {
	db, profiles, asset := seedVideoSampleCategoryTestCatalog(t)
	now := time.Now().Unix()
	legacy := model.KKAIVideoSample{
		ModelProfileID: profiles[0].ID, Title: "Legacy", Prompt: "legacy",
		Mode: VideoModeTextToVideo, ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`,
		VideoAssetID: asset.ID, AspectRatio: 1, Status: model.VideoSampleStatusPublished,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Migrator().DropColumn(&model.KKAIVideoSample{}, "category"))
	require.False(t, db.Migrator().HasColumn(&model.KKAIVideoSample{}, "category"))

	page, err := ListVideoSamples(context.Background(), db, "", "", "", 24, false, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, model.VideoSampleCategoryOther, page.Items[0].Category)

	view, err := GetVideoSample(context.Background(), db, legacy.ID, false, nil)
	require.NoError(t, err)
	require.Equal(t, model.VideoSampleCategoryOther, view.Category)

	input := VideoSampleInput{
		ModelProfileID: profiles[0].ID, Title: "Bridge create", Prompt: "bridge", Mode: VideoModeTextToVideo,
		Parameters: map[string]any{}, ReferenceAssetIDs: []int64{}, VideoAssetID: asset.ID,
		AspectRatio: 1, Category: model.VideoSampleCategoryOther, Status: model.VideoSampleStatusDraft,
	}
	created, err := CreateVideoSample(context.Background(), db, 7, input)
	require.NoError(t, err)
	require.Equal(t, model.VideoSampleCategoryOther, created.Category)

	input.Title = "Bridge update"
	updated, err := UpdateVideoSample(context.Background(), db, created.ID, 7, input)
	require.NoError(t, err)
	require.Equal(t, "Bridge update", updated.Title)

	input.Category = model.VideoSampleCategoryPeople
	_, err = CreateVideoSample(context.Background(), db, 7, input)
	require.ErrorIs(t, err, ErrInvalidVideoSample)
	_, err = ListVideoSamples(context.Background(), db, "", model.VideoSampleCategoryPeople, "", 24, false, nil)
	require.ErrorIs(t, err, ErrInvalidVideoSample)
}
