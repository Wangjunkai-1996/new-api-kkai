package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
)

func RegisterImageUploadBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	if err := registry.Register(BackgroundJob{
		Name: "image-reference-validation", Interval: 2 * time.Second, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !image_studio_setting.Get().WorkerEnabled || !ImageAsyncSchemaReady(ctx, model.DB) {
				return nil
			}
			finish, ok := BeginImageWorkerOperation()
			if !ok {
				return nil
			}
			defer finish()
			ctx, err := WithCurrentImageWorkerLease(ctx, workerID)
			if err != nil {
				return err
			}
			store, err := ImageStudioR2AssetStore(ctx)
			if err != nil {
				return err
			}
			pipeline, err := NewImageUploadPipeline(model.DB, store)
			if err != nil {
				return err
			}
			processor := NewKKAIOutboxProcessor(model.DB, workerID+"-image-upload")
			if err := pipeline.Register(processor); err != nil {
				return err
			}
			_, err = processor.ProcessBatch(ctx, 1)
			return err
		},
	}); err != nil {
		return err
	}
	return registry.Register(BackgroundJob{
		Name: "image-reference-cleanup", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !image_studio_setting.Get().WorkerEnabled || !ImageAsyncSchemaReady(ctx, model.DB) {
				return nil
			}
			finish, ok := BeginImageWorkerOperation()
			if !ok {
				return nil
			}
			defer finish()
			ctx, err := WithCurrentImageWorkerLease(ctx, workerID)
			if err != nil {
				return err
			}
			store, err := ImageStudioR2AssetStore(ctx)
			if err != nil {
				return err
			}
			_, err = ReconcileImageReferenceUploads(ctx, model.DB, store, 25)
			return err
		},
	})
}
