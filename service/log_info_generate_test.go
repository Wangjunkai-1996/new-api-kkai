package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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

func TestGenerateTextOtherInfoUsesUpstreamHeaderForDisplayedFRT(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(1500 * time.Millisecond)
	relayInfo.FirstResponseTime = start.Add(25 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.Equal(t, float64(1500), snapshot["frt"])
	require.Equal(t, float64(25000), snapshot["first_sse_ms"])
	require.Equal(t, float64(1500), snapshot["upstream_header_ms"])
	assert.NotContains(t, snapshot, "sub2_ttft_ms")
}

func TestGenerateTextOtherInfoUsesSub2TTFTSidebandForDisplayedFRT(t *testing.T) {
	for _, timingMs := range []int64{0, 1180} {
		t.Run(time.Duration(timingMs*int64(time.Millisecond)).String(), func(t *testing.T) {
			start := time.Unix(1_700_000_000, 0)
			relayInfo := newLogInfoTestRelayInfo(start)
			relayInfo.UpstreamHeaderTime = start.Add(44 * time.Second)
			relayInfo.FirstResponseTime = start.Add(45 * time.Second)
			relayInfo.SetSub2TTFTMs(timingMs)

			snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

			assert.Equal(t, float64(timingMs), snapshot["frt"])
			assert.Equal(t, float64(timingMs), snapshot["sub2_ttft_ms"])
			assert.Equal(t, float64(44000), snapshot["upstream_header_ms"])
			assert.Equal(t, float64(45000), snapshot["first_sse_ms"])
		})
	}
}

func TestGenerateTextOtherInfoIgnoresInvalidSub2TTFTAndUsesHeader(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(1500 * time.Millisecond)
	relayInfo.SetSub2TTFTMs(-1)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	assert.Equal(t, float64(1500), snapshot["frt"])
	assert.NotContains(t, snapshot, "sub2_ttft_ms")
}

func TestGenerateTextOtherInfoOmitsFRTWhenHeaderTimeMissing(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(8 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.Equal(t, float64(8000), snapshot["first_sse_ms"])
	require.NotContains(t, snapshot, "upstream_header_ms")
}

func TestGenerateTextOtherInfoIgnoresInvalidHeaderTime(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(-time.Second)
	relayInfo.FirstResponseTime = start.Add(4 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.Equal(t, float64(4000), snapshot["first_sse_ms"])
	require.NotContains(t, snapshot, "upstream_header_ms")
}

func TestGenerateTextOtherInfoOmitsInvalidResponseTimings(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(-time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.NotContains(t, snapshot, "first_sse_ms")
}
