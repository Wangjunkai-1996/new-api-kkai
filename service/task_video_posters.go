package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"gorm.io/gorm"
)

const TaskVideoPosterTopic = "task.video.poster.v1"

var ErrTaskVideoPosterUnavailable = errors.New("task video poster unavailable")
var ErrTaskVideoPosterProcessingFailed = errors.New("task video poster processing failed")

type TaskVideoPosterPayload struct {
	TaskID      int64  `json:"task_id"`
	UserID      int    `json:"user_id"`
	ArtifactKey string `json:"artifact_key"`
	Version     string `json:"version"`
	Attempt     int    `json:"attempt"`
}

type TaskVideoPosterSource struct {
	URL         string
	Headers     map[string]string
	ProxyURL    string
	ProviderURL string
}

type TaskVideoPosterResolver func(context.Context, *model.Task, string) (TaskVideoPosterSource, error)

var taskVideoPosterTools struct {
	sync.Mutex
	key       string
	processor *FFmpegVideoMediaProcessor
}

// Verify immutable binary pins once for this process/environment. The serving
// API validates capability before enqueueing work for the leader.
func TaskVideoPosterProcessor(ctx context.Context) (*FFmpegVideoMediaProcessor, error) {
	taskVideoPosterTools.Lock()
	defer taskVideoPosterTools.Unlock()
	key := strings.Join([]string{os.Getenv(videoFFmpegPathEnvironment), os.Getenv(videoFFprobePathEnvironment), os.Getenv(videoMediaVersionEnvironment), os.Getenv(videoFFmpegDigestEnvironment), os.Getenv(videoFFprobeDigestEnvironment)}, "\x00")
	if taskVideoPosterTools.processor != nil && taskVideoPosterTools.key == key {
		return taskVideoPosterTools.processor, nil
	}
	processor, err := NewPinnedFFmpegVideoMediaProcessorFromEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	taskVideoPosterTools.key, taskVideoPosterTools.processor = key, processor
	return processor, nil
}

func TaskVideoPostersEnabled() bool {
	_, err := video_studio_setting.LoadR2Config()
	return err == nil && strings.TrimSpace(os.Getenv(videoFFmpegPathEnvironment)) != "" &&
		strings.TrimSpace(os.Getenv(videoFFprobePathEnvironment)) != "" &&
		strings.TrimSpace(os.Getenv(videoMediaVersionEnvironment)) != "" &&
		strings.TrimSpace(os.Getenv(videoFFmpegDigestEnvironment)) != "" &&
		strings.TrimSpace(os.Getenv(videoFFprobeDigestEnvironment)) != ""
}

// QueueTaskVideoPoster only records a small derivative job. It never downloads
// upstream content, archives the full video, or changes its retrieval path.
func QueueTaskVideoPoster(ctx context.Context, db *gorm.DB, task *model.Task, artifactKey string) error {
	if db == nil || task == nil || task.ID <= 0 || len(artifactKey) == 0 || len(artifactKey) > 128 {
		return ErrTaskVideoPosterUnavailable
	}
	payload := TaskVideoPosterPayload{TaskID: task.ID, UserID: task.UserId, ArtifactKey: artifactKey, Version: model.TaskVideoPosterVersion(task, artifactKey)}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		poster, prepared, err := model.PrepareTaskVideoPoster(ctx, tx, task.ID, artifactKey, payload.Version, now.Unix())
		if err != nil || !prepared {
			return err
		}
		payload.Attempt = poster.Attempt
		return EnqueueKKAIOutboxEvent(ctx, tx, fmt.Sprintf("task:poster:%s:%d", payload.Version, payload.Attempt), TaskVideoPosterTopic,
			strconv.FormatInt(task.ID, 10), now, payload)
	})
}

