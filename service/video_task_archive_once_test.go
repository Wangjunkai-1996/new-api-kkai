package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	settingconfig "github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type exactTaskVideoArchiveFetcher struct {
	*staticVideoArchiveFetcher
}

type countingExactTaskVideoAssetStore struct {
	*memoryVideoAssetStore
	calls      int
	writeCalls int
}

func newCountingExactTaskVideoAssetStore() *countingExactTaskVideoAssetStore {
	return &countingExactTaskVideoAssetStore{memoryVideoAssetStore: newMemoryVideoAssetStore()}
}

func (store *countingExactTaskVideoAssetStore) PresignUpload(
	ctx context.Context,
	key string,
	contentType string,
	sizeBytes int64,
	expires time.Duration,
) (VideoAssetSignedRequest, error) {
	store.calls++
	return store.memoryVideoAssetStore.PresignUpload(ctx, key, contentType, sizeBytes, expires)
}

func (store *countingExactTaskVideoAssetStore) PresignDownload(
	ctx context.Context,
	key string,
	filename string,
	attachment bool,
	expires time.Duration,
) (string, error) {
	store.calls++
	return store.memoryVideoAssetStore.PresignDownload(ctx, key, filename, attachment, expires)
}

func (store *countingExactTaskVideoAssetStore) Head(
	ctx context.Context,
	key string,
) (VideoAssetObjectMetadata, error) {
	store.calls++
	return store.memoryVideoAssetStore.Head(ctx, key)
}

func (store *countingExactTaskVideoAssetStore) Get(
	ctx context.Context,
	key string,
) (VideoAssetObject, error) {
	store.calls++
	return store.memoryVideoAssetStore.Get(ctx, key)
}

func (store *countingExactTaskVideoAssetStore) Put(
	ctx context.Context,
	key string,
	contentType string,
	reader io.Reader,
	length int64,
) error {
	store.calls++
	store.writeCalls++
	return store.memoryVideoAssetStore.Put(ctx, key, contentType, reader, length)
}

func (store *countingExactTaskVideoAssetStore) PutArchive(
	ctx context.Context,
	key string,
	contentType string,
	reader io.Reader,
	length int64,
	digest string,
	archiveSourceDigest string,
) error {
	store.calls++
	store.writeCalls++
	return store.memoryVideoAssetStore.PutArchive(
		ctx, key, contentType, reader, length, digest, archiveSourceDigest,
	)
}

func (store *countingExactTaskVideoAssetStore) Delete(ctx context.Context, keys []string) error {
	store.calls++
	store.writeCalls++
	return store.memoryVideoAssetStore.Delete(ctx, keys)
}

func (fetcher *exactTaskVideoArchiveFetcher) FetchTaskSource(
	ctx context.Context,
	_ videoArchiveTaskSource,
	maxBytes int64,
) (*FetchedVideoArchive, error) {
	return fetcher.Fetch(ctx, "", maxBytes)
}

func seedVideoTaskArchiveOnceFixture(t *testing.T, db *gorm.DB) VideoTaskArchiveOnceInput {
	t.Helper()
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Channel{Id: 60, Key: "test-key", Name: "exact-archive"}).Error)
	task := model.Task{
		ID: 101, TaskID: "task_exact_archive", UserId: 1,
		ChannelId: 60,
		Status:    model.TaskStatusSuccess, Progress: "100%", CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{ResultURL: "https://media.example/generated.mp4"},
	}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		ID: 3, UserID: 1, TaskID: task.ID, ModelProfileID: 1,
		Model: "sd_2.0_fast_special_720p", Mode: "text", Prompt: "fixture",
		Parameters: "{}", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return VideoTaskArchiveOnceInput{
		TaskID: task.ID, GenerationID: generation.ID, ExpectedUserID: task.UserId,
		ExpectedExternalTaskID: task.TaskID,
	}
}

