package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageStudioResponseLimitRejectsOversizedBody(t *testing.T) {
	c := limitedGeminiImageContext(32)
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 33))),
	}
	_, apiErr := GeminiImageHandler(c, &relaycommon.RelayInfo{}, response)
	require.NotNil(t, apiErr)
	require.Equal(t, service.ErrImageStudioResponseTooLarge.Error(), apiErr.Error())
}

func TestGeminiImageHandlerRecordsFilteredOutputCountWithoutCountRatio(t *testing.T) {
	payload, err := common.Marshal(dto.GeminiImageResponse{Predictions: []dto.GeminiImagePrediction{
		{BytesBase64Encoded: "image-1"},
		{RaiFilteredReason: "filtered"},
		{BytesBase64Encoded: "image-2"},
		{RaiFilteredReason: "filtered"},
	}})
	require.NoError(t, err)
	requested := uint(4)
	info := &relaycommon.RelayInfo{
		Request: &dto.ImageRequest{N: &requested},
		PriceData: types.PriceData{
			ModelRatio: 1, CompletionRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	c := limitedGeminiImageContext(int64(len(payload) + 1))

	usage, apiErr := GeminiImageHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(payload)),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 516, usage.TotalTokens)
	require.Equal(t, 2, info.ImageOutputCount)
	require.Nil(t, info.PriceData.OtherRatios())
}

func limitedGeminiImageContext(maximum int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request = request.WithContext(service.WithImageStudioResponseLimit(request.Context(), maximum))
	return c
}
