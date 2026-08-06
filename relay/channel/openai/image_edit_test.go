package openai

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestConvertImageEditRequestMultipart verifies that ConvertImageRequest
// re-serializes multipart image edit requests with all fields (including
// stream) and the file intact, both when the form was already parsed and when
// it must be re-parsed from the reusable body.
func TestConvertImageEditRequestMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newMultipartContext := func(t *testing.T, prompt string) *gin.Context {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", prompt))
		require.NoError(t, writer.WriteField("stream", "true"))
		require.NoError(t, writer.WriteField("partial_images", "3"))
		part, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("fake image"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	convertAndReplay := func(t *testing.T, c *gin.Context, prompt string) {
		info := &relaycommon.RelayInfo{
			RelayMode: relayconstant.RelayModeImagesEdits,
		}
		request := dto.ImageRequest{
			Model:  "gpt-image-1",
			Prompt: prompt,
			Stream: common.GetPointer(true),
		}

		converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
		require.NoError(t, err)
		convertedBody, ok := converted.(*bytes.Buffer)
		require.True(t, ok)

		replayedRequest := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(convertedBody.Bytes()))
		replayedRequest.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
		require.NoError(t, replayedRequest.ParseMultipartForm(32<<20))

		require.Equal(t, "gpt-image-1", replayedRequest.PostForm.Get("model"))
		require.Equal(t, prompt, replayedRequest.PostForm.Get("prompt"))
		require.Equal(t, "true", replayedRequest.PostForm.Get("stream"))
		require.Equal(t, "3", replayedRequest.PostForm.Get("partial_images"))
		require.Len(t, replayedRequest.MultipartForm.File["image"], 1)

		file, err := replayedRequest.MultipartForm.File["image"][0].Open()
		require.NoError(t, err)
		defer file.Close()
		fileBytes, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, []byte("fake image"), fileBytes)
	}

	t.Run("with pre-parsed form", func(t *testing.T) {
		prompt := "edit this image"
		c := newMultipartContext(t, prompt)
		require.NoError(t, c.Request.ParseMultipartForm(32<<20))

		convertAndReplay(t, c, prompt)
	})

	t.Run("re-parses reusable body when form is missing", func(t *testing.T) {
		prompt := "edit without pre-parsed form"
		c := newMultipartContext(t, prompt)

		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		c.Request.Body = io.NopCloser(storage)
		c.Request.MultipartForm = nil
		c.Request.PostForm = nil

		convertAndReplay(t, c, prompt)
	})
}

func TestConvertImageEditRequestMultipartAppliesParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2-4k"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("quality", "high"))
	require.NoError(t, writer.WriteField("stream", "false"))
	require.NoError(t, writer.WriteField("partial_images", "0"))
	require.NoError(t, writer.WriteField("custom_parameter", "preserved"))
	imagePart, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("first image bytes"))
	require.NoError(t, err)
	secondImagePart, err := writer.CreateFormFile("image", "second.png")
	require.NoError(t, err)
	_, err = secondImagePart.Write([]byte("second image bytes"))
	require.NoError(t, err)
	maskPart, err := writer.CreateFormFile("mask", "mask.png")
	require.NoError(t, err)
	_, err = maskPart.Write([]byte("mask bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(32<<20))
	c.Request.PostForm = url.Values(c.Request.MultipartForm.Value)

	var paramOverride map[string]interface{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"operations": [
			{
				"path": "quality",
				"mode": "delete",
				"conditions": [{"path":"original_model","mode":"full","value":"gpt-image-2-4k"}]
			},
			{"path":"stream","mode":"set","value":true},
			{
				"path":"partial_images",
				"mode":"set",
				"value":1,
				"conditions":[{"path":"partial_images","mode":"lte","value":0,"pass_missing_key":true}]
			}
		]
	}`), &paramOverride))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesEdits,
		OriginModelName: "gpt-image-2-4k",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: paramOverride,
		},
	}
	request := dto.ImageRequest{Model: "mapped-image-model", Prompt: "edit this image"}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
	require.NoError(t, err)
	convertedBody, ok := converted.(*bytes.Buffer)
	require.True(t, ok)

	replayed := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(convertedBody.Bytes()))
	replayed.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	require.NoError(t, replayed.ParseMultipartForm(32<<20))
	require.Equal(t, "mapped-image-model", replayed.PostForm.Get("model"))
	require.Equal(t, "edit this image", replayed.PostForm.Get("prompt"))
	require.Equal(t, "true", replayed.PostForm.Get("stream"))
	require.Equal(t, "1", replayed.PostForm.Get("partial_images"))
	require.Equal(t, "preserved", replayed.PostForm.Get("custom_parameter"))
	require.False(t, replayed.PostForm.Has("quality"))
	require.Len(t, replayed.PostForm["model"], 1)
	require.Len(t, replayed.PostForm["stream"], 1)
	require.Len(t, replayed.PostForm["partial_images"], 1)
	require.True(t, info.UpstreamIsStream)

	assertMultipartFileContents(t, replayed.MultipartForm, "image[]", []byte("first image bytes"), []byte("second image bytes"))
	assertMultipartFileContents(t, replayed.MultipartForm, "mask", []byte("mask bytes"))
}

func assertMultipartFileContents(t *testing.T, form *multipart.Form, field string, want ...[]byte) {
	t.Helper()
	require.NotNil(t, form)
	require.Len(t, form.File[field], len(want))
	for index, fileHeader := range form.File[field] {
		file, err := fileHeader.Open()
		require.NoError(t, err)
		got, err := io.ReadAll(file)
		require.NoError(t, file.Close())
		require.NoError(t, err)
		require.Equal(t, want[index], got)
	}
}