func newVideoTaskArchiveOnceExecutorForTest(
	t *testing.T,
	db *gorm.DB,
) (*VideoTaskArchiveOnceExecutor, *countingExactTaskVideoAssetStore, *exactTaskVideoArchiveFetcher) {
	t.Helper()
	videoPath := fmt.Sprintf("%s/video.mp4", t.TempDir())
	videoContent := []byte("video-content")
	require.NoError(t, os.WriteFile(videoPath, videoContent, 0o600))
	digest := sha256.Sum256(videoContent)
	fetcher := &exactTaskVideoArchiveFetcher{staticVideoArchiveFetcher: &staticVideoArchiveFetcher{
		path: videoPath, mimeType: "video/mp4", sha256: fmt.Sprintf("%x", digest),
	}}
	store := newCountingExactTaskVideoAssetStore()
	executor, err := NewVideoTaskArchiveOnceExecutor(
		db, store, staticVideoMediaProcessor{}, fetcher, t.TempDir(), "video-archive-once-test",
	)
	require.NoError(t, err)
	return executor, store, fetcher
}

func reconcileVideoTaskArchiveOnceFixture(
	t *testing.T,
	db *gorm.DB,
	input VideoTaskArchiveOnceInput,
) (model.KKAIVideoAsset, model.KKAIOutboxEvent) {
	t.Helper()
	created, err := reconcileVideoTaskOutputExpected(context.Background(), db, input.GenerationID, &input)
	require.NoError(t, err)
	require.True(t, created)

	var link model.KKAIVideoTaskAsset
	require.NoError(t, db.First(
		&link, "task_id = ? AND role = ? AND position = ?",
		input.TaskID, model.VideoTaskAssetRoleOutput, 0,
	).Error)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, link.AssetID).Error)
	var event model.KKAIOutboxEvent
	require.NoError(t, db.First(
		&event, "event_key = ?", fmt.Sprintf("video:task:%d:archive:v1", input.TaskID),
	).Error)
	return asset, event
}

func createVideoTaskArchiveOnceEvent(
	t *testing.T,
	db *gorm.DB,
	key string,
	topic string,
	assetID int64,
) model.KKAIOutboxEvent {
	t.Helper()
	require.NoError(t, EnqueueVideoOutboxEvent(
		context.Background(), db, key, topic, strconv.FormatInt(assetID, 10),
		VideoAssetEventPayload{AssetID: assetID},
	))
	var event model.KKAIOutboxEvent
	require.NoError(t, db.First(&event, "event_key = ?", key).Error)
	return event
}

func requireVideoTaskArchiveOnceEventUnchanged(
	t *testing.T,
	db *gorm.DB,
	expected model.KKAIOutboxEvent,
) {
	t.Helper()
	var actual model.KKAIOutboxEvent
	require.NoError(t, db.First(&actual, expected.ID).Error)
	require.Equal(t, expected, actual)
}

func TestPreviewVideoTaskArchiveOnceDoesNotWrite(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)

	result, err := PreviewVideoTaskArchiveOnce(context.Background(), db, input)
	require.NoError(t, err)
	require.Equal(t, VideoTaskArchiveOnceStageAwaitingArchive, result.Stage)
	require.EqualValues(t, 101, result.TaskID)
	require.EqualValues(t, 3, result.GenerationID)
	require.Zero(t, result.AssetID)

	var assets int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.Zero(t, assets)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)
}

func TestPreviewVideoTaskArchiveOnceRequiresBackgroundJobsDisabled(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	original := video_studio_setting.Get()
	t.Cleanup(func() {
		require.NoError(t, settingconfig.GlobalConfig.LoadFromDB(map[string]string{
			"video_studio.archive_enqueue_enabled": strconv.FormatBool(original.ArchiveEnqueueEnabled),
			"video_studio.backfill_enabled":        strconv.FormatBool(original.BackfillEnabled),
			"video_studio.worker_enabled":          strconv.FormatBool(original.WorkerEnabled),
		}))
	})
	require.NoError(t, settingconfig.GlobalConfig.LoadFromDB(map[string]string{
		"video_studio.archive_enqueue_enabled": "false",
		"video_studio.backfill_enabled":        "false",
		"video_studio.worker_enabled":          "true",
	}))

	_, err := PreviewVideoTaskArchiveOnce(context.Background(), db, input)
	require.ErrorIs(t, err, ErrVideoTaskArchiveOnceBlocked)
	var assets int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.Zero(t, assets)
}

