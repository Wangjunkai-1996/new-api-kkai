package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

func inspectRasterVideoMedia(inputPath string) (VideoMediaMetadata, bool, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return VideoMediaMetadata{}, false, fmt.Errorf("%w: open media input: %v", ErrVideoMediaProcessingFailed, err)
	}
	defer input.Close()
	return inspectRasterVideoMediaReader(input)
}

func inspectRasterVideoMediaReader(input io.Reader) (VideoMediaMetadata, bool, error) {
	header := make([]byte, 512)
	headerSize, err := io.ReadFull(input, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return VideoMediaMetadata{}, false, fmt.Errorf("%w: read media header: %v", ErrVideoMediaProcessingFailed, err)
	}
	mimeType := detectSupportedImageMIME(header[:headerSize])
	if mimeType == "" {
		return VideoMediaMetadata{}, false, nil
	}
	width, height, err := decodeImageDimensions(io.MultiReader(bytes.NewReader(header[:headerSize]), input), mimeType)
	if err != nil || !validVideoMediaDimensions(width, height) {
		return VideoMediaMetadata{}, true, ErrVideoMediaInvalid
	}

	codec := ""
	switch mimeType {
	case "image/jpeg":
		codec = "mjpeg"
	case "image/png":
		codec = "png"
	case "image/webp":
		codec = "webp"
	}
	return VideoMediaMetadata{
		MIMEType: mimeType,
		Width:    width,
		Height:   height,
		Codec:    codec,
	}, true, nil
}
