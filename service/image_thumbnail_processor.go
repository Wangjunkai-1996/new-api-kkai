package service

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

var errImageThumbnailRejected = errors.New("image thumbnail source is invalid")

const imageThumbnailMaximumBytes int64 = 120 * 1024

type rasterImageThumbnailProcessor struct{}

func (rasterImageThumbnailProcessor) CreateImageThumbnail(
	ctx context.Context,
	inputPath string,
	outputPath string,
	maxBytes int64,
) error {
	inputPath = filepath.Clean(strings.TrimSpace(inputPath))
	outputPath = filepath.Clean(strings.TrimSpace(outputPath))
	if ctx == nil || inputPath == "." || outputPath == "." || inputPath == outputPath || maxBytes <= 0 {
		return errImageThumbnailRejected
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open image thumbnail source: %w", err)
	}
	defer input.Close()

	header := make([]byte, 512)
	headerSize, err := io.ReadFull(input, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("read image thumbnail source: %w", err)
	}
	mimeType := detectSupportedImageMIME(header[:headerSize])
	if mimeType == "" {
		return errImageThumbnailRejected
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind image thumbnail source: %w", err)
	}
	sourceWidth, sourceHeight, err := decodeImageDimensions(input, mimeType)
	maxPixels := image_studio_setting.Get().MaxPixels
	if err != nil || sourceWidth <= 0 || sourceHeight <= 0 || maxPixels <= 0 ||
		int64(sourceWidth) > maxPixels/int64(sourceHeight) {
		return errImageThumbnailRejected
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind image thumbnail source: %w", err)
	}

	var source image.Image
	switch mimeType {
	case "image/jpeg":
		source, err = jpeg.Decode(input)
	case "image/png":
		source, err = png.Decode(input)
	case "image/webp":
		source, err = webp.Decode(input)
	}
	if err != nil || source == nil {
		return fmt.Errorf("%w: decode source", errImageThumbnailRejected)
	}
	sourceBounds := source.Bounds()
	if sourceBounds.Dx() != sourceWidth || sourceBounds.Dy() != sourceHeight {
		return errImageThumbnailRejected
	}

	attempts := []struct {
		maxDimension int
		quality      int
	}{{maxDimension: 960, quality: 85}, {maxDimension: 720, quality: 75}, {maxDimension: 480, quality: 65}}
	for _, attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		width, height := sourceWidth, sourceHeight
		if width > attempt.maxDimension || height > attempt.maxDimension {
			if width >= height {
				height = max(1, int((int64(height)*int64(attempt.maxDimension)+int64(width)/2)/int64(width)))
				width = attempt.maxDimension
			} else {
				width = max(1, int((int64(width)*int64(attempt.maxDimension)+int64(height)/2)/int64(height)))
				height = attempt.maxDimension
			}
		}
		thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, sourceBounds, draw.Over, nil)

		output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("open image thumbnail output: %w", err)
		}
		encodeErr := jpeg.Encode(output, thumbnail, &jpeg.Options{Quality: attempt.quality})
		closeErr := output.Close()
		if encodeErr != nil || closeErr != nil {
			_ = os.Remove(outputPath)
			return fmt.Errorf("encode image thumbnail: %w", errors.Join(encodeErr, closeErr))
		}
		info, err := os.Stat(outputPath)
		if err != nil {
			return fmt.Errorf("inspect image thumbnail output: %w", err)
		}
		if info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxBytes {
			return nil
		}
	}
	_ = os.Remove(outputPath)
	return fmt.Errorf("%w: output exceeds size limit", errImageThumbnailRejected)
}
