package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type memoryVideoAssetStore struct {
	mutex               sync.Mutex
	objects             map[string][]byte
	contentType         map[string]string
	sha256              map[string]string
	archiveSourceSHA256 map[string]string
	putCount            map[string]int
	deleteCount         map[string]int
	downloadKey         string
	downloadAttachment  bool
	downloadExpires     time.Duration
}

type callbackArchiveVideoAssetStore struct {
	*memoryVideoAssetStore
	putArchive func(context.Context, string, string, io.Reader, int64, string, string) error
}

type callbackDeleteVideoAssetStore struct {
	*memoryVideoAssetStore
	deleteObjects func(context.Context, []string) error
}

func (store *callbackDeleteVideoAssetStore) Delete(ctx context.Context, keys []string) error {
	if store.deleteObjects != nil {
		return store.deleteObjects(ctx, keys)
	}
	return store.memoryVideoAssetStore.Delete(ctx, keys)
}

func (store *callbackArchiveVideoAssetStore) PutArchive(
	ctx context.Context,
	key string,
	contentType string,
	reader io.Reader,
	length int64,
	sha256 string,
	archiveSourceSHA256 string,
) error {
	if store.putArchive != nil {
		return store.putArchive(ctx, key, contentType, reader, length, sha256, archiveSourceSHA256)
	}
	return store.memoryVideoAssetStore.PutArchive(ctx, key, contentType, reader, length, sha256, archiveSourceSHA256)
}

func newMemoryVideoAssetStore() *memoryVideoAssetStore {
	return &memoryVideoAssetStore{
		objects: make(map[string][]byte), contentType: make(map[string]string),
		sha256: make(map[string]string), archiveSourceSHA256: make(map[string]string),
		putCount: make(map[string]int), deleteCount: make(map[string]int),
	}
}

func (store *memoryVideoAssetStore) PresignUpload(context.Context, string, string, int64, time.Duration) (VideoAssetSignedRequest, error) {
	return VideoAssetSignedRequest{}, nil
}

func (store *memoryVideoAssetStore) PresignDownload(_ context.Context, key string, _ string, attachment bool, expires time.Duration) (string, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.downloadKey = key
	store.downloadAttachment = attachment
	store.downloadExpires = expires
	return "https://signed.example/object", nil
}

func (store *memoryVideoAssetStore) Head(_ context.Context, key string) (VideoAssetObjectMetadata, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	content, exists := store.objects[key]
	if !exists {
		return VideoAssetObjectMetadata{}, ErrVideoAssetObjectNotFound
	}
	return VideoAssetObjectMetadata{
		ContentType: store.contentType[key], ContentLength: int64(len(content)), SHA256: store.sha256[key],
		ArchiveSourceSHA256: store.archiveSourceSHA256[key],
	}, nil
}

func (store *memoryVideoAssetStore) Get(_ context.Context, key string) (VideoAssetObject, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	content, exists := store.objects[key]
	if !exists {
		return VideoAssetObject{}, fmt.Errorf("missing object")
	}
	return VideoAssetObject{
		Body:        io.NopCloser(bytes.NewReader(append([]byte(nil), content...))),
		ContentType: store.contentType[key], ContentLength: int64(len(content)),
	}, nil
}

func (store *memoryVideoAssetStore) Put(_ context.Context, key string, contentType string, reader io.Reader, length int64) error {
	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if int64(len(content)) != length {
		return fmt.Errorf("length mismatch")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.objects[key] = content
	store.contentType[key] = contentType
	store.putCount[key]++
	return nil
}

func (store *memoryVideoAssetStore) PutArchive(
	ctx context.Context,
	key string,
	contentType string,
	reader io.Reader,
	length int64,
	sha256 string,
	archiveSourceSHA256 string,
) error {
	if err := store.Put(ctx, key, contentType, reader, length); err != nil {
		return err
	}
	store.mutex.Lock()
	store.sha256[key] = sha256
	store.archiveSourceSHA256[key] = archiveSourceSHA256
	store.mutex.Unlock()
	return nil
}

func (store *memoryVideoAssetStore) Delete(_ context.Context, keys []string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, key := range keys {
		delete(store.objects, key)
		delete(store.contentType, key)
		delete(store.sha256, key)
		delete(store.archiveSourceSHA256, key)
		store.deleteCount[key]++
	}
	return nil
}

type staticVideoArchiveFetcher struct {
	path     string
	mimeType string
	sha256   string
	fetches  int
}

func (fetcher *staticVideoArchiveFetcher) Fetch(context.Context, string, int64) (*FetchedVideoArchive, error) {
	fetcher.fetches++
	content, err := os.ReadFile(fetcher.path)
	if err != nil {
		return nil, err
	}
	copyPath := fetcher.path + fmt.Sprintf("-%d", fetcher.fetches)
	if err := os.WriteFile(copyPath, content, 0o600); err != nil {
		return nil, err
	}
	return &FetchedVideoArchive{
		Path: copyPath, MIMEType: fetcher.mimeType, SizeBytes: int64(len(content)), SHA256: fetcher.sha256,
	}, nil
}

type staticVideoMediaProcessor struct{}

func (staticVideoMediaProcessor) Inspect(context.Context, string) (VideoMediaMetadata, error) {
	return VideoMediaMetadata{MIMEType: "video/mp4", Width: 1280, Height: 720, DurationSeconds: 4, Codec: "h264"}, nil
}

func (staticVideoMediaProcessor) CreatePoster(_ context.Context, _ string, output string, _ int64) error {
	return os.WriteFile(output, []byte("poster"), 0o600)
}

func (staticVideoMediaProcessor) CreateImageThumbnail(_ context.Context, _ string, output string, _ int64) error {
	return os.WriteFile(output, []byte("poster"), 0o600)
}

func (staticVideoMediaProcessor) CreatePreview(_ context.Context, _ string, output string) error {
	return os.WriteFile(output, []byte("preview"), 0o600)
}

type callbackVideoMediaProcessor struct {
	inspect       func(context.Context, string) (VideoMediaMetadata, error)
	createPoster  func(context.Context, string, string, int64) error
	createPreview func(context.Context, string, string) error
}

func (processor callbackVideoMediaProcessor) Inspect(ctx context.Context, input string) (VideoMediaMetadata, error) {
	if processor.inspect != nil {
		return processor.inspect(ctx, input)
	}
	return staticVideoMediaProcessor{}.Inspect(ctx, input)
}

func (processor callbackVideoMediaProcessor) CreatePoster(ctx context.Context, input string, output string, maxBytes int64) error {
	if processor.createPoster != nil {
		return processor.createPoster(ctx, input, output, maxBytes)
	}
	return staticVideoMediaProcessor{}.CreatePoster(ctx, input, output, maxBytes)
}

func (processor callbackVideoMediaProcessor) CreatePreview(ctx context.Context, input string, output string) error {
	if processor.createPreview != nil {
		return processor.createPreview(ctx, input, output)
	}
	return staticVideoMediaProcessor{}.CreatePreview(ctx, input, output)
}

type callbackVideoArchiveFetcher func(context.Context, string, int64) (*FetchedVideoArchive, error)

func (fetcher callbackVideoArchiveFetcher) Fetch(ctx context.Context, sourceURL string, maxBytes int64) (*FetchedVideoArchive, error) {
	return fetcher(ctx, sourceURL, maxBytes)
}

func newVideoPipelineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-pipeline-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Task{}, &model.KKAIVideoGeneration{}, &model.KKAIVideoAsset{},
		&model.KKAIVideoTaskAsset{}, &model.KKAIVideoModelProfile{}, &model.KKAIVideoSample{}, &model.KKAIOutboxEvent{},
		&model.Channel{},
	))
	return db
}

