package service

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imagePipelineStore struct {
	objects   map[string][]byte
	putErr    error
	deleteErr error
	afterPut  func()
}

func (store *imagePipelineStore) PresignDownload(context.Context, string, string, bool, time.Duration) (string, error) {
	return "", nil
}

func (store *imagePipelineStore) Get(context.Context, string) (ImageAssetObject, error) {
	return ImageAssetObject{}, errors.New("not implemented")
}

func (store *imagePipelineStore) Put(_ context.Context, key string, _ string, reader io.Reader, _ int64) error {
	if store.putErr != nil {
		return store.putErr
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	store.objects[key] = body
	if store.afterPut != nil {
		store.afterPut()
	}
	return nil
}

func (store *imagePipelineStore) Delete(_ context.Context, keys []string) error {
	if store.deleteErr != nil {
		return store.deleteErr
	}
	for _, key := range keys {
		delete(store.objects, key)
	}
	return nil
}

type imagePipelineFetcher struct {
	payload []byte
	err     error
}

func (fetcher imagePipelineFetcher) FetchURL(context.Context, string, int64, int64) (*FetchedImageArchive, error) {
	return fetcher.fetch()
}

func (fetcher imagePipelineFetcher) FetchBase64(string, int64, int64) (*FetchedImageArchive, error) {
	return fetcher.fetch()
}

func (fetcher imagePipelineFetcher) fetch() (*FetchedImageArchive, error) {
	if fetcher.err != nil {
		return nil, fetcher.err
	}
	file, err := os.CreateTemp("", "image-pipeline-fetch-*.png")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if _, err := file.Write(fetcher.payload); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &FetchedImageArchive{
		Path: path, MIMEType: "image/png", SizeBytes: int64(len(fetcher.payload)),
		Width: 2, Height: 1, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, nil
}

func TestImageAssetPipelinePersistsReadyAssetAndThumbnailEvent(t *testing.T) {
	db := newImagePipelineTestDB(t)
	store := &imagePipelineStore{objects: map[string][]byte{}}
	payload := imageArchiveTestPNG(t, 2, 1)
	pipeline, err := NewImageAssetPipeline(db, store, imagePipelineFetcher{payload: payload}, 1<<20, 100)
	require.NoError(t, err)
	generation := imagePipelineGeneration(t, db)

	result, err := pipeline.ArchiveGeneration(context.Background(), generation, []ImageRelayResult{{Base64: "ignored"}})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Ready)
	assert.Equal(t, 0, result.Failed)
	require.Len(t, result.Assets, 1)
	assert.Equal(t, model.ImageAssetStateReady, result.Assets[0].State)
	assert.Equal(t, "image-"+strconv.FormatInt(result.Assets[0].ID, 10)+".png", result.Assets[0].OriginalFilename)
	assert.Equal(t, payload, store.objects[result.Assets[0].ObjectKey])
	var persistedAsset model.KKAIImageAsset
	require.NoError(t, db.First(&persistedAsset, result.Assets[0].ID).Error)
	assert.Equal(t, result.Assets[0].OriginalFilename, persistedAsset.OriginalFilename)

	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", ImageAssetThumbnailTopic).First(&event).Error)
	assert.Equal(t, ImageAssetThumbnailTopic, event.Topic)
	assert.Equal(t, "image-thumbnail:"+event.AggregateID+":v1", event.EventKey)
	var compensation model.KKAIOutboxEvent
	require.NoError(t, db.Where(
		"event_key = ?", imageAssetPutCompensationEventKey(result.Assets[0].ID),
	).First(&compensation).Error)
	assert.Equal(t, model.KKAIOutboxStatusDelivered, compensation.Status)
}

func TestImageAssetPipelineRecordsClassifiedFailureWithoutLeakingSource(t *testing.T) {
	db := newImagePipelineTestDB(t)
	store := &imagePipelineStore{objects: map[string][]byte{}}
	pipeline, err := NewImageAssetPipeline(
		db, store, imagePipelineFetcher{err: errors.New("https://secret.example/token=secret")}, 1<<20, 100,
	)
	require.NoError(t, err)
	generation := imagePipelineGeneration(t, db)

	result, err := pipeline.ArchiveGeneration(context.Background(), generation, []ImageRelayResult{{URL: "https://source.example/image"}})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Ready)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Assets, 1)
	assert.Equal(t, "archive_failed", result.Assets[0].FailureReason)
	assert.NotContains(t, result.Assets[0].FailureReason, "secret")
}

func TestImageAssetPipelineRejectsWorkerAfterRecoveryFence(t *testing.T) {
	db := newImagePipelineTestDB(t)
	store := &imagePipelineStore{objects: map[string][]byte{}}
	payload := imageArchiveTestPNG(t, 2, 1)
	pipeline, err := NewImageAssetPipeline(db, store, imagePipelineFetcher{payload: payload}, 1<<20, 100)
	require.NoError(t, err)
	generation := imagePipelineGeneration(t, db)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("status", model.ImageGenerationStatusRecovering).Error)

	_, err = pipeline.ArchiveGeneration(
		context.Background(), generation, []ImageRelayResult{{Base64: "ignored"}},
	)
	require.ErrorIs(t, err, ErrImageGenerationConflict)
	require.Empty(t, store.objects)
	var assets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Count(&assets).Error)
	require.Zero(t, assets)
}

func TestImageAssetPipelineDurablyDeletesLatePutAfterRecoveryFence(t *testing.T) {
	db := newImagePipelineTestDB(t)
	payload := imageArchiveTestPNG(t, 2, 1)
	store := &imagePipelineStore{
		objects:   map[string][]byte{},
		deleteErr: errors.New("temporary object delete failure"),
	}
	pipeline, err := NewImageAssetPipeline(db, store, imagePipelineFetcher{payload: payload}, 1<<20, 100)
	require.NoError(t, err)
	generation := imagePipelineGeneration(t, db)
	store.afterPut = func() {
		require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
			Update("status", model.ImageGenerationStatusRecovering).Error)
	}

	_, err = pipeline.ArchiveGeneration(
		context.Background(), generation, []ImageRelayResult{{Base64: "ignored"}},
	)
	require.ErrorIs(t, err, ErrImageGenerationConflict)
	require.Len(t, store.objects, 1)
	var asset model.KKAIImageAsset
	require.NoError(t, db.Where("generation_id = ?", generation.ID).First(&asset).Error)
	var compensation model.KKAIOutboxEvent
	require.NoError(t, db.Where(
		"event_key = ?", imageAssetPutCompensationEventKey(asset.ID),
	).First(&compensation).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, compensation.Status)

	store.deleteErr = nil
	require.NoError(t, (&ImageAssetOutboxPipeline{store: store}).HandleDelete(
		context.Background(), compensation,
	))
	require.Empty(t, store.objects)
}

func newImagePipelineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:image-pipeline-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageGeneration{}, &model.KKAIImageAsset{}, &model.KKAIOutboxEvent{}))
	return db
}

func imagePipelineGeneration(t *testing.T, db *gorm.DB) model.KKAIImageGeneration {
	t.Helper()
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 9, ModelProfileID: 11, SpecificationVersion: 1,
		Model: "image-model", Prompt: "prompt", Parameters: "{}",
		RequestHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RequestID:   "req-image-pipeline", Status: model.ImageGenerationStatusSubmitting,
		RequestedCount: 1, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return generation
}
