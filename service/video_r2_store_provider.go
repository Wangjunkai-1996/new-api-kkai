package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/video_studio_setting"
)

const videoStudioR2StoreRefreshInterval = 30 * time.Second

type videoStudioR2StoreSnapshot struct {
	config       video_studio_setting.R2Config
	store        *S3VideoAssetStore
	refreshAfter time.Time
}

type videoStudioR2StoreProvider struct {
	mu              sync.Mutex
	current         atomic.Pointer[videoStudioR2StoreSnapshot]
	loadConfig      func() (video_studio_setting.R2Config, error)
	buildStore      func(context.Context, video_studio_setting.R2Config) (*S3VideoAssetStore, error)
	now             func() time.Time
	refreshInterval time.Duration
}

var videoStudioR2Stores = newVideoStudioR2StoreProvider(
	video_studio_setting.LoadR2Config,
	NewR2VideoAssetStore,
	time.Now,
	videoStudioR2StoreRefreshInterval,
)

func newVideoStudioR2StoreProvider(
	loadConfig func() (video_studio_setting.R2Config, error),
	buildStore func(context.Context, video_studio_setting.R2Config) (*S3VideoAssetStore, error),
	now func() time.Time,
	refreshInterval time.Duration,
) *videoStudioR2StoreProvider {
	return &videoStudioR2StoreProvider{
		loadConfig:      loadConfig,
		buildStore:      buildStore,
		now:             now,
		refreshInterval: refreshInterval,
	}
}

func (provider *videoStudioR2StoreProvider) get(ctx context.Context) (*S3VideoAssetStore, error) {
	now := provider.now()
	if current := provider.current.Load(); current != nil && now.Before(current.refreshAfter) {
		return current.store, nil
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()

	now = provider.now()
	current := provider.current.Load()
	if current != nil && now.Before(current.refreshAfter) {
		return current.store, nil
	}
	// Failed refreshes stay fail-closed; only operations already holding the old store continue with it.
	config, err := provider.loadConfig()
	if err != nil {
		return nil, err
	}
	if current != nil && current.config == config {
		provider.current.Store(&videoStudioR2StoreSnapshot{
			config:       current.config,
			store:        current.store,
			refreshAfter: now.Add(provider.refreshInterval),
		})
		return current.store, nil
	}
	store, err := provider.buildStore(ctx, config)
	if err != nil {
		return nil, err
	}
	provider.current.Store(&videoStudioR2StoreSnapshot{
		config:       config,
		store:        store,
		refreshAfter: provider.now().Add(provider.refreshInterval),
	})
	return store, nil
}

// VideoStudioR2AssetStore returns the process-wide R2 store for Video Studio.
func VideoStudioR2AssetStore(ctx context.Context) (*S3VideoAssetStore, error) {
	return videoStudioR2Stores.get(ctx)
}
