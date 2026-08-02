package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/image/webp"
)

var (
	ErrImageArchiveSourceRejected       = errors.New("image archive source is not allowed")
	ErrImageArchiveResponseRejected     = errors.New("image archive source returned an invalid response")
	ErrImageArchiveMIMERejected         = errors.New("image archive source returned an unsupported media type")
	ErrImageArchiveTooLarge             = errors.New("image archive source exceeds the configured size limit")
	ErrImageArchivePixelsExceeded       = errors.New("image archive source exceeds the configured pixel limit")
	ErrImageTemporaryStorageUnavailable = errors.New("image temporary storage is unavailable")
)

type ImageArchiveSourceFetcher interface {
	FetchURL(context.Context, string, int64, int64) (*FetchedImageArchive, error)
	FetchBase64(string, int64, int64) (*FetchedImageArchive, error)
}

type ImageArchiveUploadValidator interface {
	Ingest(io.Reader, string, int64, int64) (*FetchedImageArchive, error)
}

type FetchedImageArchive struct {
	Path      string
	MIMEType  string
	SizeBytes int64
	Width     int
	Height    int
	SHA256    string
}

func (archive *FetchedImageArchive) Remove() {
	if archive == nil || archive.Path == "" {
		return
	}
	_ = os.Remove(archive.Path)
	archive.Path = ""
}

type HTTPImageArchiveFetcher struct {
	client         *http.Client
	tempDir        string
	availableBytes func(string) (uint64, error)
	validateURL    func(context.Context, *url.URL) error
}

func NewHTTPImageArchiveFetcher(tempDir string) *HTTPImageArchiveFetcher {
	foundation := NewHTTPVideoArchiveFetcher(tempDir)
	clientCopy := *foundation.client
	fetcher := &HTTPImageArchiveFetcher{
		client:         &clientCopy,
		tempDir:        strings.TrimSpace(tempDir),
		availableBytes: videoTemporaryAvailableBytes,
		validateURL:    validateStrictVideoArchiveURL,
	}
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return ErrImageArchiveSourceRejected
		}
		if err := fetcher.validateURL(request.Context(), request.URL); err != nil {
			return ErrImageArchiveSourceRejected
		}
		return nil
	}
	return fetcher
}

func (fetcher *HTTPImageArchiveFetcher) FetchURL(
	ctx context.Context,
	source string,
	maxBytes int64,
	maxPixels int64,
) (*FetchedImageArchive, error) {
	if strings.HasPrefix(strings.TrimSpace(source), "data:") {
		return fetcher.fetchDataURL(source, maxBytes, maxPixels)
	}
	if fetcher == nil || fetcher.client == nil || fetcher.availableBytes == nil ||
		fetcher.validateURL == nil || maxBytes <= 0 || maxPixels <= 0 {
		return nil, ErrImageArchiveSourceRejected
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(source))
	if err != nil || parsed == nil || !parsed.IsAbs() {
		return nil, ErrImageArchiveSourceRejected
	}
	if err := fetcher.validateURL(ctx, parsed); err != nil {
		if errors.Is(err, ErrVideoArchiveSourceRejected) {
			return nil, ErrImageArchiveSourceRejected
		}
		return nil, fmt.Errorf("validate image archive source: %w", err)
	}
	if err := fetcher.ensureTemporaryCapacity(maxBytes); err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, ErrImageArchiveSourceRejected
	}
	request.Header.Set("Accept", "image/jpeg,image/png,image/webp")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch image archive source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrImageArchiveResponseRejected
	}
	if response.ContentLength > maxBytes {
		return nil, ErrImageArchiveTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !isSupportedImageMIME(strings.ToLower(mediaType)) {
		return nil, ErrImageArchiveMIMERejected
	}
	return fetcher.writeImageFile(response.Body, strings.ToLower(mediaType), maxBytes, maxPixels)
}

func (fetcher *HTTPImageArchiveFetcher) FetchBase64(
	payload string,
	maxBytes int64,
	maxPixels int64,
) (*FetchedImageArchive, error) {
	if fetcher == nil || fetcher.availableBytes == nil || maxBytes <= 0 || maxPixels <= 0 {
		return nil, ErrImageArchiveSourceRejected
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || base64DecodedLengthUpperBoundExceedsLimit(payload, maxBytes) {
		return nil, ErrImageArchiveTooLarge
	}
	if err := fetcher.ensureTemporaryCapacity(maxBytes); err != nil {
		return nil, err
	}
	return fetcher.writeImageFile(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)), "", maxBytes, maxPixels)
}

