package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"gorm.io/gorm"
)

const (
	ImageGenerateTopic    = "image.generate.v1"
	ImageQueueTimeout     = 10 * time.Minute
	ImageExecutionTimeout = 20 * time.Minute
	ImageArchiveTimeout   = 2 * time.Minute
	ImageRecoveryGrace    = 2 * time.Minute
)

// ImageAsyncSchemaReady reads the periodically refreshed admission snapshot.
func ImageAsyncSchemaReady(ctx context.Context, db *gorm.DB) bool {
	return imageAsyncReadSchema.ready(ctx, db)
}

func ImageAsyncAdmissionEnabled(ctx context.Context, db *gorm.DB) bool {
	settings := image_studio_setting.Get()
	return settings.AsyncSubmissionEnabled && settings.WorkerEnabled && ImageAsyncSchemaReady(ctx, db)
}

func PopulateImageAsyncCapability(ctx context.Context, db *gorm.DB, capability *ImageStudioTokenCapability) {
	capability.AsyncModels = []string{}
	if !ImageAsyncAdmissionEnabled(ctx, db) {
		return
	}
	for _, name := range capability.EffectiveModels {
		if _, configured, err := image_pricing_setting.Resolve(name, ""); configured && err == nil {
			capability.AsyncModels = append(capability.AsyncModels, name)
		}
	}
	capability.AsyncSubmissionEnabled = len(capability.AsyncModels) > 0
}
