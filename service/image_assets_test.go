package service

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type imageAssetSigningStore struct {
	key        string
	filename   string
	attachment bool
}

func (store *imageAssetSigningStore) PresignDownload(
	_ context.Context,
	key string,
	filename string,
	attachment bool,
	_ time.Duration,
) (string, error) {
	store.key = key
	store.filename = filename
	store.attachment = attachment
	return "https://assets.example.test/signed", nil
}

func (*imageAssetSigningStore) Get(context.Context, string) (ImageAssetObject, error) {
	return ImageAssetObject{}, nil
}

func (*imageAssetSigningStore) Put(context.Context, string, string, io.Reader, int64) error {
	return nil
}

func (*imageAssetSigningStore) Delete(context.Context, []string) error {
	return nil
}

func TestSignAuthorizedImageAssetUsesDownloadableFilename(t *testing.T) {
	tests := []struct {
		name             string
		mimeType         string
		originalFilename string
		expectedSuffix   string
		expectedFilename string
	}{
		{name: "PNG fallback", mimeType: "image/png", expectedSuffix: ".png"},
		{name: "JPEG fallback", mimeType: "image/jpeg", expectedSuffix: ".jpg"},
		{name: "WebP fallback", mimeType: "image/webp", expectedSuffix: ".webp"},
		{
			name: "existing filename", mimeType: "image/png", originalFilename: "finished-artwork.png",
			expectedFilename: "finished-artwork.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newImageLibraryTestDB(t)
			generation := createImageLibraryGeneration(t, db, 7, tt.name)
			now := time.Now().Unix()
			asset := model.KKAIImageAsset{
				GenerationID: &generation.ID, OwnerUserID: 7, Scope: model.ImageAssetScopeUser,
				Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
				ObjectKey: "image/original", ThumbnailState: model.ImageThumbnailStatePending,
				OriginalFilename: tt.originalFilename, MIMEType: tt.mimeType,
				SizeBytes: 10, Width: 1, Height: 1, CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&asset).Error)
			store := &imageAssetSigningStore{}

			location, err := SignAuthorizedImageAsset(
				context.Background(), db, store, 7, false, asset.ID, false, true,
			)
			require.NoError(t, err)
			assert.Equal(t, "https://assets.example.test/signed", location)
			assert.Equal(t, asset.ObjectKey, store.key)
			assert.True(t, store.attachment)
			if tt.expectedFilename != "" {
				assert.Equal(t, tt.expectedFilename, store.filename)
			} else {
				assert.Equal(t, "image-"+strconv.FormatInt(asset.ID, 10)+tt.expectedSuffix, store.filename)
			}
		})
	}
}
