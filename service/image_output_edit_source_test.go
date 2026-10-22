package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imageOutputEditSourceStore struct {
	imageAssetSigningStore
	get func(context.Context, string) (ImageAssetObject, error)
}

func (store *imageOutputEditSourceStore) Get(ctx context.Context, key string) (ImageAssetObject, error) {
	return store.get(ctx, key)
}

type imageOutputReadCloser struct {
	io.Reader
	closed atomic.Bool
}

func (reader *imageOutputReadCloser) Close() error {
	reader.closed.Store(true)
	return nil
}

func imageOutputEditSourceFixture(t *testing.T) (*gorm.DB, model.KKAIImageGeneration, model.KKAIImageAsset, []byte) {
	t.Helper()
	db := newImageLibraryTestDB(t)
	generation := createImageLibraryGeneration(t, db, 7, "edit source")
	payload := imageArchiveTestPNG(t, 3, 2)
	digest := sha256.Sum256(payload)
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 7, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "private/original", ThumbnailObjectKey: "private/thumbnail",
		MIMEType: "image/png", SizeBytes: int64(len(payload)), Width: 3, Height: 2,
		SHA256: hex.EncodeToString(digest[:]), OriginalFilename: "../../untrusted\r\nfile.png",
	}
	require.NoError(t, db.Create(&asset).Error)
	return db, generation, asset, payload
}

func TestImageOutputEditSourceReturnsValidatedOriginalWithoutWrites(t *testing.T) {
	db, generation, asset, payload := imageOutputEditSourceFixture(t)
	tempDir := t.TempDir()
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
	reader := &imageOutputReadCloser{Reader: bytes.NewReader(payload)}
	store := &imageOutputEditSourceStore{get: func(_ context.Context, key string) (ImageAssetObject, error) {
		assert.Equal(t, asset.ObjectKey, key)
		return ImageAssetObject{Body: reader, ContentType: "image/png; charset=binary", ContentLength: int64(len(payload))}, nil
	}}

	archive, filename, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
	require.NoError(t, err)
	t.Cleanup(archive.Remove)
	assert.True(t, reader.closed.Load())
	assert.Equal(t, fmt.Sprintf("image-%d.png", asset.ID), filename)
	body, err := os.ReadFile(archive.Path)
	require.NoError(t, err)
	assert.Equal(t, payload, body)
	assert.Equal(t, asset.SHA256, archive.SHA256)
	var persistedAsset model.KKAIImageAsset
	var persistedGeneration model.KKAIImageGeneration
	require.NoError(t, db.First(&persistedAsset, asset.ID).Error)
	require.NoError(t, db.First(&persistedGeneration, generation.ID).Error)
	assert.Equal(t, asset, persistedAsset)
	assert.Equal(t, generation, persistedGeneration)
	archive.Remove()
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestImageOutputEditSourceAuthorization(t *testing.T) {
	for _, test := range []struct {
		name       string
		asset      map[string]any
		generation map[string]any
		expected   error
	}{
		{name: "other owner", asset: map[string]any{"owner_user_id": 8}, expected: ErrImageAssetAccessDenied},
		{name: "catalog", asset: map[string]any{"scope": model.ImageAssetScopeCatalog}, expected: ErrImageAssetAccessDenied},
		{name: "reference", asset: map[string]any{"kind": model.ImageAssetKindReference}, expected: ErrImageAssetAccessDenied},
		{name: "not ready", asset: map[string]any{"state": model.ImageAssetStateStaging}, expected: ErrImageAssetNotFound},
		{name: "deleted asset", asset: map[string]any{"deleted_at": 1}, expected: ErrImageAssetNotFound},
		{name: "missing generation", asset: map[string]any{"generation_id": nil}, expected: ErrImageAssetAccessDenied},
		{name: "other generation owner", generation: map[string]any{"user_id": 8}, expected: ErrImageGenerationNotFound},
		{name: "unsettled", generation: map[string]any{"billing_state": model.ImageGenerationBillingStatePending}, expected: ErrImageAssetNotFound},
		{name: "failed", generation: map[string]any{"status": model.ImageGenerationStatusFailed}, expected: ErrImageAssetNotFound},
		{name: "submitting", generation: map[string]any{"status": model.ImageGenerationStatusSubmitting}, expected: ErrImageAssetNotFound},
		{name: "deleted generation", generation: map[string]any{"deleted_at": 1}, expected: ErrImageAssetNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, generation, asset, _ := imageOutputEditSourceFixture(t)
			if test.asset != nil {
				require.NoError(t, db.Model(&asset).Updates(test.asset).Error)
			}
			if test.generation != nil {
				require.NoError(t, db.Model(&generation).Updates(test.generation).Error)
			}
			store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
				t.Fatal("unauthorized source must not reach object storage")
				return ImageAssetObject{}, nil
			}}
			archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
			require.ErrorIs(t, err, test.expected)
			assert.Nil(t, archive)
		})
	}
}

