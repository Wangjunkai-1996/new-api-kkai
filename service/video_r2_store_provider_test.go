package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/stretchr/testify/require"
)

func testVideoStudioR2Config(bucket string) video_studio_setting.R2Config {
	return video_studio_setting.R2Config{
		Endpoint:        "https://storage.example",
		Region:          "auto",
		Bucket:          bucket,
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}
}

func TestVideoStudioR2StoreProviderBuildsOnceForConcurrentRequests(t *testing.T) {
	config := testVideoStudioR2Config("bucket-a")
	now := time.Unix(100, 0)
	var loadCalls atomic.Int32
	var buildCalls atomic.Int32
	provider := newVideoStudioR2StoreProvider(
		func() (video_studio_setting.R2Config, error) {
			loadCalls.Add(1)
			return config, nil
		},
		func(context.Context, video_studio_setting.R2Config) (*S3VideoAssetStore, error) {
			buildCalls.Add(1)
			return &S3VideoAssetStore{}, nil
		},
		func() time.Time { return now },
		time.Minute,
	)

	const requestCount = 16
	stores := make(chan *S3VideoAssetStore, requestCount)
	errorsByRequest := make(chan error, requestCount)
	var requests sync.WaitGroup
	requests.Add(requestCount)
	for range requestCount {
		go func() {
			defer requests.Done()
			store, err := provider.get(context.Background())
			stores <- store
			errorsByRequest <- err
		}()
	}
	requests.Wait()
	close(stores)
	close(errorsByRequest)

	for err := range errorsByRequest {
		require.NoError(t, err)
	}
	var first *S3VideoAssetStore
	for store := range stores {
		if first == nil {
			first = store
			continue
		}
		require.Same(t, first, store)
	}
	require.NotNil(t, first)
	require.EqualValues(t, 1, loadCalls.Load())
	require.EqualValues(t, 1, buildCalls.Load())
}

func TestVideoStudioR2StoreProviderRefreshesOnlyAfterCompleteBuild(t *testing.T) {
	config := testVideoStudioR2Config("bucket-a")
	now := time.Unix(100, 0)
	var buildErr error
	var buildCalls int
	provider := newVideoStudioR2StoreProvider(
		func() (video_studio_setting.R2Config, error) { return config, nil },
		func(context.Context, video_studio_setting.R2Config) (*S3VideoAssetStore, error) {
			buildCalls++
			if buildErr != nil {
				return nil, buildErr
			}
			return &S3VideoAssetStore{}, nil
		},
		func() time.Time { return now },
		time.Minute,
	)

	first, err := provider.get(context.Background())
	require.NoError(t, err)
	now = now.Add(time.Minute)
	unchanged, err := provider.get(context.Background())
	require.NoError(t, err)
	require.Same(t, first, unchanged)
	require.Equal(t, 1, buildCalls)

	config = testVideoStudioR2Config("bucket-b")
	now = now.Add(time.Minute)
	buildErr = errors.New("build failed")
	failed, err := provider.get(context.Background())
	require.Nil(t, failed)
	require.ErrorIs(t, err, buildErr)
	require.Same(t, first, provider.current.Load().store)

	buildErr = nil
	second, err := provider.get(context.Background())
	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.Same(t, second, provider.current.Load().store)
	require.Equal(t, 3, buildCalls)
}
