package service

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
)

const videoStudioTempDirectoryEnvironment = "VIDEO_STUDIO_TEMP_DIR"

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
	if !settings.WorkerEnabled {
		return nil
	}
	store, err := NewR2VideoAssetStoreFromEnvironment(context.Background())
	if err != nil {
		return err
	}
	media, err := NewPinnedFFmpegVideoMediaProcessorFromEnvironment(context.Background())
	if err != nil {
		return err
	}
	tempDir := strings.TrimSpace(os.Getenv(videoStudioTempDirectoryEnvironment))
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	fetcher := NewHTTPVideoArchiveFetcher(tempDir)
	worker, err := NewVideoOutboxWorker(model.DB, workerID, store, media, fetcher, tempDir, defaultVideoOutboxConcurrency)
	if err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{
		Name: "video-studio-upload-expiry", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !video_studio_setting.Get().WorkerEnabled {
				return nil
			}
			_, err := ExpireVideoAssetUploads(ctx, model.DB, store, 100)
			return err
		},
	}); err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{
		Name: "video-studio-reference-cleanup", Interval: time.Hour, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			settings := video_studio_setting.Get()
			if !settings.WorkerEnabled {
				return nil
			}
			cutoff := time.Now().Add(-time.Duration(settings.ReferenceOrphanHours) * time.Hour)
			_, err := CleanupAbandonedVideoReferenceAssets(ctx, model.DB, cutoff, 100)
			return err
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
			return worker.ProcessOnce(ctx)
		},
	})
}