func TestReconcileVideoTaskOutputsCreatesOneArchiveAsset(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	task := model.Task{
		TaskID: "task_output", UserId: 9,
		Status: model.TaskStatusSuccess, Progress: "100%", CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{ResultURL: "https://media.example/output.mp4"},
	}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		UserID: 9, TaskID: task.ID, ModelProfileID: 3, Model: "video-model", Mode: VideoModeTextToVideo,
		Prompt: "test", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)

	created, err := ReconcileVideoTaskOutputs(context.Background(), db, 20)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	created, err = ReconcileVideoTaskOutputs(context.Background(), db, 20)
	require.NoError(t, err)
	require.Zero(t, created)

	var assets int64
	var links int64
	var events int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).Count(&links).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicArchive).Count(&events).Error)
	require.EqualValues(t, 1, assets)
	require.EqualValues(t, 1, links)
	require.EqualValues(t, 1, events)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset).Error)
	require.Equal(t, videoTaskResultArchiveSource(task.ID), asset.ArchiveSourceURL)
	var reloadedTask model.Task
	require.NoError(t, db.First(&reloadedTask, task.ID).Error)
	require.True(t, reloadedTask.PrivateData.AssetHostedResult)
}

func TestVideoAssetPipelineArchivesManagedDataResultWithoutPublicProxy(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	task := model.Task{
		TaskID: "task_data_archive", UserId: 9, Status: model.TaskStatusSuccess, Progress: "100%",
		Quota: 456, CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{
			ResultURL:         "https://gateway.example/v1/videos/task_data_archive/content",
			ArchiveSource:     "data:video/mp4;base64,dmlkZW8=",
			AssetHostedResult: true,
			BillingState:      model.TaskBillingStateCompleted,
			TokenQuota:        456,
		},
		Data: []byte(`{"provider":"raw audit payload"}`),
	}
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Create(&model.KKAIVideoGeneration{
		UserID: 9, TaskID: task.ID, ModelProfileID: 3, Model: "video-model", Mode: VideoModeTextToVideo,
		Prompt: "test", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}).Error)

	created, err := ReconcileVideoTaskOutputs(context.Background(), db, 20)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset).Error)
	require.Equal(t, videoTaskResultArchiveSource(task.ID), asset.ArchiveSourceURL)

	tempDir := t.TempDir()
	fetcher := NewHTTPVideoArchiveFetcher(tempDir)
	fetcher.availableBytes = func(string) (uint64, error) { return 2 << 30, nil }
	store := newMemoryVideoAssetStore()
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	require.NoError(t, pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{
		Payload: string(payload), Topic: VideoOutboxTopicArchive,
	}))

	require.Equal(t, []byte("video"), store.objects[asset.ObjectKey])
	var reloadedTask model.Task
	require.NoError(t, db.First(&reloadedTask, task.ID).Error)
	require.Empty(t, reloadedTask.PrivateData.ResultURL)
	require.Empty(t, reloadedTask.PrivateData.ArchiveSource)
	require.True(t, reloadedTask.PrivateData.AssetHostedResult)
	require.Equal(t, model.TaskBillingStateCompleted, reloadedTask.PrivateData.BillingState)
	require.Equal(t, 456, reloadedTask.PrivateData.TokenQuota)
	require.Equal(t, 456, reloadedTask.Quota)
	require.JSONEq(t, `{"provider":"raw audit payload"}`, string(reloadedTask.Data))
}

