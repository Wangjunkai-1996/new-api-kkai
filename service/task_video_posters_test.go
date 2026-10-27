package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskVideoPosterQueuesOnceAndPreservesBillingSnapshot(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	task := &model.Task{TaskID: "poster-task", UserId: 7, Status: model.TaskStatusSuccess, FinishTime: 100}
	task.PrivateData.BillingSource = "wallet"
	task.PrivateData.ResultURL = "https://media.example/video.mp4"
	require.NoError(t, db.Create(task).Error)
	ctx := context.Background()
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	var events []model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", TaskVideoPosterTopic).Find(&events).Error)
	require.Len(t, events, 1)
	var stored model.Task
	require.NoError(t, db.First(&stored, task.ID).Error)
	assert.Equal(t, "wallet", stored.PrivateData.BillingSource)
	assert.Equal(t, "pending", stored.PrivateData.VideoPosters["video"].State)
	assert.NotContains(t, events[0].Payload, "https://")
	assert.NotContains(t, events[0].Payload, "wallet")

	// The worker merges fresh private_data, preserving billing written after
	// enqueue rather than serializing the stale in-memory task value.
	stored.PrivateData.BillingSource = "subscription"
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", stored.PrivateData).Error)
	version := model.TaskVideoPosterVersion(&stored, "video")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := model.UpdateTaskVideoPoster(ctx, tx, task.ID, "video", model.TaskVideoPosterSnapshot{
			State: "ready", Version: version, ObjectKey: taskVideoPosterObjectKey(7, version), UpdatedAt: time.Now().Unix(),
		}, false)
		return err
	}))
	require.NoError(t, db.First(&stored, task.ID).Error)
	assert.Equal(t, "subscription", stored.PrivateData.BillingSource)
	assert.Equal(t, "ready", stored.PrivateData.VideoPosters["video"].State)
}

func TestTaskVideoPosterRetriesOnceWithoutStaleEventOverwrite(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	task := &model.Task{TaskID: "poster-retry", UserId: 7, Status: model.TaskStatusSuccess, FinishTime: 100}
	task.PrivateData.ResultURL = "https://media.example/retry.mp4"
	require.NoError(t, db.Create(task).Error)
	ctx := context.Background()
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	require.NoError(t, db.First(task, task.ID).Error)
	version := model.TaskVideoPosterVersion(task, "video")
	snapshot := task.PrivateData.VideoPosters["video"]
	snapshot.State = "unavailable"
	snapshot.UpdatedAt = time.Now().Add(-31 * time.Minute).Unix()
	task.PrivateData.VideoPosters["video"] = snapshot
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error)
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	var events []model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", TaskVideoPosterTopic).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	assert.NotEqual(t, events[0].EventKey, events[1].EventKey)
	require.NoError(t, db.First(task, task.ID).Error)
	assert.Equal(t, 1, task.PrivateData.VideoPosters["video"].Attempt)
	var updated bool
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		updated, err = model.UpdateTaskVideoPoster(ctx, tx, task.ID, "video", model.TaskVideoPosterSnapshot{
			State: "unavailable", Version: version, Attempt: 0, UpdatedAt: time.Now().Unix(),
		}, false)
		return err
	}))
	assert.False(t, updated)
	require.NoError(t, db.First(task, task.ID).Error)
	assert.Equal(t, "pending", task.PrivateData.VideoPosters["video"].State)

	key := taskVideoPosterObjectKey(7, version)
	store := newMemoryVideoAssetStore()
	store.objects[key] = []byte("poster")
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", events[0].ID).Update("status", model.KKAIOutboxStatusDead).Error)
	_, err := CleanupTaskVideoPosterObjects(ctx, db, store, 0, 10)
	require.NoError(t, err)
	assert.Contains(t, store.objects, key, "old completed attempts must not delete a live retry's object")

	snapshot = task.PrivateData.VideoPosters["video"]
	snapshot.State = "unavailable"
	snapshot.UpdatedAt = time.Now().Add(-31 * time.Minute).Unix()
	task.PrivateData.VideoPosters["video"] = snapshot
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error)
	require.NoError(t, QueueTaskVideoPoster(ctx, db, task, "video"))
	require.NoError(t, db.Where("topic = ?", TaskVideoPosterTopic).Find(&events).Error)
	assert.Len(t, events, 2, "a terminal retry must not enqueue indefinitely")
}

