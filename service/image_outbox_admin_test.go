package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRedriveImageThumbnailDeadEventResetsAssetAndIsIdempotent(t *testing.T) {
	db := newImageLibraryTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	asset := model.KKAIImageAsset{
		OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateReady, ObjectKey: "image/redrive-thumbnail",
		ThumbnailState: model.ImageThumbnailStateFailed, FailureReason: "thumbnail_failed",
		MIMEType: "image/png", SizeBytes: 10, Width: 1, Height: 1,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(imageThumbnailPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := createDeadImageOutboxEvent(t, db, ImageAssetThumbnailTopic, asset.ID, string(payload), now)

	redriven, applied, err := RedriveImageOutboxDeadEvent(
		context.Background(), db, event.ID, "thumbnail-retry-1", "admin:42", now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, model.KKAIOutboxStatusPending, redriven.Status)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.ImageThumbnailStatePending, asset.ThumbnailState)
	require.Empty(t, asset.FailureReason)

	duplicate, applied, err := RedriveImageOutboxDeadEvent(
		context.Background(), db, event.ID, "thumbnail-retry-1", "admin:42", now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, redriven.ID, duplicate.ID)
}

func TestRedriveImageDeleteDeadEventRequiresLogicallyDeletedAsset(t *testing.T) {
	db := newImageLibraryTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	asset := model.KKAIImageAsset{
		OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateDeleted, ObjectKey: "image/redrive-delete",
		ThumbnailState: model.ImageThumbnailStateReady, ThumbnailObjectKey: "image/redrive-delete.thumb",
		DeletedAt: now.Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(imageAssetDeletePayload{
		AssetID: asset.ID, ObjectKey: asset.ObjectKey, ThumbnailObjectKey: asset.ThumbnailObjectKey,
	})
	require.NoError(t, err)
	event := createDeadImageOutboxEvent(t, db, ImageAssetDeleteTopic, asset.ID, string(payload), now)

	_, applied, err := RedriveImageOutboxDeadEvent(
		context.Background(), db, event.ID, "delete-retry-1", "admin:42", now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.True(t, applied)

	active := model.KKAIImageAsset{
		OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateReady, ObjectKey: "image/not-deleted",
		ThumbnailState: model.ImageThumbnailStateReady,
		CreatedAt:      now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&active).Error)
	payload, err = common.Marshal(imageAssetDeletePayload{AssetID: active.ID, ObjectKey: active.ObjectKey})
	require.NoError(t, err)
	conflict := createDeadImageOutboxEvent(t, db, ImageAssetDeleteTopic, active.ID, string(payload), now)
	_, applied, err = RedriveImageOutboxDeadEvent(
		context.Background(), db, conflict.ID, "delete-retry-2", "admin:42", now.Add(time.Minute),
	)
	require.ErrorIs(t, err, ErrImageOutboxRedriveConflict)
	require.False(t, applied)
}

func TestRedriveImageOutboxRejectsNonImageTopic(t *testing.T) {
	db := newImageLibraryTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := createDeadImageOutboxEvent(t, db, "video.asset.archive.v1", 1, `{}`, now)

	_, applied, err := RedriveImageOutboxDeadEvent(
		context.Background(), db, event.ID, "wrong-topic", "admin:42", now,
	)
	require.ErrorIs(t, err, ErrImageOutboxEventNotFound)
	require.False(t, applied)
}

func createDeadImageOutboxEvent(
	t *testing.T,
	db *gorm.DB,
	topic string,
	assetID int64,
	payload string,
	now time.Time,
) model.KKAIOutboxEvent {
	t.Helper()
	event := model.KKAIOutboxEvent{
		EventKey: "image-redrive-" + topic + "-" + strconv.FormatInt(assetID, 10),
		Topic:    topic, AggregateID: strconv.FormatInt(assetID, 10), Payload: payload,
		Status: model.KKAIOutboxStatusDead, Attempts: 12, AvailableAt: now.Unix(),
		LastError: "delivery failed", CreatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&event).Error)
	return event
}
