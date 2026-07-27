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
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

type videoAssetHTTPStatusError struct {
	status int
}

type videoAssetStoreRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip videoAssetStoreRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func (err videoAssetHTTPStatusError) Error() string {
	return "video asset http error"
}

func (err videoAssetHTTPStatusError) HTTPStatusCode() int {
	return err.status
}

func TestVideoAssetObjectNotFoundRecognizesS3ErrorCodes(t *testing.T) {
	for _, code := range []string{"NotFound", "NoSuchKey", "NoSuchObject"} {
		require.True(t, isVideoAssetObjectNotFound(&smithy.GenericAPIError{Code: code}))
	}
	require.False(t, isVideoAssetObjectNotFound(&smithy.GenericAPIError{Code: "AccessDenied"}))
}

func TestVideoAssetObjectNotFoundRecognizesHTTP404(t *testing.T) {
	require.True(t, isVideoAssetObjectNotFound(videoAssetHTTPStatusError{status: http.StatusNotFound}))
	require.False(t, isVideoAssetObjectNotFound(videoAssetHTTPStatusError{status: http.StatusForbidden}))
}

func TestS3VideoAssetStorePresignedUploadHeadersOmitBrowserManagedHeaders(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-access-key", "test-secret-key", "")),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example")
		options.UsePathStyle = true
	})
	store := &S3VideoAssetStore{bucket: "video-test", client: client, presign: s3.NewPresignClient(client)}

	tests := []struct {
		name                  string
		presign               func() (VideoAssetSignedRequest, error)
		expectedSignedHeaders string
		expectedHeaders       map[string]string
	}{
		{
			name:                  "single part upload",
			expectedSignedHeaders: "host",
			expectedHeaders:       map[string]string{},
			presign: func() (VideoAssetSignedRequest, error) {
				return store.PresignUpload(context.Background(), "users/7/input.mp4", "video/mp4", 1024, time.Minute)
			},
		},
		{
			name:                  "multipart upload part",
			expectedSignedHeaders: "host",
			expectedHeaders:       map[string]string{},
			presign: func() (VideoAssetSignedRequest, error) {
				return store.PresignUploadPart(context.Background(), "users/7/input.mp4", "upload-id", 1, 1024, time.Minute)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signedRequest, err := test.presign()
			require.NoError(t, err)

			parsedURL, err := url.Parse(signedRequest.URL)
			require.NoError(t, err)

			require.NotEmpty(t, parsedURL.Query().Get("X-Amz-Signature"))
			require.Equal(t, test.expectedSignedHeaders, parsedURL.Query().Get("X-Amz-SignedHeaders"))
			require.Equal(t, test.expectedHeaders, signedRequest.Headers)
			for name := range signedRequest.Headers {
				require.False(t, strings.EqualFold(name, "Host"), "browser-managed Host header must not be returned")
				require.False(t, strings.EqualFold(name, "Content-Length"), "browser-managed Content-Length header must not be returned")
			}
		})
	}
}

func TestS3VideoAssetStorePersistsArchiveContentAndSourceFingerprints(t *testing.T) {
	storedBody := ""
	storedContentSHA256 := ""
	storedArchiveSourceSHA256 := ""
	httpClient := &http.Client{Transport: videoAssetStoreRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			storedBody = string(body)
			storedContentSHA256 = request.Header.Get("x-amz-meta-sha256")
			storedArchiveSourceSHA256 = request.Header.Get("x-amz-meta-archive-source-sha256")
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: request,
			}, nil
		case http.MethodHead:
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: 5,
				Header: http.Header{
					"Content-Type":                     []string{"video/mp4"},
					"Content-Length":                   []string{"5"},
					"x-amz-meta-sha256":                []string{storedContentSHA256},
					"x-amz-meta-archive-source-sha256": []string{storedArchiveSourceSHA256},
				},
				Body: http.NoBody, Request: request,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusMethodNotAllowed, Header: http.Header{}, Body: http.NoBody, Request: request,
			}, nil
		}
	})}
	client := s3.NewFromConfig(aws.Config{
		Region:      "auto",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-access-key", "test-secret-key", "")),
		HTTPClient:  httpClient,
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example")
		options.UsePathStyle = true
	})
	store := &S3VideoAssetStore{bucket: "video-test", client: client, presign: s3.NewPresignClient(client)}
	contentSHA256 := strings.Repeat("a", 64)
	archiveSourceSHA256 := strings.Repeat("b", 64)
	require.NoError(t, store.PutArchive(
		context.Background(), "users/7/output.mp4", "video/mp4", strings.NewReader("video"), 5,
		contentSHA256, archiveSourceSHA256,
	))

	metadata, err := store.Head(context.Background(), "users/7/output.mp4")
	require.NoError(t, err)
	require.Equal(t, "video", storedBody)
	require.Equal(t, contentSHA256, metadata.SHA256)
	require.Equal(t, archiveSourceSHA256, metadata.ArchiveSourceSHA256)
}

func TestS3VideoAssetStoreRejectsInvalidArchiveSourceFingerprint(t *testing.T) {
	store := &S3VideoAssetStore{}
	err := store.PutArchive(
		context.Background(), "users/7/output.mp4", "video/mp4", strings.NewReader("video"), 5,
		strings.Repeat("a", 64), "https://media.example/private.mp4?api_key=secret",
	)
	require.ErrorIs(t, err, ErrInvalidVideoAssetStoreRequest)
}