func TestImageOutputEditSourceAcceptsSettledPartial(t *testing.T) {
	db, generation, asset, payload := imageOutputEditSourceFixture(t)
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", t.TempDir())
	require.NoError(t, db.Model(&generation).Update("status", model.ImageGenerationStatusPartial).Error)
	store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
		return ImageAssetObject{Body: io.NopCloser(bytes.NewReader(payload)), ContentType: "image/png", ContentLength: -1}, nil
	}}
	archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
	require.NoError(t, err)
	archive.Remove()
}

func TestImageOutputEditSourceRejectsCorruptAndOversizedObjects(t *testing.T) {
	settings := image_studio_setting.Get()
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.max_reference_bytes": "256"}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.max_reference_bytes": fmt.Sprint(settings.MaxReferenceBytes)}))
	})
	for _, test := range []struct {
		name     string
		payload  func([]byte) []byte
		mime     string
		length   int64
		asset    map[string]any
		expected error
	}{
		{name: "actual bytes over limit with forged length", payload: func(data []byte) []byte { return append(data, make([]byte, 257)...) }, mime: "image/png", length: 1, expected: ErrImageArchiveTooLarge},
		{name: "declared bytes over limit", mime: "image/png", length: 257, expected: ErrImageArchiveTooLarge},
		{name: "object MIME mismatch", mime: "image/jpeg", length: -1, expected: ErrImageArchiveMIMERejected},
		{name: "magic MIME mismatch", payload: func([]byte) []byte { return []byte("this is not an image") }, mime: "image/png", length: -1, expected: ErrImageArchiveMIMERejected},
		{name: "object short read", mime: "image/png", length: 255, expected: ErrImageOutputEditSourceUnavailable},
		{name: "database size mismatch", mime: "image/png", length: -1, asset: map[string]any{"size_bytes": 254}, expected: ErrImageArchiveResponseRejected},
		{name: "hash mismatch", mime: "image/png", length: -1, asset: map[string]any{"sha256": strings.Repeat("a", 64)}, expected: ErrImageArchiveResponseRejected},
		{name: "dimensions mismatch", mime: "image/png", length: -1, asset: map[string]any{"width": 4}, expected: ErrImageArchiveResponseRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _, asset, payload := imageOutputEditSourceFixture(t)
			tempDir := t.TempDir()
			t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
			if test.asset != nil {
				require.NoError(t, db.Model(&asset).Updates(test.asset).Error)
			}
			if test.payload != nil {
				payload = test.payload(payload)
			}
			reader := &imageOutputReadCloser{Reader: bytes.NewReader(payload)}
			store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
				return ImageAssetObject{Body: reader, ContentType: test.mime, ContentLength: test.length}, nil
			}}
			archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
			require.ErrorIs(t, err, test.expected)
			assert.Nil(t, archive)
			assert.True(t, reader.closed.Load())
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestImageOutputEditSourceNormalizesObjectErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		expected error
	}{
		{"missing sentinel", ErrVideoAssetObjectNotFound, ErrImageAssetNotFound},
		{"nested S3 missing", fmt.Errorf("private object-key: %w", &smithy.GenericAPIError{Code: "NoSuchKey"}), ErrImageAssetNotFound},
		{"storage error", errors.New("private object-key and credentials"), ErrImageOutputEditSourceUnavailable},
		{"read timeout", fmt.Errorf("private object-key and endpoint: %w", context.DeadlineExceeded), ErrImageOutputEditSourceTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _, asset, _ := imageOutputEditSourceFixture(t)
			store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
				return ImageAssetObject{}, test.err
			}}
			archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
			require.ErrorIs(t, err, test.expected)
			assert.Nil(t, archive)
			assert.NotContains(t, err.Error(), "private")
		})
	}
}

