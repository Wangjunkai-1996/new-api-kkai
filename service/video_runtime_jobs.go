package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
)

const videoStudioTempDirectoryEnvironment = "VIDEO_STUDIO_TEMP_DIR"

type videoStudioWorkerJobs interface {
	ExpireUploads(context.Context) error
	CleanupReferences(context.Context) error
	ProcessOutbox(context.Context) error
}

type videoStudioWorkerRuntime struct {
	mu       sync.Mutex
	workerID string
	store    VideoAssetStore
	worker   *VideoOutboxWorker
}

func (runtime *videoStudioWorkerRuntime) initialize(ctx context.Context) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.worker != nil && runtime.store != nil {
		return nil
	}
	store, err := NewR2VideoAssetStoreFromEnvironment(ctx)
	if err != nil {
		return err
	}
	media, err := NewPinnedFFmpegVideoMediaProcessorFromEnvironment(ctx)
	if err != nil {
		return err
	}
	tempDir := strings.TrimSpace(os.Getenv(videoStudioTempDirectoryEnvironment))
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	worker, err := NewVideoOutboxWorker(
		model.DB,
		runtime.workerID,
		store,
		media,
		NewHTTPVideoArchiveFetcher(tempDir),
		tempDir,
		defaultVideoOutboxConcurrency,
	)
	if err != nil {
		return err
	}
	runtime.store = store
	runtime.worker = worker
	return nil
}

func (runtime *videoStudioWorkerRuntime) ExpireUploads(ctx context.Context) error {
	if err := runtime.initialize(ctx); err != nil {
		return err
	}
	_, err := ExpireVideoAssetUploads(ctx, model.DB, runtime.store, 100)
	return err
}

func (runtime *videoStudioWorkerRuntime) CleanupReferences(ctx context.Context) error {
	if err := runtime.initialize(ctx); err != nil {
		return err
	}
	settings := video_studio_setting.Get()
	cutoff := time.Now().Add(-time.Duration(settings.ReferenceOrphanHours) * time.Hour)
	_, err := CleanupAbandonedVideoReferenceAssets(ctx, model.DB, cutoff, 100)
	return err
}

func (runtime *videoStudioWorkerRuntime) ProcessOutbox(ctx context.Context) error {
	if err := runtime.initialize(ctx); err != nil {
		return err
	}
	return runtime.worker.ProcessOnce(ctx)
}

func RegisterVideoStudioBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	settings := video_studio_setting.Get()
	if settings.ArchiveEnqueueEnabled {
		reconciler := &videoTaskOutputReconciler{}
		if err := registry.Register(BackgroundJob{
			Name: "video-studio-reconcile", Interval: 5 * time.Second, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
			Run: func(ctx context.Context) error {
				if !video_studio_setting.Get().ArchiveEnqueueEnabled {
					return nil
				}
				_, err := reconciler.Reconcile(ctx, model.DB, 50)
				return err
			},
		}); err != nil {
			return err
		}
	}
	if settings.BackfillEnabled {
		reconciler := &videoTaskOutputReconciler{}
		if err := registry.Register(BackgroundJob{
			Name: "video-studio-backfill", Interval: time.Minute, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
			Run: func(ctx context.Context) error {
				if !video_studio_setting.Get().BackfillEnabled {
					return nil
				}
				_, err := reconciler.Reconcile(ctx, model.DB, 500)
				return err
			},
		}); err != nil {
			return err
		}
	}
	if settings.AccessMode != video_studio_setting.AccessModeOff || settings.ArchiveEnqueueEnabled ||
		settings.BackfillEnabled || settings.WorkerEnabled {
		if err := registry.Register(BackgroundJob{
			Name: "video-studio-idempotency-cleanup", Interval: time.Hour, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
			Run: func(ctx context.Context) error {
				current := video_studio_setting.Get()
				if current.AccessMode == video_studio_setting.AccessModeOff && !current.ArchiveEnqueueEnabled &&
					!current.BackfillEnabled && !current.WorkerEnabled {
					return nil
				}
				_, err := CleanupExpiredIdempotencyKeys(ctx, model.DB, time.Now(), 500)
				return err
			},
		}); err != nil {
			return err
		}
	}
	return registerVideoStudioWorkerJobs(registry, &videoStudioWorkerRuntime{workerID: workerID})
}

func registerVideoStudioWorkerJobs(registry *BackgroundJobRegistry, runtime videoStudioWorkerJobs) error {
	if registry == nil || runtime == nil {
		return ErrInvalidBackgroundJob
	}
	if err := registry.Register(BackgroundJob{
		Name: "video-studio-upload-expiry", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !video_studio_setting.Get().WorkerEnabled {
				return nil
			}
			return runtime.ExpireUploads(ctx)
		},
	}); err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{
		Name: "video-studio-reference-cleanup", Interval: time.Hour, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !video_studio_setting.Get().WorkerEnabled {
				return nil
			}
			return runtime.CleanupReferences(ctx)
		},
	}); err != nil {
		return err
	}
	return registry.Register(BackgroundJob{
		Name: "video-studio-outbox", Interval: 2 * time.Second, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !video_studio_setting.Get().WorkerEnabled {
				return nil
			}
			return runtime.ProcessOutbox(ctx)
		},
	})
}
