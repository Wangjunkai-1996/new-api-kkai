package replicate

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageEditRejectsMultipleReferenceImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, image := range []struct {
		name string
		data string
	}{
		{name: "first.png", data: "first image"},
		{name: "second.png", data: "second image"},
	} {
		part, err := writer.CreateFormFile("image", image.name)
		require.NoError(t, err)
		_, err = part.Write([]byte(image.data))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(32<<20))
	t.Cleanup(func() { require.NoError(t, c.Request.MultipartForm.RemoveAll()) })

	_, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.ImageRequest{
		Model:  ModelFlux11Pro,
		Prompt: "combine these references",
	})
	require.EqualError(t, err, "replicate adaptor: multiple reference images are not supported")
}
