package ali

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageEditPreservesReferenceImageOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstImage := []byte("\x89PNG\r\n\x1a\nfirst image")
	secondImage := []byte("\x89PNG\r\n\x1a\nsecond image")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for index, image := range [][]byte{firstImage, secondImage} {
		part, err := writer.CreateFormFile("image", []string{"first.png", "second.png"}[index])
		require.NoError(t, err)
		_, err = part.Write(image)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(32<<20))
	t.Cleanup(func() { require.NoError(t, c.Request.MultipartForm.RemoveAll()) })

	converted, err := oaiFormEdit2AliImageEdit(c, &relaycommon.RelayInfo{}, dto.ImageRequest{
		Model:  "qwen-image-edit",
		Prompt: "combine these references",
	})
	require.NoError(t, err)
	input, ok := converted.Input.(AliImageInput)
	require.True(t, ok)
	require.Len(t, input.Messages, 1)
	require.Equal(t, []AliMediaContent{
		{Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(firstImage)},
		{Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(secondImage)},
		{Text: "combine these references"},
	}, input.Messages[0].Content)
}
