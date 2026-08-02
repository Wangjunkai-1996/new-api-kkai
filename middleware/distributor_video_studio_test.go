package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestRecognizesVideoStudioPlaygroundRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/pg/videos", "/pg/videos/quote"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"sora-2","group":"vip","prompt":"test"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, shouldSelectChannel, err := getModelRequest(ctx)
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.True(t, shouldSelectChannel)
			assert.Equal(t, "sora-2", request.Model)
			assert.Equal(t, "vip", request.Group)
			assert.Equal(t, relayconstant.RelayModeVideoSubmit, ctx.GetInt("relay_mode"))
			assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))

			var body map[string]any
			require.NoError(t, common.UnmarshalBodyReusable(ctx, &body))
			assert.Equal(t, "test", body["prompt"])
		})
	}
}

func TestGetModelRequestRecognizesImageStudioPlaygroundRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/pg/images", "/pg/images/quote"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"image-model","prompt":"test"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, shouldSelectChannel, err := getModelRequest(ctx)
			require.NoError(t, err)
			assert.True(t, shouldSelectChannel)
			assert.Equal(t, "image-model", request.Model)
			assert.Equal(t, relayconstant.RelayModeImagesGenerations, ctx.GetInt("relay_mode"))
		})
	}
}