func TestVideoAssetPipelineArchivesManagedProviderContentWithoutPublicProxy(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	providerCalls := atomic.Int32{}
	requestPath := make(chan string, 1)
	authorization := make(chan string, 1)
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls.Add(1)
		requestPath <- request.URL.Path
		authorization <- request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "provider-video")
	}))
	t.Cleanup(providerServer.Close)
	providerURL := providerServer.URL

	channel := model.Channel{
		Id: 91, Type: constant.ChannelTypeSora, Key: "provider-key", BaseURL: common.GetPointer(providerURL),
	}
	require.NoError(t, db.Create(&channel).Error)
	now := time.Now().Unix()
	task := model.Task{
		TaskID: "task_provider_archive", UserId: 9, ChannelId: channel.Id,
		Status: model.TaskStatusSuccess, Progress: "100%", CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID:    "upstream-video",
			ResultURL:         "https://gateway.example/v1/videos/task_provider_archive/content",
			AssetHostedResult: true,
		},
		Data: []byte(`{"id":"upstream-video","status":"completed"}`),
	}
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Create(&model.KKAIVideoGeneration{
		UserID: 9, TaskID: task.ID, ModelProfileID: 3, Model: "video-model", Mode: VideoModeTextToVideo,
		Prompt: "test", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}).Error)
	created, err := ReconcileVideoTaskOutputs(context.Background(), db, 20)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset).Error)

	tempDir := t.TempDir()
	fetcher := NewHTTPVideoArchiveFetcher(tempDir)
	fetcher.availableBytes = func(string) (uint64, error) { return 2 << 30, nil }
	providerClientCalls := atomic.Int32{}
	fetcher.providerClient = func(proxy string) (*http.Client, error) {
		require.Empty(t, proxy)
		providerClientCalls.Add(1)
		return providerServer.Client(), nil
	}
	store := newMemoryVideoAssetStore()
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	require.NoError(t, pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{
		Payload: string(payload), Topic: VideoOutboxTopicArchive,
	}))

	require.EqualValues(t, 1, providerCalls.Load())
	require.EqualValues(t, 1, providerClientCalls.Load())
	require.Equal(t, "/v1/videos/upstream-video/content", <-requestPath)
	require.Equal(t, "Bearer provider-key", <-authorization)
	require.Equal(t, []byte("provider-video"), store.objects[asset.ObjectKey])
	expectedArchiveSourceSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(providerURL+"/v1/videos/upstream-video/content")))
	require.Equal(t, expectedArchiveSourceSHA256, store.archiveSourceSHA256[asset.ObjectKey])
	var reloadedTask model.Task
	require.NoError(t, db.First(&reloadedTask, task.ID).Error)
	require.Empty(t, reloadedTask.PrivateData.ResultURL)
	require.Empty(t, reloadedTask.PrivateData.ArchiveSource)
	require.JSONEq(t, `{"id":"upstream-video","status":"completed"}`, string(reloadedTask.Data))
}

func TestVideoAssetPipelineArchiveIsIdempotent(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0o600))
	digest := strings.Repeat("a", 64)
	fetcher := &staticVideoArchiveFetcher{path: sourcePath, mimeType: "video/mp4", sha256: digest}
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/output/source.mp4",
		ArchiveSourceURL: "https://media.example/output.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload), Topic: VideoOutboxTopicArchive}

	require.NoError(t, pipeline.HandleArchive(context.Background(), event))
	require.NoError(t, pipeline.HandleArchive(context.Background(), event))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Empty(t, asset.ArchiveSourceURL)
	require.Equal(t, digest, asset.SHA256)
	require.Equal(t, 1, store.putCount[asset.ObjectKey])

	var inspectEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&inspectEvents).Error)
	require.EqualValues(t, 1, inspectEvents)
}

func TestVideoAssetPipelineArchiveRecoversR2WriteBeforeDatabaseFailure(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0o600))
	digest := strings.Repeat("b", 64)
	fetcher := &staticVideoArchiveFetcher{path: sourcePath, mimeType: "video/mp4", sha256: digest}
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/recovery/source.mp4",
		ArchiveSourceURL: "https://media.example/output.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload), Topic: VideoOutboxTopicArchive}

	failFirstUpdate := true
	callbackName := "test:fail_video_archive_database_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if failFirstUpdate && tx.Statement.Table == (model.KKAIVideoAsset{}).TableName() {
			failFirstUpdate = false
			tx.AddError(errors.New("forced archive database failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	require.Error(t, pipeline.HandleArchive(context.Background(), event))
	require.Equal(t, 1, fetcher.fetches)
	require.Equal(t, digest, store.sha256[asset.ObjectKey])
	expectedArchiveSourceSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(asset.ArchiveSourceURL)))
	require.Equal(t, expectedArchiveSourceSHA256, store.archiveSourceSHA256[asset.ObjectKey])
	require.NoError(t, pipeline.HandleArchive(context.Background(), event))
	require.Equal(t, 1, fetcher.fetches)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Empty(t, asset.ArchiveSourceURL)
	require.Equal(t, digest, asset.SHA256)
}

