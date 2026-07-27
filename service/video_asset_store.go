package service

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrVideoAssetObjectNotFound = errors.New("video asset object not found")

type VideoAssetObject struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
}

type VideoAssetObjectMetadata struct {
	ContentType         string
	ContentLength       int64
	ETag                string
	SHA256              string
	ArchiveSourceSHA256 string
}

type VideoAssetSignedRequest struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt int64             `json:"expires_at"`
}

type VideoAssetUploadedPart struct {
	PartNumber int32  `json:"part_number"`
	SizeBytes  int64  `json:"size_bytes"`
	ETag       string `json:"etag"`
}

type VideoAssetCompletedPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type VideoAssetStore interface {
	PresignUpload(context.Context, string, string, int64, time.Duration) (VideoAssetSignedRequest, error)
	PresignDownload(context.Context, string, string, bool, time.Duration) (string, error)
	Head(context.Context, string) (VideoAssetObjectMetadata, error)
	Get(context.Context, string) (VideoAssetObject, error)
	Put(context.Context, string, string, io.Reader, int64) error
	Delete(context.Context, []string) error
}

type VideoMultipartAssetStore interface {
	VideoAssetStore
	CreateMultipartUpload(context.Context, string, string) (string, error)
	PresignUploadPart(context.Context, string, string, int32, int64, time.Duration) (VideoAssetSignedRequest, error)
	ListUploadedParts(context.Context, string, string) ([]VideoAssetUploadedPart, error)
	CompleteMultipartUpload(context.Context, string, string, []VideoAssetCompletedPart) error
	AbortMultipartUpload(context.Context, string, string) error
}

type VideoArchiveAssetStore interface {
	VideoAssetStore
	PutArchive(context.Context, string, string, io.Reader, int64, string, string) error
}