func TestTaskVideoPosterWorkerExtractsRealOutputOnceAndSignsOnlyScopedKey(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	owner := &model.User{Id: 7, Username: "poster-owner", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(owner).Error)
	task := &model.Task{TaskID: "poster-task", UserId: 7, Status: model.TaskStatusSuccess, FinishTime: 100}
	task.PrivateData.ResultURL = "https://media.example/video.mp4"
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, QueueTaskVideoPoster(context.Background(), db, task, "video"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("actual-video-bytes"))
	}))
	defer server.Close()
	fetcher := NewHTTPVideoArchiveFetcher(t.TempDir())
	fetcher.client = server.Client()
	fetcher.validateURL = func(context.Context, *url.URL) error { return nil }
	fetcher.availableBytes = func(string) (uint64, error) { return 1 << 30, nil }
	store := newMemoryVideoAssetStore()
	var calls int
	media := callbackVideoMediaProcessor{createPoster: func(_ context.Context, input, output string, _ int64) error {
		calls++
		content, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		return os.WriteFile(output, append([]byte("jpeg-poster:"), content...), 0o600)
	}}
	worker, err := NewTaskVideoPosterWorker(db, "test-poster", store, media, fetcher,
		func(_ context.Context, _ *model.Task, key string) (TaskVideoPosterSource, error) {
			assert.Equal(t, "video", key)
			return TaskVideoPosterSource{URL: server.URL}, nil
		}, t.TempDir())
	require.NoError(t, err)
	_, err = worker.processors[0].ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	var stored model.Task
	require.NoError(t, db.First(&stored, task.ID).Error)
	poster := stored.PrivateData.VideoPosters["video"]
	assert.Equal(t, "ready", poster.State)
	assert.Equal(t, taskVideoPosterObjectKey(7, poster.Version), poster.ObjectKey)
	assert.Equal(t, []byte("jpeg-poster:actual-video-bytes"), store.objects[poster.ObjectKey])
	assert.Equal(t, 1, calls)
	view := TaskVideoPosterForDisplay(context.Background(), store, &stored, "video")
	require.NotNil(t, view)
	assert.Equal(t, "ready", view.State)
	assert.Equal(t, "https://signed.example/object", view.URL)
	assert.Equal(t, poster.ObjectKey, store.downloadKey)
	_, err = worker.processors[0].ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestTaskVideoPosterFetcherRejectsPrivateSource(t *testing.T) {
	fetcher := NewHTTPVideoArchiveFetcher(t.TempDir())
	_, err := fetcher.Fetch(context.Background(), "https://127.0.0.1/private.mp4", 1<<20)
	require.ErrorIs(t, err, ErrVideoArchiveSourceRejected)
}

func TestTaskVideoPosterCleanupKeepsLiveResultAndDeletesDiscardedResult(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	task := &model.Task{TaskID: "poster-cleanup", UserId: 7, Status: model.TaskStatusSuccess, FinishTime: 100}
	task.PrivateData.ResultURL = "https://media.example/video.mp4"
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, QueueTaskVideoPoster(context.Background(), db, task, "video"))
	version := model.TaskVideoPosterVersion(task, "video")
	key := taskVideoPosterObjectKey(7, version)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := model.UpdateTaskVideoPoster(context.Background(), tx, task.ID, "video", model.TaskVideoPosterSnapshot{
			State: "ready", Version: version, ObjectKey: key, UpdatedAt: time.Now().Unix(),
		}, false)
		return err
	}))
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", TaskVideoPosterTopic).Update("status", model.KKAIOutboxStatusDelivered).Error)
	store := newMemoryVideoAssetStore()
	store.objects[key] = []byte("poster")
	store.contentType[key] = "image/jpeg"
	cursor, err := CleanupTaskVideoPosterObjects(context.Background(), db, store, 0, 10)
	require.NoError(t, err)
	assert.Positive(t, cursor)
	assert.Equal(t, []byte("poster"), store.objects[key])
	require.NoError(t, db.First(task, task.ID).Error)
	task.PrivateData.ResultDiscarded = true
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error)
	_, err = CleanupTaskVideoPosterObjects(context.Background(), db, store, 0, 10)
	require.NoError(t, err)
	assert.NotContains(t, store.objects, key)
}

func TestTaskVideoPosterVersionIgnoresKnownSignedQueryButTracksSource(t *testing.T) {
	task := &model.Task{ID: 7, TaskID: "video", FinishTime: 100}
	task.PrivateData.ResultURL = "https://media.example/a.mp4?X-Amz-Signature=first&id=42"
	first := model.TaskVideoPosterVersion(task, "video")
	task.PrivateData.ResultURL = "https://media.example/a.mp4?X-Amz-Signature=second&id=42"
	assert.Equal(t, first, model.TaskVideoPosterVersion(task, "video"))
	task.PrivateData.ResultURL = "https://media.example/b.mp4?X-Amz-Signature=second&id=42"
	assert.NotEqual(t, first, model.TaskVideoPosterVersion(task, "video"))
	task.PrivateData.ResultURL = "https://media.example/a.mp4?X-Amz-Signature=second&id=43"
	assert.NotEqual(t, first, model.TaskVideoPosterVersion(task, "video"))
	task.PrivateData.ResultURL = "https://media.example/a.mp4?token=one&key=first"
	unsigned := model.TaskVideoPosterVersion(task, "video")
	task.PrivateData.ResultURL = "https://media.example/a.mp4?token=two&key=first"
	assert.NotEqual(t, unsigned, model.TaskVideoPosterVersion(task, "video"))
	task.PrivateData.ResultURL = "https://media.example/a.mp4?token=one&key=second"
	assert.NotEqual(t, unsigned, model.TaskVideoPosterVersion(task, "video"))
	task.PrivateData.ResultURL = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: "video-plugin"}}
	task.Data = []byte(`{"video":"https://media.example/output-1.mp4"}`)
	firstArtifact := model.TaskVideoPosterVersion(task, "clip-1")
	task.Data = []byte(`{"video":"https://media.example/output-2.mp4"}`)
	assert.NotEqual(t, firstArtifact, model.TaskVideoPosterVersion(task, "clip-1"))
	task.PrivateData.Execution = nil
	assert.Equal(t, model.TaskVideoPosterVersion(task, "video"), model.TaskVideoPosterVersion(&model.Task{
		ID: task.ID, TaskID: task.TaskID, FinishTime: task.FinishTime,
	}, "video"))
}