func (fetcher *HTTPImageArchiveFetcher) Ingest(
	reader io.Reader,
	declaredMIME string,
	maxBytes int64,
	maxPixels int64,
) (*FetchedImageArchive, error) {
	declaredMIME = strings.ToLower(strings.TrimSpace(strings.Split(declaredMIME, ";")[0]))
	if fetcher == nil || fetcher.availableBytes == nil || reader == nil || maxBytes <= 0 || maxPixels <= 0 ||
		!isSupportedImageMIME(declaredMIME) {
		return nil, ErrImageArchiveMIMERejected
	}
	if err := fetcher.ensureTemporaryCapacity(maxBytes); err != nil {
		return nil, err
	}
	return fetcher.writeImageFile(reader, declaredMIME, maxBytes, maxPixels)
}

func (fetcher *HTTPImageArchiveFetcher) fetchDataURL(
	source string,
	maxBytes int64,
	maxPixels int64,
) (*FetchedImageArchive, error) {
	header, payload, found := strings.Cut(strings.TrimSpace(source), ",")
	if !found || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return nil, ErrImageArchiveSourceRejected
	}
	mediaType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	mediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil || !isSupportedImageMIME(strings.ToLower(mediaType)) {
		return nil, ErrImageArchiveMIMERejected
	}
	if base64DecodedLengthUpperBoundExceedsLimit(payload, maxBytes) {
		return nil, ErrImageArchiveTooLarge
	}
	if err := fetcher.ensureTemporaryCapacity(maxBytes); err != nil {
		return nil, err
	}
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))
	return fetcher.writeImageFile(decoder, strings.ToLower(mediaType), maxBytes, maxPixels)
}

// DecodedLen is an upper bound for padded base64 and can exceed the actual
// decoded length by two bytes. The streaming writer remains the exact limit.
func base64DecodedLengthUpperBoundExceedsLimit(payload string, maxBytes int64) bool {
	return int64(base64.StdEncoding.DecodedLen(len(payload))) > maxBytes+2
}

func (fetcher *HTTPImageArchiveFetcher) ensureTemporaryCapacity(maxBytes int64) error {
	available, err := fetcher.availableBytes(fetcher.tempDir)
	if err != nil || available < uint64(maxBytes)+videoTemporaryStorageReserveBytes {
		return ErrImageTemporaryStorageUnavailable
	}
	return nil
}

func (fetcher *HTTPImageArchiveFetcher) writeImageFile(
	reader io.Reader,
	declaredMIME string,
	maxBytes int64,
	maxPixels int64,
) (*FetchedImageArchive, error) {
	file, err := os.CreateTemp(fetcher.tempDir, "new-api-image-archive-*")
	if err != nil {
		return nil, ErrImageTemporaryStorageUnavailable
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, ErrImageArchiveResponseRejected
	}
	if written <= 0 {
		return nil, ErrImageArchiveResponseRejected
	}
	if written > maxBytes {
		return nil, ErrImageArchiveTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrImageArchiveResponseRejected
	}
	header := make([]byte, 512)
	headerSize, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, ErrImageArchiveResponseRejected
	}
	detectedMIME := detectSupportedImageMIME(header[:headerSize])
	if detectedMIME == "" || (declaredMIME != "" && declaredMIME != detectedMIME) {
		return nil, ErrImageArchiveMIMERejected
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrImageArchiveResponseRejected
	}
	width, height, err := decodeImageDimensions(file, detectedMIME)
	if err != nil || width <= 0 || height <= 0 {
		return nil, ErrImageArchiveResponseRejected
	}
	if int64(width) > maxPixels/int64(height) {
		return nil, ErrImageArchivePixelsExceeded
	}
	if err := file.Close(); err != nil {
		return nil, ErrImageArchiveResponseRejected
	}
	remove = false
	return &FetchedImageArchive{
		Path: path, MIMEType: detectedMIME, SizeBytes: written, Width: width, Height: height,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func isSupportedImageMIME(mediaType string) bool {
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func detectSupportedImageMIME(header []byte) string {
	if len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP" {
		return "image/webp"
	}
	mediaType := http.DetectContentType(header)
	if isSupportedImageMIME(mediaType) {
		return mediaType
	}
	return ""
}

func decodeImageDimensions(reader io.Reader, mediaType string) (int, int, error) {
	switch mediaType {
	case "image/jpeg":
		config, err := jpeg.DecodeConfig(reader)
		return config.Width, config.Height, err
	case "image/png":
		config, err := png.DecodeConfig(reader)
		return config.Width, config.Height, err
	case "image/webp":
		config, err := webp.DecodeConfig(reader)
		return config.Width, config.Height, err
	default:
		return 0, 0, ErrImageArchiveMIMERejected
	}
}