func TestVideoAssetPipelineArchiveRejectsExistingObjectWithoutMatchingSourceFingerprint(t *testing.T) {
	tests := []struct {
		name                      string
		existingSourceFingerprint string
	}{
		{name: "missing source fingerprint"},
		{
			name:                      "different source fingerprint",
			existingSourceFingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte("https://media.example/old.mp4"))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			store := newMemoryVideoAssetStore()
			tempDir := t.TempDir()
			sourcePath := filepath.Join(tempDir, "source.mp4")
			require.NoError(t, os.WriteFile(sourcePath, []byte("fresh"), 0o600))
			digest := strings.Repeat("c", 64)
			fetcher := &staticVideoArchiveFetcher{path: sourcePath, mimeType: "video/mp4", sha256: digest}
			pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
			require.NoError(t, err)
			asset := model.KKAIVideoAsset{
				OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
				State: model.VideoAssetStateProcessing, ObjectKey: "users/9/source-change/output.mp4",
				ArchiveSourceURL: "  https://media.example/current.mp4  ", MIMEType: "video/mp4",
				CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
			}
			require.NoError(t, db.Create(&asset).Error)
			store.objects[asset.ObjectKey] = []byte("stale")
			store.contentType[asset.ObjectKey] = "video/mp4"
			store.sha256[asset.ObjectKey] = strings.Repeat("d", 64)
			store.archiveSourceSHA256[asset.ObjectKey] = tt.existingSourceFingerprint
			payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
			require.NoError(t, err)

			require.NoError(t, pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{
				Payload: string(payload), Topic: VideoOutboxTopicArchive,
			}))

			require.Equal(t, 1, fetcher.fetches)
			require.Equal(t, 1, store.deleteCount[asset.ObjectKey])
			require.Equal(t, []byte("fresh"), store.objects[asset.ObjectKey])
			expectedSourceFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte("https://media.example/current.mp4")))
			require.Equal(t, expectedSourceFingerprint, store.archiveSourceSHA256[asset.ObjectKey])
		})
	}
}

func TestVideoAssetPipelineArchiveDoesNotFetchUntilMismatchedObjectIsDeleted(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	memoryStore := newMemoryVideoAssetStore()
	store := &callbackDeleteVideoAssetStore{memoryVideoAssetStore: memoryStore}
	deleteFailure := errors.New("forced stale archive deletion failure")
	deleteCalls := 0
	store.deleteObjects = func(ctx context.Context, keys []string) error {
		deleteCalls++
		if deleteCalls == 1 {
			return deleteFailure
		}
		return memoryStore.Delete(ctx, keys)
	}
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("fresh"), 0o600))
	digest := strings.Repeat("c", 64)
	fetcher := &staticVideoArchiveFetcher{path: sourcePath, mimeType: "video/mp4", sha256: digest}
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/delete-retry/output.mp4",
		ArchiveSourceURL: "https://media.example/current.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	memoryStore.objects[asset.ObjectKey] = []byte("stale")
	memoryStore.contentType[asset.ObjectKey] = "video/mp4"
	memoryStore.sha256[asset.ObjectKey] = strings.Repeat("d", 64)
	memoryStore.archiveSourceSHA256[asset.ObjectKey] = fmt.Sprintf(
		"%x", sha256.Sum256([]byte("https://media.example/old.mp4")),
	)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload), Topic: VideoOutboxTopicArchive}

	err = pipeline.HandleArchive(context.Background(), event)
	require.ErrorIs(t, err, deleteFailure)
	require.Equal(t, 1, deleteCalls)
	require.Equal(t, 0, fetcher.fetches)
	require.Equal(t, 0, memoryStore.putCount[asset.ObjectKey])
	require.Equal(t, []byte("stale"), memoryStore.objects[asset.ObjectKey])
	var afterFailedDelete model.KKAIVideoAsset
	require.NoError(t, db.First(&afterFailedDelete, asset.ID).Error)
	require.Equal(t, asset.ArchiveSourceURL, afterFailedDelete.ArchiveSourceURL)
	require.Empty(t, afterFailedDelete.SHA256)
	var inspectEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&inspectEvents).Error)
	require.Zero(t, inspectEvents)

	require.NoError(t, pipeline.HandleArchive(context.Background(), event))
	require.Equal(t, 2, deleteCalls)
	require.Equal(t, 1, memoryStore.deleteCount[asset.ObjectKey])
	require.Equal(t, 1, fetcher.fetches)
	require.Equal(t, 1, memoryStore.putCount[asset.ObjectKey])
	require.Equal(t, []byte("fresh"), memoryStore.objects[asset.ObjectKey])
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Empty(t, asset.ArchiveSourceURL)
	require.Equal(t, digest, asset.SHA256)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&inspectEvents).Error)
	require.EqualValues(t, 1, inspectEvents)
}

func TestVideoAssetPipelineArchiveFetchDeadlineReleasesNextEvent(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0o600))
	previousRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() { common.RelayTimeout = previousRelayTimeout })

	fetches := 0
	fetcher := callbackVideoArchiveFetcher(func(ctx context.Context, _ string, _ int64) (*FetchedVideoArchive, error) {
		fetches++
		deadline, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		require.Greater(t, time.Until(deadline), time.Duration(0))
		if fetches == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &FetchedVideoArchive{
			Path: sourcePath, MIMEType: "video/mp4", SizeBytes: 5, SHA256: strings.Repeat("d", 64),
		}, nil
	})
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	pipeline.archiveFetchTimeout = 25 * time.Millisecond
	pipeline.archivePutTimeout = time.Second

	assets := []model.KKAIVideoAsset{
		{
			OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
			State: model.VideoAssetStateProcessing, ObjectKey: "users/9/timeout/first.mp4",
			ArchiveSourceURL: "https://media.example/first.mp4", MIMEType: "video/mp4",
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		},
		{
			OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
			State: model.VideoAssetStateProcessing, ObjectKey: "users/9/timeout/second.mp4",
			ArchiveSourceURL: "https://media.example/second.mp4", MIMEType: "video/mp4",
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	firstPayload, err := common.Marshal(VideoAssetEventPayload{AssetID: assets[0].ID})
	require.NoError(t, err)
	secondPayload, err := common.Marshal(VideoAssetEventPayload{AssetID: assets[1].ID})
	require.NoError(t, err)

	startedAt := time.Now()
	err = pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{Payload: string(firstPayload)})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(startedAt), time.Second)
	require.NoError(t, pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{Payload: string(secondPayload)}))
	require.Equal(t, 2, fetches)
	require.Equal(t, 1, store.putCount[assets[1].ObjectKey])
}

