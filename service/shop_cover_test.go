package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/draw"
)

type failingShopCoverStore struct {
	ImageAssetStore
	err error
}

func (store failingShopCoverStore) Get(context.Context, string) (ImageAssetObject, error) {
	return ImageAssetObject{}, store.err
}

func TestShopCoverUploadNormalizesPixelsAndKeepsStablePublicURL(t *testing.T) {
	store := newMemoryVideoAssetStore()
	canvas := image.NewNRGBA(image.Rect(0, 0, 2000, 1000))
	draw.Draw(canvas, image.Rect(0, 0, 1000, 1000), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, canvas))
	input.WriteString("private-file-metadata-that-must-not-be-published")
	cover, err := SaveShopCover(context.Background(), store, bytes.NewReader(input.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, 1600, cover.Width)
	assert.Equal(t, 800, cover.Height)
	require.Regexp(t, `^/api/shop/covers/[a-f0-9]{64}\.jpg$`, cover.URL)

	object, err := ReadShopCover(context.Background(), store, strings.TrimPrefix(cover.URL, "/api/shop/covers/"))
	require.NoError(t, err)
	defer object.Body.Close()
	assert.Equal(t, "image/jpeg", object.ContentType)
	content, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "private-file-metadata")
	decoded, err := jpeg.Decode(bytes.NewReader(content))
	require.NoError(t, err)
	red := color.NRGBAModel.Convert(decoded.At(100, 100)).(color.NRGBA)
	assert.Greater(t, red.R, uint8(240))
	assert.Less(t, red.G, uint8(20))
	assert.Less(t, red.B, uint8(20))
	white := color.NRGBAModel.Convert(decoded.At(1500, 100)).(color.NRGBA)
	assert.Greater(t, white.R, uint8(240))
	assert.Greater(t, white.G, uint8(240))
	assert.Greater(t, white.B, uint8(240), "transparent pixels must not become black")
	second, err := SaveShopCover(context.Background(), store, bytes.NewReader(input.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, cover, second, "retries return the same immutable image URL")
	assert.Len(t, store.objects, 1)
}

func TestShopCoverRejectsUntrustedImagesBeforeStoring(t *testing.T) {
	var truncated, oversizedDimensions bytes.Buffer
	require.NoError(t, png.Encode(&truncated, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	require.NoError(t, png.Encode(&oversizedDimensions, image.NewGray(image.Rect(0, 0, 4097, 1))))
	for _, tc := range []struct {
		name string
		data []byte
		err  error
	}{
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), ErrInvalidShopCover},
		{"truncated-png", truncated.Bytes()[:33], ErrInvalidShopCover},
		{"dimensions", oversizedDimensions.Bytes(), ErrInvalidShopCover},
		{"size", bytes.Repeat([]byte{1}, MaxShopCoverBytes+1), ErrShopCoverTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryVideoAssetStore()
			cover, err := SaveShopCover(context.Background(), store, bytes.NewReader(tc.data))
			assert.ErrorIs(t, err, tc.err)
			assert.Nil(t, cover)
			assert.Empty(t, store.objects)
		})
	}
}

func TestShopCoverPublicReadCannotResolvePrivateObjectKeys(t *testing.T) {
	for _, filename := range []string{"image-studio/users/1/source", "../private.jpg", strings.Repeat("a", 64) + ".png", strings.Repeat("a", 64) + ".jpg/..", "%2e%2e%2fprivate"} {
		t.Run(filename, func(t *testing.T) {
			_, err := ReadShopCover(context.Background(), nil, filename)
			assert.ErrorIs(t, err, ErrVideoAssetObjectNotFound)
		})
	}
	store := newMemoryVideoAssetStore()
	filename := strings.Repeat("a", 64) + ".jpg"
	require.NoError(t, store.Put(context.Background(), "shop/covers/v1/"+filename, "image/svg+xml", strings.NewReader("<svg/>"), 6))
	_, err := ReadShopCover(context.Background(), store, filename)
	assert.ErrorIs(t, err, ErrVideoAssetObjectNotFound)
}

func TestShopCoverReadMapsMissingS3ObjectWithoutMaskingOtherStorageFailures(t *testing.T) {
	filename := strings.Repeat("a", 64) + ".jpg"
	missing := fmt.Errorf("get cover: %w", &smithy.GenericAPIError{Code: "NoSuchKey"})
	_, err := ReadShopCover(context.Background(), failingShopCoverStore{err: missing}, filename)
	assert.ErrorIs(t, err, ErrVideoAssetObjectNotFound)

	denied := fmt.Errorf("get cover: %w", &smithy.GenericAPIError{Code: "AccessDenied"})
	_, err = ReadShopCover(context.Background(), failingShopCoverStore{err: denied}, filename)
	assert.ErrorIs(t, err, denied)
}
