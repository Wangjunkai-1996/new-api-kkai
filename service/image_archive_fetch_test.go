package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPImageArchiveFetcherValidatesBase64Image(t *testing.T) {
	payload := imageArchiveTestPNG(t, 3, 2)
	fetcher := NewHTTPImageArchiveFetcher(t.TempDir())
	fetcher.availableBytes = func(string) (uint64, error) { return math.MaxUint64, nil }

	archive, err := fetcher.FetchBase64(base64.StdEncoding.EncodeToString(payload), 1<<20, 100)
	require.NoError(t, err)
	t.Cleanup(archive.Remove)
	assert.Equal(t, "image/png", archive.MIMEType)
	assert.Equal(t, 3, archive.Width)
	assert.Equal(t, 2, archive.Height)
	assert.Equal(t, int64(len(payload)), archive.SizeBytes)
	assert.Len(t, archive.SHA256, 64)
}

func TestHTTPImageArchiveFetcherAllowsPaddedPayloadAtExactByteLimit(t *testing.T) {
	payload := imageArchiveTestPNG(t, 3, 2)
	encoded := base64.StdEncoding.EncodeToString(payload)
	require.Greater(t, base64.StdEncoding.DecodedLen(len(encoded)), len(payload))
	fetcher := NewHTTPImageArchiveFetcher(t.TempDir())
	fetcher.availableBytes = func(string) (uint64, error) { return math.MaxUint64, nil }

	archive, err := fetcher.FetchBase64(encoded, int64(len(payload)), 100)
	require.NoError(t, err)
	t.Cleanup(archive.Remove)
	assert.Equal(t, int64(len(payload)), archive.SizeBytes)
}

func TestHTTPImageArchiveFetcherRejectsMIMEConfusionAndPixelOverflow(t *testing.T) {
	payload := imageArchiveTestPNG(t, 3, 2)
	fetcher := NewHTTPImageArchiveFetcher(t.TempDir())
	fetcher.availableBytes = func(string) (uint64, error) { return math.MaxUint64, nil }

	_, err := fetcher.writeImageFile(bytes.NewReader(payload), "image/jpeg", 1<<20, 100)
	require.ErrorIs(t, err, ErrImageArchiveMIMERejected)

	_, err = fetcher.FetchBase64(base64.StdEncoding.EncodeToString(payload), 1<<20, 5)
	require.ErrorIs(t, err, ErrImageArchivePixelsExceeded)
}

func TestHTTPImageArchiveFetcherRejectsUnsafeURLBeforeRequest(t *testing.T) {
	fetcher := NewHTTPImageArchiveFetcher(t.TempDir())
	fetcher.availableBytes = func(string) (uint64, error) { return math.MaxUint64, nil }

	_, err := fetcher.FetchURL(context.Background(), "http://127.0.0.1/private.png", 1<<20, 100)
	require.ErrorIs(t, err, ErrImageArchiveSourceRejected)
}

func imageArchiveTestPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})
	var output bytes.Buffer
	require.NoError(t, png.Encode(&output, canvas))
	return output.Bytes()
}
