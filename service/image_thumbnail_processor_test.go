package service

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRasterImageThumbnailProcessorCreatesBoundedJPEGFromPNG(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "source.png")
	outputPath := filepath.Join(tempDir, "thumbnail.jpg")
	source := image.NewRGBA(image.Rect(0, 0, 1254, 1254))
	for y := 0; y < source.Bounds().Dy(); y++ {
		for x := 0; x < source.Bounds().Dx(); x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	input, err := os.OpenFile(inputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)
	require.NoError(t, png.Encode(input, source))
	require.NoError(t, input.Close())

	require.NoError(t, (rasterImageThumbnailProcessor{}).CreateImageThumbnail(
		context.Background(), inputPath, outputPath, imageThumbnailMaximumBytes,
	))
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	require.Positive(t, info.Size())
	require.LessOrEqual(t, info.Size(), imageThumbnailMaximumBytes)
	output, err := os.Open(outputPath)
	require.NoError(t, err)
	config, err := jpeg.DecodeConfig(output)
	require.NoError(t, err)
	require.NoError(t, output.Close())
	require.LessOrEqual(t, config.Width, 960)
	require.LessOrEqual(t, config.Height, 960)
}

func TestRasterImageThumbnailProcessorRejectsInvalidImage(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "source.png")
	outputPath := filepath.Join(tempDir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(inputPath, []byte("not-an-image"), 0o600))

	err := (rasterImageThumbnailProcessor{}).CreateImageThumbnail(
		context.Background(), inputPath, outputPath, imageThumbnailMaximumBytes,
	)
	require.ErrorIs(t, err, errImageThumbnailRejected)
}
