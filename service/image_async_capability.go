package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
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

var imageAsyncSchemaCache struct {
	sync.Mutex
	db        *gorm.DB
	ready     bool
	expiresAt time.Time
}

// ImageAsyncSchemaReady validates the complete migration ledger and physical
// schema. Cache both outcomes so admission and worker lanes do not probe every request.
func ImageAsyncSchemaReady(ctx context.Context, db *gorm.DB) bool {
	if db == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	imageAsyncSchemaCache.Lock()
	defer imageAsyncSchemaCache.Unlock()
	if imageAsyncSchemaCache.db == db && time.Now().Before(imageAsyncSchemaCache.expiresAt) {
		return imageAsyncSchemaCache.ready
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ready := kkaimigrate.CheckImageAsyncSchema(checkCtx, db) == nil
	ttl := 5 * time.Second
	if ready {
		ttl = time.Minute
	}
	imageAsyncSchemaCache.db = db
	imageAsyncSchemaCache.ready = ready
	imageAsyncSchemaCache.expiresAt = time.Now().Add(ttl)
	return ready
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