func TaskVideoPosterForDisplay(ctx context.Context, store VideoAssetStore, task *model.Task, artifactKey string) *dto.TaskVideoPoster {
	if task == nil || task.Status != model.TaskStatusSuccess || !task.ResultRetrievable() || task.IsAssetHostedResult() {
		return nil
	}
	if artifactKey == "" {
		return nil
	}
	snapshot, exists := task.PrivateData.VideoPosters[artifactKey]
	if !exists || snapshot.Version != model.TaskVideoPosterVersion(task, artifactKey) {
		return nil
	}
	view := &dto.TaskVideoPoster{State: snapshot.State, ArtifactKey: artifactKey, FailureCode: snapshot.FailureCode}
	if view.State == "pending" && snapshot.UpdatedAt < time.Now().Add(-15*time.Minute).Unix() {
		view.State, view.FailureCode = "unavailable", "processing_failed"
	} else if !TaskVideoPostersEnabled() && view.State == "pending" {
		view.State, view.FailureCode = "unavailable", "service_unavailable"
	}
	if snapshot.State != "ready" {
		return view
	}
	if store == nil || snapshot.ObjectKey != taskVideoPosterObjectKey(task.UserId, snapshot.Version) {
		view.State, view.FailureCode = "unavailable", "service_unavailable"
		return view
	}
	ttl := time.Duration(video_studio_setting.Get().SignedURLSeconds) * time.Second
	location, err := store.PresignDownload(ctx, snapshot.ObjectKey, "poster.jpg", false, ttl)
	if err != nil {
		view.State, view.FailureCode = "unavailable", "service_unavailable"
		return view
	}
	view.URL, view.ExpiresAt = location, time.Now().Add(ttl).Unix()
	return view
}

func taskVideoPosterObjectKey(userID int, version string) string {
	return fmt.Sprintf("users/%d/task-posters/%s.jpg", userID, version)
}

// TaskVideoPosterWorker uses two durable leased consumers, independent of
// Studio's archival and backfill switches. Only the poster bytes are retained.
type TaskVideoPosterWorker struct {
	processors []*KKAIOutboxProcessor
}