func TestVideoTaskArchiveOnceProcessesOnlyExactTaskAndIsIdempotent(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	payload := fmt.Sprintf(`{"asset_id":%d}`, 999)
	unrelated := model.KKAIOutboxEvent{
		EventKey: "video:task:999:archive:v1", Topic: VideoOutboxTopicArchive,
		AggregateID: "999", Payload: payload, Status: model.KKAIOutboxStatusPending,
		AvailableAt: time.Now().Unix(), CreatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&unrelated).Error)
	executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)

	result, err := executor.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, VideoTaskArchiveOnceStageReady, result.Stage)
	require.Positive(t, result.AssetID)
	require.Equal(t, 1, fetcher.fetches)
	require.Equal(t, 1, store.putCount[fmt.Sprintf("users/1/generations/3/source.mp4")])
	require.Equal(t, 1, store.putCount[fmt.Sprintf("users/1/generations/3/source.poster.jpg")])

	var persistedUnrelated model.KKAIOutboxEvent
	require.NoError(t, db.First(&persistedUnrelated, unrelated.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, persistedUnrelated.Status)
	require.Empty(t, persistedUnrelated.LockedBy)

	second, err := executor.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, VideoTaskArchiveOnceStageReady, second.Stage)
	require.Equal(t, result.AssetID, second.AssetID)
	require.Equal(t, 1, fetcher.fetches)
	require.Equal(t, 1, store.putCount[fmt.Sprintf("users/1/generations/3/source.mp4")])
	require.Equal(t, 1, store.putCount[fmt.Sprintf("users/1/generations/3/source.poster.jpg")])

	for _, eventKey := range []string{
		"video:task:101:archive:v1",
		"video:asset:" + strconv.FormatInt(result.AssetID, 10) + ":inspect:v1",
		"video:asset:" + strconv.FormatInt(result.AssetID, 10) + ":poster:v1",
	} {
		var event model.KKAIOutboxEvent
		require.NoError(t, db.First(&event, "event_key = ?", eventKey).Error)
		require.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
	}
}

func TestStandardVideoWorkerIgnoresExactArchiveEvents(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	_, event := reconcileVideoTaskArchiveOnceFixture(t, db, input)
	executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)
	worker, err := NewVideoOutboxWorker(
		db, "standard-worker", store, staticVideoMediaProcessor{}, fetcher, executor.tempDir, 1,
	)
	require.NoError(t, err)

	require.NoError(t, worker.ProcessOnce(context.Background()))
	require.Zero(t, fetcher.fetches)
	require.Zero(t, store.calls)
	requireVideoTaskArchiveOnceEventUnchanged(t, db, event)
}

func TestExactClaimRevalidatesCoordinatesBeforeHandler(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	asset, event := reconcileVideoTaskArchiveOnceFixture(t, db, input)
	executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)
	result := VideoTaskArchiveOnceResult{
		TaskID: input.TaskID, GenerationID: input.GenerationID, AssetID: asset.ID,
		Stage: VideoTaskArchiveOnceStageAwaitingArchive,
	}
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).
		Update("owner_user_id", 999).Error)

	err := executor.processor.processExactVideoTaskArchiveEvent(
		context.Background(), input, videoTaskArchiveOnceEventForStage(result),
	)
	require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
	require.Zero(t, fetcher.fetches)
	require.Zero(t, store.calls)
	requireVideoTaskArchiveOnceEventUnchanged(t, db, event)
}

func TestVideoTaskArchiveOnceRejectsMismatchedTaskPairWithoutWrites(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	input.ExpectedExternalTaskID = "task_wrong"
	executor, _, _ := newVideoTaskArchiveOnceExecutorForTest(t, db)

	_, err := executor.Execute(context.Background(), input)
	require.ErrorIs(t, err, ErrVideoTaskArchiveOnceMismatch)

	var assets int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.Zero(t, assets)
	var task model.Task
	require.NoError(t, db.First(&task, 101).Error)
	require.False(t, task.PrivateData.AssetHostedResult)
	require.Empty(t, task.PrivateData.ArchiveSource)
}

