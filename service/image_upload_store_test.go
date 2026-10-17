package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageUploadSignatureBindsExactLengthWithoutBrowserForbiddenHeaders(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region: "auto", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example")
		options.UsePathStyle = true
	})
	store := &S3VideoAssetStore{bucket: "image-test", client: client, presign: s3.NewPresignClient(client)}
	signed, err := store.PresignImageUploadPart(context.Background(), "image-studio/users/7/input", "upload-id", 1, 1024, time.Minute)
	require.NoError(t, err)
	parsed, err := url.Parse(signed.URL)
	require.NoError(t, err)
	assert.Contains(t, strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";"), "content-length")
	assert.NotEmpty(t, parsed.Query().Get("X-Amz-Signature"))
	for name := range signed.Headers {
		assert.False(t, strings.EqualFold(name, "Content-Length"))
		assert.False(t, strings.EqualFold(name, "Host"))
	}
	_, err = store.PresignImageUploadPart(context.Background(), "image-studio/users/7/input", "upload-id", 1, 0, time.Minute)
	assert.ErrorIs(t, err, ErrInvalidImageUpload)
}

func TestImageUploadCleanupOnlyAbortsExactKeyAndAcceptsConcurrentCompletion(t *testing.T) {
	var aborted []string
	client := s3.NewFromConfig(aws.Config{
		Region: "auto", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
		HTTPClient: &http.Client{Transport: videoAssetStoreRoundTripper(func(request *http.Request) (*http.Response, error) {
			status := http.StatusOK
			var body string
			switch request.Method {
			case http.MethodGet:
				assert.Equal(t, "image-studio/target", request.URL.Query().Get("prefix"))
				body = `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>image-studio/target</Key><UploadId>exact</UploadId></Upload><Upload><Key>image-studio/target-other</Key><UploadId>other</UploadId></Upload></ListMultipartUploadsResult>`
			case http.MethodDelete:
				aborted = append(aborted, request.URL.Query().Get("uploadId"))
				status, body = http.StatusNotFound, `<Error><Code>NoSuchUpload</Code></Error>`
			default:
				t.Fatalf("unexpected storage request %s", request.Method)
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		})},
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example")
		options.UsePathStyle = true
	})
	store := &S3VideoAssetStore{bucket: "image-test", client: client, presign: s3.NewPresignClient(client)}
	require.NoError(t, store.AbortImageUploadSessions(context.Background(), "image-studio/target"))
	assert.Equal(t, []string{"exact"}, aborted)
}