func TestVideoAssetPipelineArchivePutHasIndependentHardDeadline(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0o600))
	putDeadlineRemaining := make(chan time.Duration, 1)
	store := &callbackArchiveVideoAssetStore{
		memoryVideoAssetStore: newMemoryVideoAssetStore(),
		putArchive: func(ctx context.Context, _ string, _ string, _ io.Reader, _ int64, _ string, _ string) error {
			deadline, hasDeadline := ctx.Deadline()
			require.True(t, hasDeadline)
			putDeadlineRemaining <- time.Until(deadline)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	fetcher := callbackVideoArchiveFetcher(func(ctx context.Context, _ string, _ int64) (*FetchedVideoArchive, error) {
		deadline, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		require.Greater(t, time.Until(deadline), 60*time.Millisecond)
		timer := time.NewTimer(60 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &FetchedVideoArchive{
			Path: sourcePath, MIMEType: "video/mp4", SizeBytes: 5, SHA256: strings.Repeat("e", 64),
		}, nil
	})
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	pipeline.archiveFetchTimeout = 100 * time.Millisecond
	pipeline.archivePutTimeout = 120 * time.Millisecond
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/timeout/put.mp4",
		ArchiveSourceURL: "https://media.example/put.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	err = pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Greater(t, <-putDeadlineRemaining, 80*time.Millisecond)
}

func TestVideoAssetPipelineArchiveFetchHonorsParentCancellation(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	started := make(chan struct{})
	fetcher := callbackVideoArchiveFetcher(func(ctx context.Context, _ string, _ int64) (*FetchedVideoArchive, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, t.TempDir())
	require.NoError(t, err)
	pipeline.archiveFetchTimeout = time.Second
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/cancel/source.mp4",
		ArchiveSourceURL: "https://media.example/cancel.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- pipeline.HandleArchive(ctx, model.KKAIOutboxEvent{Payload: string(payload)})
	}()
	<-started
	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("archive handler did not return after cancellation")
	}
}

func TestVideoAssetPipelineArchiveDoesNotReviveDeletingAsset(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(sourcePath, []byte("video"), 0o600))
	digest := strings.Repeat("c", 64)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "users/9/race/source.mp4",
		ArchiveSourceURL: "https://media.example/output.mp4", MIMEType: "video/mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	fetcher := callbackVideoArchiveFetcher(func(context.Context, string, int64) (*FetchedVideoArchive, error) {
		require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).
			Update("state", model.VideoAssetStateDeleting).Error)
		return &FetchedVideoArchive{
			Path: sourcePath, MIMEType: "video/mp4", SizeBytes: 5, SHA256: digest,
		}, nil
	})
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, fetcher, tempDir)
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)
	require.NotContains(t, store.objects, asset.ObjectKey)
}

func TestVideoAssetPipelineInspectDoesNotReviveDeletingVideo(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["source.mp4"] = []byte("video")
	store.contentType["source.mp4"] = "video/mp4"
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "source.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	media := callbackVideoMediaProcessor{inspect: func(context.Context, string) (VideoMediaMetadata, error) {
		require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).
			Update("state", model.VideoAssetStateDeleting).Error)
		return staticVideoMediaProcessor{}.Inspect(context.Background(), "")
	}}
	pipeline, err := NewVideoAssetPipeline(db, store, media, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandleInspect(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)
	var posterEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicPoster).Count(&posterEvents).Error)
	require.Zero(t, posterEvents)
}

func TestVideoAssetPipelineInspectFailureDoesNotReviveDeletingVideo(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["source.mp4"] = []byte("video")
	store.contentType["source.mp4"] = "video/mp4"
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "source.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, ArchiveSourceURL: "https://media.example/output.mp4",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	media := callbackVideoMediaProcessor{inspect: func(context.Context, string) (VideoMediaMetadata, error) {
		require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).
			Update("state", model.VideoAssetStateDeleting).Error)
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}}
	pipeline, err := NewVideoAssetPipeline(db, store, media, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	err = pipeline.HandleInspect(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.Error(t, err)
	var permanent permanentKKAIOutboxError
	require.ErrorAs(t, err, &permanent)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)
	require.Empty(t, asset.FailureReason)
	require.Equal(t, "https://media.example/output.mp4", asset.ArchiveSourceURL)
}

func TestVideoAssetPipelinePosterDoesNotReviveDeletingAsset(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["source.mp4"] = []byte("video")
	store.contentType["source.mp4"] = "video/mp4"
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "source.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	media := callbackVideoMediaProcessor{createPoster: func(_ context.Context, _ string, output string, _ int64) error {
		require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).
			Update("state", model.VideoAssetStateDeleting).Error)
		return os.WriteFile(output, []byte("poster"), 0o600)
	}}
	pipeline, err := NewVideoAssetPipeline(db, store, media, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandlePoster(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)
	require.NotContains(t, store.objects, videoAssetDerivedPath(asset.ObjectKey, ".poster.jpg"))
}