func TestVideoTaskArchiveOnceRejectsMalformedExactEventBeforeStoreAccess(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*model.KKAIOutboxEvent)
	}{
		{
			name: "wrong topic",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.Topic = VideoOutboxTopicInspect
			},
		},
		{
			name: "wrong aggregate",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.AggregateID = "999"
			},
		},
		{
			name: "wrong payload",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.Payload = `{"asset_id":999}`
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			input := seedVideoTaskArchiveOnceFixture(t, db)
			_, event := reconcileVideoTaskArchiveOnceFixture(t, db, input)
			testCase.mutate(&event)
			require.NoError(t, db.Save(&event).Error)
			executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)

			_, err := executor.Execute(context.Background(), input)
			require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
			require.Zero(t, fetcher.fetches)
			require.Zero(t, store.calls)
			require.Zero(t, store.writeCalls)
			requireVideoTaskArchiveOnceEventUnchanged(t, db, event)
		})
	}
}

func TestVideoTaskArchiveOnceLeavesBlockedExactEventUntouched(t *testing.T) {
	fixedNow := time.Unix(2_000_000_000, 0)
	testCases := []struct {
		name   string
		mutate func(*model.KKAIOutboxEvent)
	}{
		{
			name: "dead",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.Status = model.KKAIOutboxStatusDead
				event.LastError = "operator review required"
			},
		},
		{
			name: "future retry",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.Attempts = 1
				event.AvailableAt = fixedNow.Add(time.Minute).Unix()
				event.LastError = "retry scheduled"
			},
		},
		{
			name: "live lock",
			mutate: func(event *model.KKAIOutboxEvent) {
				event.LockedAt = fixedNow.Add(-time.Second).Unix()
				event.LockedBy = "active-worker"
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			input := seedVideoTaskArchiveOnceFixture(t, db)
			_, event := reconcileVideoTaskArchiveOnceFixture(t, db, input)
			testCase.mutate(&event)
			require.NoError(t, db.Save(&event).Error)
			executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)
			executor.processor.now = func() time.Time { return fixedNow }

			_, err := executor.Execute(context.Background(), input)
			require.ErrorIs(t, err, ErrVideoTaskArchiveOnceBlocked)
			require.Zero(t, fetcher.fetches)
			require.Zero(t, store.calls)
			require.Zero(t, store.writeCalls)
			requireVideoTaskArchiveOnceEventUnchanged(t, db, event)
		})
	}
}

func TestVideoTaskArchiveOnceReclaimsStaleExactEventLock(t *testing.T) {
	fixedNow := time.Unix(2_000_000_000, 0)
	db := newVideoPipelineTestDB(t)
	input := seedVideoTaskArchiveOnceFixture(t, db)
	_, archiveEvent := reconcileVideoTaskArchiveOnceFixture(t, db, input)
	archiveEvent.LockedAt = fixedNow.Add(-videoTaskArchiveOnceLockTimeout - time.Second).Unix()
	archiveEvent.LockedBy = "stale-worker"
	require.NoError(t, db.Save(&archiveEvent).Error)
	executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)
	executor.processor.now = func() time.Time { return fixedNow }

	result, err := executor.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, VideoTaskArchiveOnceStageReady, result.Stage)
	require.Equal(t, 1, fetcher.fetches)
	require.Equal(t, 1, store.putCount["users/1/generations/3/source.mp4"])

	var persisted model.KKAIOutboxEvent
	require.NoError(t, db.First(&persisted, archiveEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, persisted.Status)
	require.Zero(t, persisted.LockedAt)
	require.Empty(t, persisted.LockedBy)
}

func TestVideoTaskArchiveOnceDoesNotDeliverEventForInvalidAsset(t *testing.T) {
	videoDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("video-content")))
	testCases := []struct {
		name        string
		mutateAsset func(*model.KKAIVideoAsset)
	}{
		{
			name: "failed asset",
			mutateAsset: func(asset *model.KKAIVideoAsset) {
				asset.State = model.VideoAssetStateFailed
				asset.FailureReason = "archive failed"
			},
		},
		{
			name: "archived asset with missing object",
			mutateAsset: func(asset *model.KKAIVideoAsset) {
				asset.ArchiveSourceURL = ""
				asset.MIMEType = "video/mp4"
				asset.SizeBytes = int64(len("video-content"))
				asset.SHA256 = videoDigest
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			input := seedVideoTaskArchiveOnceFixture(t, db)
			asset, archiveEvent := reconcileVideoTaskArchiveOnceFixture(t, db, input)
			testCase.mutateAsset(&asset)
			require.NoError(t, db.Save(&asset).Error)
			executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)

			_, err := executor.Execute(context.Background(), input)
			require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
			require.Zero(t, fetcher.fetches)
			require.Zero(t, store.writeCalls)
			requireVideoTaskArchiveOnceEventUnchanged(t, db, archiveEvent)
		})
	}
}

