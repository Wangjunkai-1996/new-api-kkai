package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

var (
	ErrInvalidVideoAssetStoreRequest = errors.New("invalid video asset store request")
	ErrVideoMultipartUploadNotFound  = errors.New("video multipart upload not found")
)

type S3VideoAssetStore struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewR2VideoAssetStore(ctx context.Context, config video_studio_setting.R2Config) (*S3VideoAssetStore, error) {
	if ctx == nil || strings.TrimSpace(config.Endpoint) == "" || strings.TrimSpace(config.Bucket) == "" ||
		strings.TrimSpace(config.AccessKeyID) == "" || strings.TrimSpace(config.SecretAccessKey) == "" {
		return nil, video_studio_setting.ErrR2NotConfigured
	}
	if _, err := url.ParseRequestURI(config.Endpoint); err != nil {
		return nil, fmt.Errorf("invalid R2 endpoint: %w", err)
	}
	awsConfig := aws.Config{
		Region:      config.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.Endpoint)
		options.UsePathStyle = true
	})
	return &S3VideoAssetStore{
		bucket:  config.Bucket,
		client:  client,
		presign: s3.NewPresignClient(client),
	}, nil
}

func NewR2VideoAssetStoreFromEnvironment(ctx context.Context) (*S3VideoAssetStore, error) {
	config, err := video_studio_setting.LoadR2Config()
	if err != nil {
		return nil, err
	}
	return NewR2VideoAssetStore(ctx, config)
}

func (s *S3VideoAssetStore) PresignUpload(ctx context.Context, key string, contentType string, contentLength int64, expires time.Duration) (VideoAssetSignedRequest, error) {
	if err := s.validateRequest(key); err != nil || strings.TrimSpace(contentType) == "" || contentLength <= 0 || expires <= 0 {
		return VideoAssetSignedRequest{}, ErrInvalidVideoAssetStoreRequest
	}
	request, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return VideoAssetSignedRequest{}, fmt.Errorf("presign video asset upload: %w", err)
	}
	headers := make(map[string]string, len(request.SignedHeader))
	for name, values := range request.SignedHeader {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	return VideoAssetSignedRequest{
		URL:       request.URL,
		Method:    http.MethodPut,
		Headers:   headers,
		ExpiresAt: time.Now().Add(expires).Unix(),
	}, nil
}

func (s *S3VideoAssetStore) PresignDownload(ctx context.Context, key string, filename string, attachment bool, expires time.Duration) (string, error) {
	if err := s.validateRequest(key); err != nil || expires <= 0 {
		return "", ErrInvalidVideoAssetStoreRequest
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if filename != "" {
		disposition := "inline"
		if attachment {
			disposition = "attachment"
		}
		input.ResponseContentDisposition = aws.String(fmt.Sprintf(`%s; filename*=UTF-8''%s`, disposition, url.PathEscape(filename)))
	}
	request, err := s.presign.PresignGetObject(ctx, input, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return "", fmt.Errorf("presign video asset download: %w", err)
	}
	return request.URL, nil
}

func (s *S3VideoAssetStore) Head(ctx context.Context, key string) (VideoAssetObjectMetadata, error) {
	if err := s.validateRequest(key); err != nil {
		return VideoAssetObjectMetadata{}, err
	}
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isVideoAssetObjectNotFound(err) {
			return VideoAssetObjectMetadata{}, ErrVideoAssetObjectNotFound
		}
		return VideoAssetObjectMetadata{}, fmt.Errorf("head video asset: %w", err)
	}
	sha256 := ""
	archiveSourceSHA256 := ""
	for key, value := range result.Metadata {
		switch {
		case strings.EqualFold(key, "sha256"):
			sha256 = strings.ToLower(strings.TrimSpace(value))
		case strings.EqualFold(key, "archive-source-sha256"):
			archiveSourceSHA256 = strings.TrimSpace(value)
		}
	}
	return VideoAssetObjectMetadata{
		ContentType:         aws.ToString(result.ContentType),
		ContentLength:       aws.ToInt64(result.ContentLength),
		ETag:                strings.Trim(aws.ToString(result.ETag), `"`),
		SHA256:              sha256,
		ArchiveSourceSHA256: archiveSourceSHA256,
	}, nil
}

func (s *S3VideoAssetStore) Get(ctx context.Context, key string) (VideoAssetObject, error) {
	if err := s.validateRequest(key); err != nil {
		return VideoAssetObject{}, err
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return VideoAssetObject{}, fmt.Errorf("get video asset: %w", err)
	}
	return VideoAssetObject{
		Body:          result.Body,
		ContentType:   aws.ToString(result.ContentType),
		ContentLength: aws.ToInt64(result.ContentLength),
		ETag:          strings.Trim(aws.ToString(result.ETag), `"`),
	}, nil
}

func (s *S3VideoAssetStore) Put(ctx context.Context, key string, contentType string, body io.Reader, contentLength int64) error {
	return s.putObject(ctx, key, contentType, body, contentLength, nil)
}

func (s *S3VideoAssetStore) PutArchive(
	ctx context.Context,
	key string,
	contentType string,
	body io.Reader,
	contentLength int64,
	sha256 string,
	archiveSourceSHA256 string,
) error {
	sha256 = strings.ToLower(strings.TrimSpace(sha256))
	archiveSourceSHA256 = strings.ToLower(strings.TrimSpace(archiveSourceSHA256))
	if !validSHA256Hex(sha256) || !validSHA256Hex(archiveSourceSHA256) {
		return ErrInvalidVideoAssetStoreRequest
	}
	return s.putObject(ctx, key, contentType, body, contentLength, map[string]string{
		"sha256":                sha256,
		"archive-source-sha256": archiveSourceSHA256,
	})
}

