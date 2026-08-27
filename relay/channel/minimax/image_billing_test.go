package minimax

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiniMaxImageResponseBillsEachUsableOutputOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for outputCount := 1; outputCount <= 4; outputCount++ {
		t.Run(fmt.Sprintf("n=%d", outputCount), func(t *testing.T) {
			urls := make([]string, outputCount)
			for index := range urls {
				urls[index] = fmt.Sprintf("https://example.com/minimax-%d.png", index)
			}
			responseBody, err := common.Marshal(map[string]any{
				"data": map[string]any{"image_urls": urls},
			})
			require.NoError(t, err)

			requestedCount := uint(outputCount)
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesGenerations,
				StartTime: time.Unix(1_700_000_000, 0),
				Request:   &dto.ImageRequest{N: &requestedCount},
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(responseBody)),
			}

			usage, apiErr := (&Adaptor{}).DoResponse(ctx, response, info)
			require.Nil(t, apiErr)
			require.IsType(t, &dto.Usage{}, usage)
			assert.Equal(t, float64(outputCount), info.PriceData.OtherRatios()["n"])

			var imageResponse dto.ImageResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &imageResponse))
			assert.Len(t, imageResponse.Data, outputCount)
		})
	}
}

func TestMiniMaxImageResponseRejectsMoreOutputsThanRequested(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responseBody, err := common.Marshal(map[string]any{
		"data": map[string]any{"image_urls": []string{
			"https://example.com/1.png", "https://example.com/2.png",
			"https://example.com/3.png", "https://example.com/4.png",
			"https://example.com/5.png",
		}},
	})
	require.NoError(t, err)

	requestedCount := uint(4)
	info := &relaycommon.RelayInfo{
		StartTime: time.Unix(1_700_000_000, 0),
		Request:   &dto.ImageRequest{N: &requestedCount},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}

	_, apiErr := miniMaxImageHandler(ctx, response, info)
	require.NotNil(t, apiErr)
	assert.ErrorIs(t, apiErr, service.ErrInvalidImageOutputCount)
	assert.Nil(t, info.PriceData.OtherRatios())
	assert.Empty(t, recorder.Body.String())
}
