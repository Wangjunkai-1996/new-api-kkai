package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
)

type imageStudioWorkerRuntime struct {
	mu       sync.Mutex
	workerID string
	current  atomic.Pointer[imageStudioWorkerRuntimeSnapshot]
}

type imageStudioWorkerRuntimeSnapshot struct {
	store              ImageAssetStore
	processor          *KKAIOutboxProcessor
	thumbnailMaxPixels int64
}

func (runtime *imageStudioWorkerRuntime) initialize(ctx context.Context) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	store, err := ImageStudioR2AssetStore(ctx)
	if err != nil {
		return err
	}
	settings := image_studio_setting.Get()
	if current := runtime.current.Load(); current != nil && current.store == store &&
		current.thumbnailMaxPixels == settings.ThumbnailMaxPixels {
		return nil
	}
	media, err := newRasterImageThumbnailProcessor(settings.ThumbnailMaxPixels)
	if err != nil {
		return err
	}
	pipeline, err := NewImageAssetOutboxPipeline(model.DB, store, media, ImageStudioTempDirectory())
	if err != nil {
		return err
	}
	processor := NewKKAIOutboxProcessor(model.DB, runtime.workerID+"-image")
	if err := pipeline.Register(processor); err != nil {
		return err
	}
	runtime.current.Store(&imageStudioWorkerRuntimeSnapshot{
		store: store, processor: processor, thumbnailMaxPixels: settings.ThumbnailMaxPixels,
	})
	return nil
}

func (runtime *imageStudioWorkerRuntime) ProcessOutbox(ctx context.Context) error {
	if err := runtime.initialize(ctx); err != nil {
		return err
	}
	current := runtime.current.Load()
	if current == nil || current.processor == nil {
		return ErrInvalidImageAssetPipeline
	}
	_, err := current.processor.ProcessBatch(ctx, 50)
	return err
}

func RegisterImageStudioBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	runtime := &imageStudioWorkerRuntime{workerID: workerID}
	if err := registry.Register(BackgroundJob{
		Name: "image-studio-outbox", Interval: 2 * time.Second, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !image_studio_setting.Get().WorkerEnabled {
				return nil
			}
			return runtime.ProcessOutbox(ctx)
		},
	}); err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{
		Name: "image-studio-submission-reconcile", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			settings := image_studio_setting.Get()
			if settings.AccessMode == image_studio_setting.AccessModeOff && !settings.WorkerEnabled {
				return nil
			}
			staleBefore := time.Now().Add(-time.Duration(settings.SubmissionTimeoutSecs) * time.Second)
			_, err := ReconcileStaleImageGenerations(ctx, model.DB, staleBefore, 100)
			return err
		},
	}); err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{
		Name: "image-studio-catalog-orphan-reconcile", Interval: time.Hour, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			settings := image_studio_setting.Get()
			if settings.AccessMode == image_studio_setting.AccessModeOff && !settings.WorkerEnabled {
				return nil
			}
			_, err := ReconcileOrphanedImageCatalogAssets(
				ctx, model.DB, time.Now().Add(-ImageCatalogOrphanTTL), 100,
			)
			return err
		},
	}); err != nil {
		return err
	}
	return registry.Register(BackgroundJob{
		Name: "image-studio-idempotency-cleanup", Interval: time.Hour, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			settings := image_studio_setting.Get()
			if settings.AccessMode == image_studio_setting.AccessModeOff && !settings.WorkerEnabled {
				return nil
			}
			_, err := cleanupExpiredIdempotencyKeysForOperation(
				ctx, model.DB, model.ImageIdempotencyOperationSubmit, time.Now(), 500,
			)
			return err
		},
	})
}