func (s *S3VideoAssetStore) putObject(ctx context.Context, key string, contentType string, body io.Reader, contentLength int64, metadata map[string]string) error {
	if err := s.validateRequest(key); err != nil || strings.TrimSpace(contentType) == "" || body == nil || contentLength <= 0 {
		return ErrInvalidVideoAssetStoreRequest
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
		Body:          body,
		Metadata:      metadata,
	})
	if err != nil {
		return fmt.Errorf("put video asset: %w", err)
	}
	return nil
}

func (s *S3VideoAssetStore) Delete(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := s.validateRequest(key); err != nil {
			return err
		}
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}); err != nil {
			return fmt.Errorf("delete video asset: %w", err)
		}
	}
	return nil
}

func (s *S3VideoAssetStore) CreateMultipartUpload(ctx context.Context, key string, contentType string) (string, error) {
	if err := s.validateRequest(key); err != nil || strings.TrimSpace(contentType) == "" {
		return "", ErrInvalidVideoAssetStoreRequest
	}
	result, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("create video multipart upload: %w", err)
	}
	uploadID := strings.TrimSpace(aws.ToString(result.UploadId))
	if uploadID == "" || len(uploadID) > 512 {
		return "", ErrInvalidVideoAssetStoreRequest
	}
	return uploadID, nil
}

func (s *S3VideoAssetStore) PresignUploadPart(
	ctx context.Context,
	key string,
	uploadID string,
	partNumber int32,
	contentLength int64,
	expires time.Duration,
) (VideoAssetSignedRequest, error) {
	if err := s.validateMultipartRequest(key, uploadID); err != nil || partNumber <= 0 || contentLength <= 0 || expires <= 0 {
		return VideoAssetSignedRequest{}, ErrInvalidVideoAssetStoreRequest
	}
	request, err := s.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
		PartNumber: aws.Int32(partNumber), ContentLength: aws.Int64(contentLength),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return VideoAssetSignedRequest{}, fmt.Errorf("presign video multipart part: %w", err)
	}
	headers := make(map[string]string, len(request.SignedHeader))
	for name, values := range request.SignedHeader {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	return VideoAssetSignedRequest{
		URL: request.URL, Method: http.MethodPut, Headers: headers, ExpiresAt: time.Now().Add(expires).Unix(),
	}, nil
}

func (s *S3VideoAssetStore) ListUploadedParts(ctx context.Context, key string, uploadID string) ([]VideoAssetUploadedPart, error) {
	if err := s.validateMultipartRequest(key, uploadID); err != nil {
		return nil, err
	}
	paginator := s3.NewListPartsPaginator(s.client, &s3.ListPartsInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	parts := make([]VideoAssetUploadedPart, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNoSuchVideoMultipartUpload(err) {
				return nil, ErrVideoMultipartUploadNotFound
			}
			return nil, fmt.Errorf("list video multipart parts: %w", err)
		}
		for _, part := range page.Parts {
			parts = append(parts, VideoAssetUploadedPart{
				PartNumber: aws.ToInt32(part.PartNumber), SizeBytes: aws.ToInt64(part.Size), ETag: aws.ToString(part.ETag),
			})
		}
	}
	sort.Slice(parts, func(left int, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	return parts, nil
}

func (s *S3VideoAssetStore) CompleteMultipartUpload(
	ctx context.Context,
	key string,
	uploadID string,
	parts []VideoAssetCompletedPart,
) error {
	if err := s.validateMultipartRequest(key, uploadID); err != nil || len(parts) == 0 {
		return ErrInvalidVideoAssetStoreRequest
	}
	completed := make([]s3types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		if part.PartNumber <= 0 || strings.TrimSpace(part.ETag) == "" {
			return ErrInvalidVideoAssetStoreRequest
		}
		completed = append(completed, s3types.CompletedPart{PartNumber: aws.Int32(part.PartNumber), ETag: aws.String(part.ETag)})
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		if isNoSuchVideoMultipartUpload(err) {
			return ErrVideoMultipartUploadNotFound
		}
		return fmt.Errorf("complete video multipart upload: %w", err)
	}
	return nil
}

func (s *S3VideoAssetStore) AbortMultipartUpload(ctx context.Context, key string, uploadID string) error {
	if err := s.validateMultipartRequest(key, uploadID); err != nil {
		return err
	}
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	if err != nil {
		if isNoSuchVideoMultipartUpload(err) {
			return ErrVideoMultipartUploadNotFound
		}
		return fmt.Errorf("abort video multipart upload: %w", err)
	}
	return nil
}

func (s *S3VideoAssetStore) validateRequest(key string) error {
	if s == nil || s.client == nil || s.presign == nil || strings.TrimSpace(s.bucket) == "" ||
		strings.TrimSpace(key) == "" || len(key) > 191 || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return ErrInvalidVideoAssetStoreRequest
	}
	return nil
}

func (s *S3VideoAssetStore) validateMultipartRequest(key string, uploadID string) error {
	if err := s.validateRequest(key); err != nil || strings.TrimSpace(uploadID) == "" || len(uploadID) > 512 {
		return ErrInvalidVideoAssetStoreRequest
	}
	return nil
}

func isNoSuchVideoMultipartUpload(err error) bool {
	var apiError smithy.APIError
	return errors.As(err, &apiError) && apiError.ErrorCode() == "NoSuchUpload"
}

func isVideoAssetObjectNotFound(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchObject":
			return true
		}
	}
	var responseError interface{ HTTPStatusCode() int }
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusNotFound
}
