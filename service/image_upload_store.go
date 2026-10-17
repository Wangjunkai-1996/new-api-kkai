package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ImageUploadStore adds only the multipart operations used by reference images.
// The image signer deliberately does not change the older video upload protocol.
type ImageUploadStore interface {
	ImageAssetStore
	Head(context.Context, string) (VideoAssetObjectMetadata, error)
	CreateMultipartUpload(context.Context, string, string) (string, error)
	PresignImageUploadPart(context.Context, string, string, int32, int64, time.Duration) (VideoAssetSignedRequest, error)
	ListUploadedParts(context.Context, string, string) ([]VideoAssetUploadedPart, error)
	CompleteMultipartUpload(context.Context, string, string, []VideoAssetCompletedPart) error
	AbortMultipartUpload(context.Context, string, string) error
	AbortImageUploadSessions(context.Context, string) error
}

func (s *S3VideoAssetStore) PresignImageUploadPart(
	ctx context.Context, key, uploadID string, partNumber int32, contentLength int64, expires time.Duration,
) (VideoAssetSignedRequest, error) {
	if err := s.validateMultipartRequest(key, uploadID); err != nil || partNumber <= 0 || contentLength <= 0 || expires <= 0 {
		return VideoAssetSignedRequest{}, ErrInvalidImageUpload
	}
	request, err := s.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
		PartNumber: aws.Int32(partNumber), ContentLength: aws.Int64(contentLength),
	}, func(options *s3.PresignOptions) { options.Expires = expires })
	if err != nil {
		return VideoAssetSignedRequest{}, fmt.Errorf("sign image upload part: %w", err)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || !strings.Contains(";"+parsed.Query().Get("X-Amz-SignedHeaders")+";", ";content-length;") {
		return VideoAssetSignedRequest{}, ErrImageUploadLengthNotSigned
	}
	headers := make(map[string]string)
	for name, values := range request.SignedHeader {
		// Browsers set these from the URL and Blob; scripts cannot set them.
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") || len(values) == 0 {
			continue
		}
		headers[name] = values[0]
	}
	return VideoAssetSignedRequest{URL: request.URL, Method: http.MethodPut, Headers: headers, ExpiresAt: time.Now().Add(expires).Unix()}, nil
}

// A process may die after CreateMultipartUpload but before saving its ID. Exact
// key enumeration also removes those otherwise-unbound sessions during cleanup.
func (s *S3VideoAssetStore) AbortImageUploadSessions(ctx context.Context, key string) error {
	if err := s.validateRequest(key); err != nil || !strings.HasPrefix(key, "image-studio/") {
		return ErrInvalidImageUpload
	}
	paginator := s3.NewListMultipartUploadsPaginator(s.client, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(s.bucket), Prefix: aws.String(key), MaxUploads: aws.Int32(100),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list image upload sessions: %w", err)
		}
		for _, upload := range page.Uploads {
			if aws.ToString(upload.Key) != key {
				continue
			}
			if err := s.AbortMultipartUpload(ctx, key, aws.ToString(upload.UploadId)); err != nil && !errors.Is(err, ErrVideoMultipartUploadNotFound) {
				return err
			}
		}
	}
	return nil
}