func TestVideoAssetPipelineDeleteIsIdempotent(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["source.mp4"] = []byte("video")
	store.objects["poster.jpg"] = []byte("poster")
	store.objects["preview.mp4"] = []byte("preview")
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateDeleting, ObjectKey: "source.mp4", PosterObjectKey: "poster.jpg", PreviewObjectKey: "preview.mp4",
		MIMEType: "video/mp4", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload), Topic: VideoOutboxTopicDelete}

	require.NoError(t, pipeline.HandleDelete(context.Background(), event))
	require.NoError(t, pipeline.HandleDelete(context.Background(), event))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.Positive(t, asset.DeletedAt)
	require.Equal(t, 1, store.deleteCount["source.mp4"])
	require.Equal(t, 1, store.deleteCount["poster.jpg"])
	require.Equal(t, 1, store.deleteCount["preview.mp4"])
}

func TestVideoAssetPipelineDeleteRechecksReferenceUseBeforeRemovingR2Object(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["reference.png"] = []byte("image")
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateDeleting, ObjectKey: "reference.png", MIMEType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	activeTask := model.Task{
		TaskID: "delete-recheck-active-reference", UserId: 9, Status: model.TaskStatusInProgress,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&activeTask).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: activeTask.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleReference, CreatedAt: now,
	}).Error)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	err = pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{
		Payload: string(payload), Topic: VideoOutboxTopicDelete,
	})
	var deferred deferredKKAIOutboxError
	require.ErrorAs(t, err, &deferred)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)
	require.Equal(t, []byte("image"), store.objects[asset.ObjectKey])
	require.Zero(t, store.deleteCount[asset.ObjectKey])

	require.NoError(t, db.Model(&activeTask).Update("status", model.TaskStatusSuccess).Error)
	require.NoError(t, pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{
		Payload: string(payload), Topic: VideoOutboxTopicDelete,
	}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.NotContains(t, store.objects, asset.ObjectKey)
}

func TestVideoAssetPipelineDeleteDefersWhileCatalogSampleUsesAsset(t *testing.T) {
	for _, status := range []string{model.VideoSampleStatusDraft, model.VideoSampleStatusPublished} {
		t.Run(status, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			store := newMemoryVideoAssetStore()
			store.objects["catalog/in-use-sample.mp4"] = []byte("video")
			pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir())
			require.NoError(t, err)
			now := time.Now().Unix()
			asset := model.KKAIVideoAsset{
				OwnerUserID: 9, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
				State: model.VideoAssetStateDeleting, ObjectKey: "catalog/in-use-sample.mp4", MIMEType: "video/mp4",
				CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&asset).Error)
			require.NoError(t, db.Create(&model.KKAIVideoSample{
				ModelProfileID: 1, Title: "In-use sample", Prompt: "prompt", Mode: VideoModeTextToVideo,
				ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: asset.ID,
				AspectRatio: 1, Status: status, CreatedAt: now, UpdatedAt: now,
			}).Error)
			payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
			require.NoError(t, err)

			err = pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{
				Payload: string(payload), Topic: VideoOutboxTopicDelete,
			})
			var deferred deferredKKAIOutboxError
			require.ErrorAs(t, err, &deferred)
			require.NoError(t, db.First(&asset, asset.ID).Error)
			require.Equal(t, model.VideoAssetStateDeleting, asset.State)
			require.Contains(t, store.objects, asset.ObjectKey)
			require.Zero(t, store.deleteCount[asset.ObjectKey])
		})
	}
}

func TestVideoAssetPipelineSamplePrepareCreatesPreviewWithoutAnotherTopic(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["sample.mp4"] = []byte("video")
	store.contentType["sample.mp4"] = "video/mp4"
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady,
		ObjectKey: "sample.mp4", PosterObjectKey: "poster.jpg", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	sample := model.KKAIVideoSample{
		ModelProfileID: 1, Title: "sample", Prompt: "sample", Mode: VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: asset.ID,
		AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&sample).Error)
	require.NoError(t, db.Model(&sample).UpdateColumn("category", nil).Error)
	payload, err := common.Marshal(VideoSamplePrepareEventPayload{SampleID: sample.ID})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandleSamplePrepare(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.NotEmpty(t, asset.PreviewObjectKey)
	require.Equal(t, []byte("preview"), store.objects[asset.PreviewObjectKey])
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)
}

func TestEnqueueVideoSamplePreparationDoesNotCollapseSameSecondUpdates(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, enqueueVideoSamplePreparation(context.Background(), db, 77))
	require.NoError(t, enqueueVideoSamplePreparation(context.Background(), db, 77))

	var events []model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", VideoOutboxTopicSamplePrepare).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	require.NotEqual(t, events[0].EventKey, events[1].EventKey)
	require.Contains(t, events[0].EventKey, ":prepare:v1:")
}