func TestImageOutputEditSourceAppliesOutputAndActualPixelLimits(t *testing.T) {
	rawSetting := config.GlobalConfig.Get("image_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(rawSetting, original)) })
	for _, test := range []struct {
		name     string
		limits   map[string]string
		asset    map[string]any
		append   int
		expected error
	}{
		{name: "output byte limit", limits: map[string]string{"max_output_bytes": "256"}, append: 257, expected: ErrImageArchiveTooLarge},
		{name: "actual pixel limit despite smaller metadata", limits: map[string]string{"max_pixels": "1"}, asset: map[string]any{"width": 1, "height": 1}, expected: ErrImageArchivePixelsExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
			require.NoError(t, config.UpdateConfigFromMap(rawSetting, test.limits))
			db, _, asset, payload := imageOutputEditSourceFixture(t)
			tempDir := t.TempDir()
			t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
			if test.asset != nil {
				require.NoError(t, db.Model(&asset).Updates(test.asset).Error)
			}
			payload = append(payload, make([]byte, test.append)...)
			store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
				return ImageAssetObject{Body: io.NopCloser(bytes.NewReader(payload)), ContentType: "image/png", ContentLength: -1}, nil
			}}
			archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
			require.ErrorIs(t, err, test.expected)
			assert.Nil(t, archive)
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

type imageOutputFailedReader struct{}

func (imageOutputFailedReader) Read([]byte) (int, error) {
	return 0, errors.New("private object read failed")
}

func TestImageOutputEditSourceMidReadFailureCleansTemp(t *testing.T) {
	db, _, asset, payload := imageOutputEditSourceFixture(t)
	tempDir := t.TempDir()
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
	reader := &imageOutputReadCloser{Reader: io.MultiReader(bytes.NewReader(payload[:len(payload)/2]), imageOutputFailedReader{})}
	store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
		return ImageAssetObject{Body: reader, ContentType: "image/png", ContentLength: asset.SizeBytes}, nil
	}}
	archive, _, err := ReadImageOutputEditSource(context.Background(), db, store, 7, asset.ID)
	require.ErrorIs(t, err, ErrImageOutputEditSourceUnavailable)
	assert.NotContains(t, err.Error(), "private")
	assert.Nil(t, archive)
	assert.True(t, reader.closed.Load())
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

type imageOutputBlockingReader struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (reader *imageOutputBlockingReader) Read([]byte) (int, error) {
	close(reader.started)
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *imageOutputBlockingReader) Close() error {
	reader.once.Do(func() { close(reader.closed) })
	return nil
}

func TestImageOutputEditSourceCancellationInterruptsReadAndCleansTemp(t *testing.T) {
	db, _, asset, _ := imageOutputEditSourceFixture(t)
	tempDir := t.TempDir()
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &imageOutputBlockingReader{started: make(chan struct{}), closed: make(chan struct{})}
	store := &imageOutputEditSourceStore{get: func(context.Context, string) (ImageAssetObject, error) {
		return ImageAssetObject{Body: reader, ContentType: "image/png", ContentLength: asset.SizeBytes}, nil
	}}
	go func() {
		select {
		case <-reader.started:
			cancel()
		case <-ctx.Done():
		}
	}()
	archive, _, err := ReadImageOutputEditSource(ctx, db, store, 7, asset.ID)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, archive)
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