func NewTaskVideoPosterWorker(db *gorm.DB, workerID string, store VideoAssetStore, media VideoMediaProcessor, fetcher *HTTPVideoArchiveFetcher, resolve TaskVideoPosterResolver, tempDir string) (*TaskVideoPosterWorker, error) {
	if db == nil || store == nil || media == nil || fetcher == nil || resolve == nil || workerID == "" {
		return nil, ErrTaskVideoPosterUnavailable
	}
	worker := &TaskVideoPosterWorker{}
	for index := 0; index < 2; index++ {
		processor := NewKKAIOutboxProcessor(db, fmt.Sprintf("%s-task-poster-%d", workerID, index))
		processor.maxAttempts, processor.baseRetry, processor.maxRetry = 3, time.Minute, 5*time.Minute
		processor.lockTimeout = 30 * time.Second
		if err := processor.Register(TaskVideoPosterTopic, func(ctx context.Context, event model.KKAIOutboxEvent) error {
			var payload TaskVideoPosterPayload
			if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.TaskID <= 0 || payload.UserID <= 0 || payload.ArtifactKey == "" || len(payload.ArtifactKey) > 128 || len(payload.Version) != 64 || payload.Attempt < 0 || payload.Attempt > 1 || event.AggregateID != strconv.FormatInt(payload.TaskID, 10) {
				return PermanentKKAIOutboxError(ErrTaskVideoPosterUnavailable)
			}
			var task model.Task
			if err := db.WithContext(ctx).First(&task, payload.TaskID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if task.UserId != payload.UserID || task.Status != model.TaskStatusSuccess || !task.ResultRetrievable() || task.IsAssetHostedResult() || payload.Version != model.TaskVideoPosterVersion(&task, payload.ArtifactKey) {
				return nil
			}
			snapshot, exists := task.PrivateData.VideoPosters[payload.ArtifactKey]
			if !exists || snapshot.Version != payload.Version || snapshot.State != "pending" || snapshot.Attempt != payload.Attempt {
				return nil
			}
			// Recheck the live owner before spending network/storage resources.
			var owner model.User
			if err := db.WithContext(ctx).Select("id", "status", "tenant_id").First(&owner, task.UserId).Error; err != nil {
				return PermanentKKAIOutboxError(ErrTaskVideoPosterUnavailable)
			}
			if owner.Status != common.UserStatusEnabled || owner.TenantID != task.TenantID {
				return PermanentKKAIOutboxError(ErrTaskVideoPosterUnavailable)
			}
			jobCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			objectKey := taskVideoPosterObjectKey(task.UserId, payload.Version)
			metadata, err := store.Head(jobCtx, objectKey)
			if err != nil && !errors.Is(err, ErrVideoAssetObjectNotFound) {
				return ErrTaskVideoPosterUnavailable
			}
			if err != nil || metadata.ContentType != "image/jpeg" || metadata.ContentLength <= 0 || metadata.ContentLength > videoPosterMaximumBytes {
				source, err := resolve(jobCtx, &task, payload.ArtifactKey)
				if err != nil {
					return PermanentKKAIOutboxError(ErrTaskVideoPosterUnavailable)
				}
				maxBytes := min(video_studio_setting.Get().MaxArchivedVideoBytes, int64(256<<20))
				fetched, err := fetcher.FetchTaskSource(jobCtx, videoArchiveTaskSource{
					Source: source.URL, Headers: source.Headers, ProxyURL: source.ProxyURL, ProviderContentBaseURL: source.ProviderURL,
				}, maxBytes)
				if err != nil {
					if isPermanentVideoAssetError(err) {
						return PermanentKKAIOutboxError(ErrTaskVideoPosterUnavailable)
					}
					return ErrTaskVideoPosterUnavailable
				}
				defer fetched.Remove()
				output, err := os.CreateTemp(tempDir, "new-api-task-poster-*.jpg")
				if err != nil {
					return ErrTaskVideoPosterUnavailable
				}
				outputPath := output.Name()
				_ = output.Close()
				defer os.Remove(outputPath)
				if err := media.CreatePoster(jobCtx, fetched.Path, outputPath, videoPosterMaximumBytes); err != nil {
					if isPermanentVideoAssetError(err) {
						return PermanentKKAIOutboxError(ErrTaskVideoPosterProcessingFailed)
					}
					return ErrTaskVideoPosterProcessingFailed
				}
				file, err := os.Open(outputPath)
				if err != nil {
					return ErrTaskVideoPosterUnavailable
				}
				info, statErr := file.Stat()
				if statErr != nil || info.Size() <= 0 || info.Size() > videoPosterMaximumBytes {
					_ = file.Close()
					return PermanentKKAIOutboxError(ErrTaskVideoPosterProcessingFailed)
				}
				putErr := store.Put(jobCtx, objectKey, "image/jpeg", file, info.Size())
				closeErr := file.Close()
				if putErr != nil || closeErr != nil {
					return ErrTaskVideoPosterUnavailable
				}
			}
			var updated bool
			err = db.WithContext(jobCtx).Transaction(func(tx *gorm.DB) error {
				var updateErr error
				updated, updateErr = model.UpdateTaskVideoPoster(jobCtx, tx, task.ID, payload.ArtifactKey,
					model.TaskVideoPosterSnapshot{State: "ready", Version: payload.Version, ObjectKey: objectKey, Attempt: payload.Attempt, UpdatedAt: time.Now().Unix()}, false)
				return updateErr
			})
			if err != nil {
				return err
			}
			if !updated {
				// An owner can discard/change the result during processing.
				var current model.Task
				if err := db.WithContext(jobCtx).First(&current, task.ID).Error; err == nil {
					if poster := current.PrivateData.VideoPosters[payload.ArtifactKey]; poster.Version == payload.Version &&
						current.ResultRetrievable() && (poster.State == "pending" || poster.State == "ready" && poster.ObjectKey == objectKey) {
						return nil
					}
				}
				return store.Delete(jobCtx, []string{objectKey})
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if err := processor.registerDeadLetter(TaskVideoPosterTopic, func(ctx context.Context, tx *gorm.DB, event model.KKAIOutboxEvent, deliveryErr error, now time.Time) error {
			var payload TaskVideoPosterPayload
			if common.UnmarshalJsonStr(event.Payload, &payload) != nil {
				return nil
			}
			code := "source_unavailable"
			if errors.Is(deliveryErr, ErrTaskVideoPosterProcessingFailed) {
				code = "processing_failed"
			}
			_, err := model.UpdateTaskVideoPoster(ctx, tx, payload.TaskID, payload.ArtifactKey,
				model.TaskVideoPosterSnapshot{State: "unavailable", Version: payload.Version, FailureCode: code, Attempt: payload.Attempt, UpdatedAt: now.Unix()}, false)
			return err
		}); err != nil {
			return nil, err
		}
		worker.processors = append(worker.processors, processor)
	}
	return worker, nil
}

func (worker *TaskVideoPosterWorker) ProcessOnce(ctx context.Context) error {
	var wait sync.WaitGroup
	errorsByWorker := make([]error, len(worker.processors))
	for index, processor := range worker.processors {
		wait.Add(1)
		go func(index int, processor *KKAIOutboxProcessor) {
			defer wait.Done()
			_, errorsByWorker[index] = processor.ProcessBatch(ctx, 1)
		}(index, processor)
	}
	wait.Wait()
	return errors.Join(errorsByWorker...)
}

// CleanupTaskVideoPosterObjects walks completed jobs in bounded batches. It
// removes derivative bytes once the task/result has gone away or changed.
func CleanupTaskVideoPosterObjects(ctx context.Context, db *gorm.DB, store VideoAssetStore, afterID int64, limit int) (int64, error) {
	if db == nil || store == nil || limit <= 0 || limit > 500 {
		return afterID, ErrTaskVideoPosterUnavailable
	}
	var events []model.KKAIOutboxEvent
	if err := db.WithContext(ctx).Where("topic = ? AND id > ? AND status IN ?", TaskVideoPosterTopic, afterID,
		[]string{model.KKAIOutboxStatusDelivered, model.KKAIOutboxStatusDead}).Order("id ASC").Limit(limit).Find(&events).Error; err != nil {
		return afterID, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		var payload TaskVideoPosterPayload
		if common.UnmarshalJsonStr(event.Payload, &payload) == nil && payload.TaskID > 0 {
			ids = append(ids, payload.TaskID)
		}
	}
	var tasks []model.Task
	if len(ids) > 0 {
		if err := db.WithContext(ctx).Where("id IN ?", ids).Find(&tasks).Error; err != nil {
			return afterID, err
		}
	}
	byID := make(map[int64]model.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	for _, event := range events {
		var payload TaskVideoPosterPayload
		if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.TaskID <= 0 || payload.UserID <= 0 || len(payload.Version) != 64 {
			afterID = event.ID
			continue
		}
		task, exists := byID[payload.TaskID]
		keep := exists && task.UserId == payload.UserID && task.Status == model.TaskStatusSuccess && task.ResultRetrievable() &&
			!task.IsAssetHostedResult() && model.TaskVideoPosterVersion(&task, payload.ArtifactKey) == payload.Version
		if keep {
			poster := task.PrivateData.VideoPosters[payload.ArtifactKey]
			keep = poster.Version == payload.Version && (poster.State == "pending" ||
				poster.State == "ready" && poster.ObjectKey == taskVideoPosterObjectKey(payload.UserID, payload.Version))
		}
		if !keep {
			if err := store.Delete(ctx, []string{taskVideoPosterObjectKey(payload.UserID, payload.Version)}); err != nil {
				return afterID, err
			}
		}
		afterID = event.ID
	}
	return afterID, nil
}
