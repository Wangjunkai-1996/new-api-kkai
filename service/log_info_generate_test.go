package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newLogInfoTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func newLogInfoTestRelayInfo(start time.Time) relaycommon.RelayInfo {
	return relaycommon.RelayInfo{
		StartTime: start,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
}

func TestGenerateTextOtherInfoUsesUpstreamHeaderTimeForDisplayedFRT(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(1500 * time.Millisecond)
	relayInfo.FirstResponseTime = start.Add(25 * time.Second)

	other := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, float64(1500), other["frt"])
	require.Equal(t, float64(25000), other["first_sse_ms"])
}

func TestGenerateTextOtherInfoFallsBackToFirstSSEWhenHeaderTimeMissing(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(8 * time.Second)

	other := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, float64(8000), other["frt"])
	require.Equal(t, float64(8000), other["first_sse_ms"])
}

func TestGenerateTextOtherInfoIgnoresInvalidHeaderTime(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(-time.Second)
	relayInfo.FirstResponseTime = start.Add(4 * time.Second)

	other := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, float64(4000), other["frt"])
	require.Equal(t, float64(4000), other["first_sse_ms"])
}

func TestGenerateTextOtherInfoOmitsInvalidResponseTimings(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(-time.Second)

	other := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1)

	require.NotContains(t, other, "frt")
	require.NotContains(t, other, "first_sse_ms")
}