func TestVideoTaskArchiveOnceRejectsBadSuccessorBeforeProcessingPredecessor(t *testing.T) {
	t.Run("archive waits behind malformed inspect event", func(t *testing.T) {
		db := newVideoPipelineTestDB(t)
		input := seedVideoTaskArchiveOnceFixture(t, db)
		asset, archiveEvent := reconcileVideoTaskArchiveOnceFixture(t, db, input)
		inspectEvent := createVideoTaskArchiveOnceEvent(
			t, db, fmt.Sprintf("video:asset:%d:inspect:v1", asset.ID), videoOutboxTopicInspectOnce, asset.ID,
		)
		inspectEvent.Payload = `{"asset_id":999}`
		require.NoError(t, db.Save(&inspectEvent).Error)
		executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)

		_, err := executor.Execute(context.Background(), input)
		require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
		require.Zero(t, fetcher.fetches)
		require.Zero(t, store.calls)
		requireVideoTaskArchiveOnceEventUnchanged(t, db, archiveEvent)
	})

	t.Run("inspect waits behind malformed poster event", func(t *testing.T) {
		db := newVideoPipelineTestDB(t)
		input := seedVideoTaskArchiveOnceFixture(t, db)
		asset, archiveEvent := reconcileVideoTaskArchiveOnceFixture(t, db, input)
		asset.ArchiveSourceURL = ""
		asset.MIMEType = "video/mp4"
		asset.SizeBytes = int64(len("video-content"))
		asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("video-content")))
		require.NoError(t, db.Save(&asset).Error)
		archiveEvent.Status = model.KKAIOutboxStatusDelivered
		archiveEvent.DeliveredAt = time.Now().Unix()
		require.NoError(t, db.Save(&archiveEvent).Error)
		inspectEvent := createVideoTaskArchiveOnceEvent(
			t, db, fmt.Sprintf("video:asset:%d:inspect:v1", asset.ID), videoOutboxTopicInspectOnce, asset.ID,
		)
		posterEvent := createVideoTaskArchiveOnceEvent(
			t, db, fmt.Sprintf("video:asset:%d:poster:v1", asset.ID), videoOutboxTopicPosterOnce, asset.ID,
		)
		posterEvent.AggregateID = "999"
		require.NoError(t, db.Save(&posterEvent).Error)
		executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)

		_, err := executor.Execute(context.Background(), input)
		require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
		require.Zero(t, fetcher.fetches)
		require.Zero(t, store.calls)
		requireVideoTaskArchiveOnceEventUnchanged(t, db, inspectEvent)
	})
}

func TestVideoTaskArchiveOnceReadyStateRequiresArchivedObjects(t *testing.T) {
	testCases := []struct {
		name       string
		missingKey string
	}{
		{
			name:       "source object missing",
			missingKey: "users/1/generations/3/source.mp4",
		},
		{
			name:       "poster object missing",
			missingKey: "users/1/generations/3/source.poster.jpg",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			input := seedVideoTaskArchiveOnceFixture(t, db)
			executor, store, fetcher := newVideoTaskArchiveOnceExecutorForTest(t, db)
			ready, err := executor.Execute(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, VideoTaskArchiveOnceStageReady, ready.Stage)
			require.NoError(t, store.memoryVideoAssetStore.Delete(
				context.Background(), []string{testCase.missingKey},
			))

			_, err = executor.Execute(context.Background(), input)
			require.ErrorIs(t, err, ErrVideoTaskArchiveOnceCorrupt)
			require.Equal(t, 1, fetcher.fetches)
			require.Equal(t, 1, store.putCount["users/1/generations/3/source.mp4"])
			require.Equal(t, 1, store.putCount["users/1/generations/3/source.poster.jpg"])
		})
	}
}