func TestVideoPipelineDefersOnlyReadinessWaits(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, failingVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateProcessing,
		ObjectKey: "sample.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	sample := model.KKAIVideoSample{
		ModelProfileID: 1, Title: "sample", Prompt: "sample", Mode: VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: asset.ID,
		AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&sample).Error)
	samplePayload, err := common.Marshal(VideoSamplePrepareEventPayload{SampleID: sample.ID})
	require.NoError(t, err)
	waitErr := pipeline.HandleSamplePrepare(context.Background(), model.KKAIOutboxEvent{Payload: string(samplePayload)})
	var deferred deferredKKAIOutboxError
	require.ErrorAs(t, waitErr, &deferred)
	expiredWaitErr := pipeline.HandleSamplePrepare(context.Background(), model.KKAIOutboxEvent{
		Payload: string(samplePayload), CreatedAt: time.Now().Add(-11 * time.Minute).Unix(),
	})
	require.Error(t, expiredWaitErr)
	require.False(t, errors.As(expiredWaitErr, &deferred))

	archiveAsset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateProcessing, ObjectKey: "output.mp4", ArchiveSourceURL: "https://media.example/output.mp4",
		MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&archiveAsset).Error)
	archivePayload, err := common.Marshal(VideoAssetEventPayload{AssetID: archiveAsset.ID})
	require.NoError(t, err)
	archiveErr := pipeline.HandleArchive(context.Background(), model.KKAIOutboxEvent{Payload: string(archivePayload)})
	require.Error(t, archiveErr)
	require.False(t, errors.As(archiveErr, &deferred))
	var permanent permanentKKAIOutboxError
	require.False(t, errors.As(archiveErr, &permanent))
}

type failingVideoArchiveFetcher struct{}

func (failingVideoArchiveFetcher) Fetch(context.Context, string, int64) (*FetchedVideoArchive, error) {
	return nil, errors.New("R2 source unavailable")
}

func TestNewVideoOutboxWorkerUsesTwoThirtySecondProcessors(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	worker, err := NewVideoOutboxWorker(
		db, "worker-a", store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir(), 2,
	)
	require.NoError(t, err)
	require.Len(t, worker.processors, 2)
	for _, processor := range worker.processors {
		require.Equal(t, 30*time.Second, processor.lockTimeout)
		require.Len(t, processor.registeredTopics(), 5)
	}
}

func TestVideoOutboxWorkerAdvancesUploadedAssetToReady(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["reference.mp4"] = []byte("video")
	store.contentType["reference.mp4"] = "video/mp4"
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateUploaded, ObjectKey: "reference.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, EnqueueVideoOutboxEvent(
		context.Background(), db, fmt.Sprintf("video:asset:%d:inspect:v1", asset.ID),
		VideoOutboxTopicInspect, fmt.Sprintf("%d", asset.ID), VideoAssetEventPayload{AssetID: asset.ID},
	))
	worker, err := NewVideoOutboxWorker(
		db, "worker-lifecycle", store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir(), 1,
	)
	require.NoError(t, err)

	require.NoError(t, worker.ProcessOnce(context.Background()))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateProcessing, asset.State)

	require.NoError(t, worker.ProcessOnce(context.Background()))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateReady, asset.State)
	require.NotEmpty(t, asset.PosterObjectKey)
}

