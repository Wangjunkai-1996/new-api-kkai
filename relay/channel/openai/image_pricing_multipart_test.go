package openai

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertImageEditMultipartRejectsPricingParamOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		path  string
		value any
	}{
		{name: "size override", path: "size", value: "3840x2160"},
		{name: "count override", path: "n", value: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "gpt-image-2"))
			require.NoError(t, writer.WriteField("prompt", "edit"))
			require.NoError(t, writer.WriteField("size", "1024x1024"))
			require.NoError(t, writer.WriteField("n", "2"))
			part, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("image bytes"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
			context.Request.Header.Set("Content-Type", writer.FormDataContentType())
			require.NoError(t, context.Request.ParseMultipartForm(32<<20))
			context.Request.PostForm = url.Values(context.Request.MultipartForm.Value)
			t.Cleanup(func() { require.NoError(t, context.Request.MultipartForm.RemoveAll()) })

			count := uint(2)
			request := dto.ImageRequest{
				Model: "gpt-image-2",
				Size:  "1024x1024",
				N:     &count,
			}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesEdits,
				ChannelMeta: &relaycommon.ChannelMeta{
					ParamOverride: map[string]interface{}{
						"operations": []interface{}{
							map[string]interface{}{"path": test.path, "mode": "set", "value": test.value},
						},
					},
				},
				ImagePricingSnapshot: &imagepricing.Snapshot{
					PolicyVersion:  "policy-v1",
					PolicyHash:     "policy-hash",
					Model:          "gpt-image-2",
					Size:           "1024x1024",
					Tier:           "1k",
					UnitPrice:      0.67,
					QuotaPerUnit:   500000,
					GroupRatio:     1,
					RequestedCount: 2,
				},
			}

			_, err = (&Adaptor{}).ConvertImageRequest(context, info, request)
			assert.True(t, errors.Is(err, imagepricing.ErrOutboundMismatch), "unexpected error: %v", err)
			assert.False(t, info.ImagePricingOutboundValidated)
		})
	}
}
