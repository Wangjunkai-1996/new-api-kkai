package relay

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func pricedPassthroughSnapshot() *imagepricing.Snapshot {
	return &imagepricing.Snapshot{
		PolicyVersion:  "policy-v1",
		PolicyHash:     "policy-hash",
		Model:          "gpt-image-2",
		Size:           "1024x1024",
		Tier:           "1k",
		UnitPrice:      0.67,
		QuotaPerUnit:   500000,
		GroupRatio:     1,
		RequestedCount: 2,
	}
}

func TestBuildPricedImagePassthroughJSONAddsExplicitPricingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalBody := []byte(`{"model":"gpt-image-2","prompt":"draw","stream":true}`)
	storage, err := appcommon.CreateBodyStorage(originalBody)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(originalBody))
	context.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{ImagePricingSnapshot: pricedPassthroughSnapshot()}

	body, size, closer, err := buildPricedImagePassthroughBody(context, info, storage)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	assert.Equal(t, int64(len(encoded)), size)
	assert.Equal(t, "1024x1024", gjson.GetBytes(encoded, "size").String())
	assert.Equal(t, int64(2), gjson.GetBytes(encoded, "n").Int())
	assert.True(t, gjson.GetBytes(encoded, "stream").Bool())
	assert.True(t, info.UpstreamIsStream)
	assert.True(t, info.ImagePricingOutboundValidated)
}

func TestBuildPricedImagePassthroughJSONCanonicalizesPricingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalBody := []byte(`{"model":"gpt-image-2","size":"3840x2160","size":"1024x1024","n":3,"n":2,"custom":{"keep":true}}`)
	storage, err := appcommon.CreateBodyStorage(originalBody)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(originalBody))
	context.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{ImagePricingSnapshot: pricedPassthroughSnapshot()}

	body, _, closer, err := buildPricedImagePassthroughBody(context, info, storage)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(encoded), `"size"`))
	assert.Equal(t, 1, strings.Count(string(encoded), `"n"`))
	assert.Equal(t, "1024x1024", gjson.GetBytes(encoded, "size").String())
	assert.Equal(t, int64(2), gjson.GetBytes(encoded, "n").Int())
	assert.True(t, gjson.GetBytes(encoded, "custom.keep").Bool())
	assert.True(t, info.ImagePricingOutboundValidated)
}

func TestBuildPricedImagePassthroughMultipartPreservesExplicitPricingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var originalBody bytes.Buffer
	writer := multipart.NewWriter(&originalBody)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "edit"))
	require.NoError(t, writer.WriteField("size", "1024x1024"))
	require.NoError(t, writer.WriteField("n", "2"))
	require.NoError(t, writer.WriteField("stream", "true"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	originalBytes := append([]byte(nil), originalBody.Bytes()...)
	storage, err := appcommon.CreateBodyStorage(originalBytes)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(originalBytes))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, context.Request.ParseMultipartForm(32<<20))
	t.Cleanup(func() { require.NoError(t, context.Request.MultipartForm.RemoveAll()) })
	info := &relaycommon.RelayInfo{ImagePricingSnapshot: pricedPassthroughSnapshot()}

	body, size, closer, err := buildPricedImagePassthroughBody(context, info, storage)
	require.NoError(t, err)
	assert.Nil(t, closer)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, int64(len(encoded)), size)

	replayed := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(encoded))
	replayed.Header.Set("Content-Type", context.Request.Header.Get("Content-Type"))
	require.NoError(t, replayed.ParseMultipartForm(32<<20))
	t.Cleanup(func() { require.NoError(t, replayed.MultipartForm.RemoveAll()) })
	assert.Equal(t, "1024x1024", replayed.PostForm.Get("size"))
	assert.Equal(t, "2", replayed.PostForm.Get("n"))
	assert.Equal(t, "true", replayed.PostForm.Get("stream"))
	require.Len(t, replayed.MultipartForm.File["image"], 1)
	file, err := replayed.MultipartForm.File["image"][0].Open()
	require.NoError(t, err)
	fileBytes, readErr := io.ReadAll(file)
	require.NoError(t, file.Close())
	require.NoError(t, readErr)
	assert.Equal(t, []byte("image bytes"), fileBytes)
	assert.True(t, info.UpstreamIsStream)
	assert.True(t, info.ImagePricingOutboundValidated)
}