func TestVideoOutboxDeadLetterConvergesAggregateAndRedriveReusesEvent(t *testing.T) {
	tests := []struct {
		name             string
		topic            string
		initialState     string
		retryState       string
		preparePayload   func(*testing.T, *gorm.DB, *model.KKAIVideoAsset) string
		assertGeneration bool
	}{
		{
			name: "archive", topic: VideoOutboxTopicArchive,
			initialState: model.VideoAssetStateProcessing, retryState: model.VideoAssetStateProcessing,
			preparePayload:   videoAssetDeadLetterPayload,
			assertGeneration: true,
		},
		{
			name: "inspect", topic: VideoOutboxTopicInspect,
			initialState: model.VideoAssetStateProcessing, retryState: model.VideoAssetStateProcessing,
			preparePayload:   videoAssetDeadLetterPayload,
			assertGeneration: true,
		},
		{
			name: "poster", topic: VideoOutboxTopicPoster,
			initialState: model.VideoAssetStateProcessing, retryState: model.VideoAssetStateProcessing,
			preparePayload:   videoAssetDeadLetterPayload,
			assertGeneration: true,
		},
		{
			name: "delete", topic: VideoOutboxTopicDelete,
			initialState: model.VideoAssetStateDeleting, retryState: model.VideoAssetStateDeleting,
			preparePayload:   videoAssetDeadLetterPayload,
			assertGeneration: true,
		},
		{
			name: "sample preview", topic: VideoOutboxTopicSamplePrepare,
			initialState: model.VideoAssetStateReady, retryState: model.VideoAssetStateReady,
			preparePayload: func(t *testing.T, db *gorm.DB, asset *model.KKAIVideoAsset) string {
				t.Helper()
				sample := model.KKAIVideoSample{
					ModelProfileID: 1, Title: "sample", Prompt: "sample", Mode: VideoModeTextToVideo,
					ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: asset.ID,
					AspectRatio: 1, Status: model.VideoSampleStatusDraft,
					CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt,
				}
				require.NoError(t, db.Create(&sample).Error)
				payload, err := common.Marshal(VideoSamplePrepareEventPayload{SampleID: sample.ID})
				require.NoError(t, err)
				return string(payload)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			now := time.Unix(1_720_000_000, 0)
			asset := model.KKAIVideoAsset{
				OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
				State: tt.initialState, ObjectKey: "video/asset.mp4", MIMEType: "video/mp4",
				ArchiveSourceURL: "https://media.example/output.mp4", PosterObjectKey: "poster.jpg",
				CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
			}
			if tt.topic == VideoOutboxTopicSamplePrepare {
				asset.Scope = model.VideoAssetScopeCatalog
				asset.Kind = model.VideoAssetKindSample
			}
			require.NoError(t, db.Create(&asset).Error)

			var task model.Task
			var generation model.KKAIVideoGeneration
			if tt.assertGeneration {
				task = model.Task{
					TaskID: "task_" + tt.name, UserId: asset.OwnerUserID, Status: model.TaskStatusSuccess,
					Progress: "100%", Quota: 321, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
					PrivateData: model.TaskPrivateData{
						AssetHostedResult: true, BillingState: model.TaskBillingStateCompleted, TokenQuota: 321,
					},
				}
				require.NoError(t, db.Create(&task).Error)
				generation = model.KKAIVideoGeneration{
					UserID: task.UserId, TaskID: task.ID, ModelProfileID: 1, Model: "video-model",
					Mode: VideoModeTextToVideo, Prompt: "test", Parameters: `{}`,
					CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
				}
				require.NoError(t, db.Create(&generation).Error)
				require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
					TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput,
					Position: 0, CreatedAt: now.Unix(),
				}).Error)
			}

			payload := tt.preparePayload(t, db, &asset)
			event := model.KKAIOutboxEvent{
				EventKey: "dead-letter-" + tt.name, Topic: tt.topic,
				AggregateID: fmt.Sprintf("%d", asset.ID), Payload: payload,
				Status: model.KKAIOutboxStatusPending, AvailableAt: now.Unix(), CreatedAt: now.Unix(),
			}
			require.NoError(t, db.Create(&event).Error)

			store := newMemoryVideoAssetStore()
			pipeline, err := NewVideoAssetPipeline(
				db, store, staticVideoMediaProcessor{},
				failingVideoArchiveFetcher{}, t.TempDir(),
			)
			require.NoError(t, err)
			processor := NewKKAIOutboxProcessor(db, "video-dead-letter-test")
			processor.now = func() time.Time { return now }
			processor.maxAttempts = 1
			require.NoError(t, pipeline.Register(processor))
			require.NoError(t, processor.Register(tt.topic, func(context.Context, model.KKAIOutboxEvent) error {
				return errors.New("transient video dependency failure")
			}))

			result, err := processor.ProcessBatch(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, result.Dead)
			require.NoError(t, db.First(&event, event.ID).Error)
			require.Equal(t, model.KKAIOutboxStatusDead, event.Status)
			require.NoError(t, db.First(&asset, asset.ID).Error)
			require.Equal(t, model.VideoAssetStateFailed, asset.State)
			require.NotEmpty(t, asset.FailureReason)

			if tt.assertGeneration {
				view, err := GetVideoGeneration(context.Background(), db, task.UserId, generation.ID)
				require.NoError(t, err)
				require.Equal(t, "failed", view.Status)
				var unchangedTask model.Task
				require.NoError(t, db.First(&unchangedTask, task.ID).Error)
				require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), unchangedTask.Status)
				require.Equal(t, 321, unchangedTask.Quota)
				require.Equal(t, model.TaskBillingStateCompleted, unchangedTask.PrivateData.BillingState)
				require.Equal(t, 321, unchangedTask.PrivateData.TokenQuota)
			}

			redriven, applied, err := RedriveVideoOutboxDeadEvent(
				context.Background(), db, event.ID, "operator-retry-1", "admin:42", now.Add(time.Minute),
			)
			require.NoError(t, err)
			require.True(t, applied)
			require.Equal(t, event.ID, redriven.ID)
			require.Equal(t, model.KKAIOutboxStatusPending, redriven.Status)
			require.Zero(t, redriven.Attempts)
			require.NoError(t, db.First(&asset, asset.ID).Error)
			require.Equal(t, tt.retryState, asset.State)
			require.Empty(t, asset.FailureReason)

			var eventCount int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&eventCount).Error)
			require.EqualValues(t, 1, eventCount)
			require.Empty(t, store.objects)
			require.Empty(t, store.putCount)
			_, applied, err = RedriveVideoOutboxDeadEvent(
				context.Background(), db, event.ID, "operator-retry-1", "admin:42", now.Add(time.Minute),
			)
			require.NoError(t, err)
			require.False(t, applied)
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&eventCount).Error)
			require.EqualValues(t, 1, eventCount)

			handled := 0
			require.NoError(t, processor.Register(tt.topic, func(context.Context, model.KKAIOutboxEvent) error {
				handled++
				return nil
			}))
			processor.now = func() time.Time { return now.Add(time.Minute) }
			result, err = processor.ProcessBatch(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, result.Delivered)
			require.Equal(t, 1, handled)
			require.NoError(t, db.First(&event, event.ID).Error)
			require.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
		})
	}
}

func videoAssetDeadLetterPayload(t *testing.T, _ *gorm.DB, asset *model.KKAIVideoAsset) string {
	t.Helper()
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	return string(payload)
}

func TestRedriveVideoOutboxDeadEventRejectsNonVideoTopic(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := model.KKAIOutboxEvent{
		EventKey: "non-video-dead-letter", Topic: model.KKAIOutboxTopicTaskBillingAudit,
		AggregateID: "42", Payload: `{}`, Status: model.KKAIOutboxStatusDead,
		Attempts: 12, AvailableAt: now.Unix(), LastError: "billing audit failed", CreatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&event).Error)

	_, applied, err := RedriveVideoOutboxDeadEvent(
		context.Background(), db, event.ID, "video-admin-retry", "admin:42", now,
	)
	require.ErrorIs(t, err, ErrVideoOutboxEventNotFound)
	require.False(t, applied)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDead, event.Status)
}
