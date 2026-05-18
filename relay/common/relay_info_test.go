package common

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoBeginStreamAttemptReplacesPreviousStatus(t *testing.T) {
	oldStatus := NewStreamStatus()
	oldStatus.SetEndReason(StreamEndReasonTimeout, fmt.Errorf("previous timeout"))
	oldStatus.RecordError("previous error")
	info := &RelayInfo{StreamStatus: oldStatus}

	newStatus := info.BeginStreamAttempt()

	require.NotSame(t, oldStatus, newStatus)
	require.Same(t, newStatus, info.StreamStatus)
	require.Equal(t, StreamEndReasonNone, newStatus.EndReason)
	require.Nil(t, newStatus.EndError)
	require.Equal(t, 0, newStatus.TotalErrorCount())
	require.Equal(t, StreamEndReasonTimeout, oldStatus.EndReason)
	require.Equal(t, 1, oldStatus.TotalErrorCount())
}

func TestRelayInfoBeginStreamAttemptNilReceiver(t *testing.T) {
	var info *RelayInfo

	streamStatus := info.BeginStreamAttempt()

	require.NotNil(t, streamStatus)
	require.Equal(t, StreamEndReasonNone, streamStatus.EndReason)
}
