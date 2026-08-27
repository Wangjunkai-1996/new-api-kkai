package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyImageOutputBillingCountUsesActualCountExactlyOnce(t *testing.T) {
	requestedCount := uint(4)
	info := &relaycommon.RelayInfo{
		Request: &dto.ImageRequest{N: &requestedCount},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	info.PriceData.AddOtherRatio("n", 4)

	require.NoError(t, ApplyImageOutputBillingCount(info, 3))
	assert.Equal(t, map[string]float64{"n": 3}, info.PriceData.OtherRatios())

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{
		PromptTokens: 1,
		TotalTokens:  1,
	})
	assert.Equal(t, 3, summary.Quota)
}

func TestApplyImageOutputBillingCountRejectsUnsafeCounts(t *testing.T) {
	requestedCount := uint(4)
	zeroRequestedCount := uint(0)
	oversizedRequestedCount := ^uint(0)
	tests := []struct {
		name        string
		info        *relaycommon.RelayInfo
		outputCount int
	}{
		{name: "nil relay info", outputCount: 1},
		{name: "zero output", info: &relaycommon.RelayInfo{}, outputCount: 0},
		{name: "global limit exceeded", info: &relaycommon.RelayInfo{}, outputCount: dto.MaxImageN + 1},
		{
			name:        "zero requested count",
			info:        &relaycommon.RelayInfo{Request: &dto.ImageRequest{N: &zeroRequestedCount}},
			outputCount: 1,
		},
		{
			name:        "unsigned requested count overflow",
			info:        &relaycommon.RelayInfo{Request: &dto.ImageRequest{N: &oversizedRequestedCount}},
			outputCount: 1,
		},
		{
			name:        "requested count exceeded",
			info:        &relaycommon.RelayInfo{Request: &dto.ImageRequest{N: &requestedCount}},
			outputCount: 5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ApplyImageOutputBillingCount(test.info, test.outputCount)
			require.ErrorIs(t, err, ErrInvalidImageOutputCount)
			if test.info != nil {
				assert.Nil(t, test.info.PriceData.OtherRatios())
			}
		})
	}
}
