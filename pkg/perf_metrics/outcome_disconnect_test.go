package perfmetrics

import (
	"context"
	"errors"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRelayResultAfterDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream bool
		mark   func(*relaycommon.StreamStatus)
		end    relaycommon.StreamEndReason
		err    *types.NewAPIError
		want   Outcome
	}{
		{name: "completed non-stream response", want: OutcomeSuccess},
		{name: "unfinished stream", stream: true, want: OutcomeIgnored},
		{name: "completed stream", stream: true, mark: (*relaycommon.StreamStatus).MarkCompleted, end: relaycommon.StreamEndReasonEOF, want: OutcomeSuccess},
		{name: "completed stream then connection closes", stream: true, mark: (*relaycommon.StreamStatus).MarkCompleted, end: relaycommon.StreamEndReasonClientGone, want: OutcomeSuccess},
		{name: "done marker", stream: true, end: relaycommon.StreamEndReasonDone, want: OutcomeSuccess},
		{name: "read interrupted before terminal", stream: true, end: relaycommon.StreamEndReasonScannerErr, want: OutcomeIgnored},
		{name: "client gone before terminal", stream: true, end: relaycommon.StreamEndReasonClientGone, want: OutcomeIgnored},
		{name: "explicit cancellation", stream: true, mark: (*relaycommon.StreamStatus).MarkCancelled, end: relaycommon.StreamEndReasonDone, want: OutcomeIgnored},
		{name: "upstream failure before disconnect", stream: true, mark: func(s *relaycommon.StreamStatus) { s.MarkFailed("server_error", "", 500) }, want: OutcomeFailure},
		{name: "upstream failure after completion", stream: true, mark: func(s *relaycommon.StreamStatus) { s.MarkCompleted(); s.MarkFailed("server_error", "", 500) }, want: OutcomeFailure},
		{name: "output limit", stream: true, mark: func(s *relaycommon.StreamStatus) { s.MarkIncomplete("max_output_tokens") }, want: OutcomeSuccess},
		{name: "content filter", stream: true, mark: func(s *relaycommon.StreamStatus) { s.MarkIncomplete("content_filter") }, want: OutcomeIgnored},
		{name: "request interrupted", err: types.NewOpenAIError(context.Canceled, types.ErrorCodeDoRequestFailed, 500), want: OutcomeIgnored},
		{name: "non-stream read interrupted", err: types.NewOpenAIError(errors.New("reader closed"), types.ErrorCodeReadResponseBodyFailed, 500), want: OutcomeIgnored},
		{name: "provider failure before disconnect", err: types.InitOpenAIError("server_error", 500), want: OutcomeFailure},
		{name: "provider credentials rejected before disconnect", err: types.InitOpenAIError("invalid_api_key", 401), want: OutcomeFailure},
		{name: "provider policy rejection before disconnect", err: types.InitOpenAIError("content_policy_violation", 400), want: OutcomeIgnored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetKKAIGroupSignalState(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			info := &relaycommon.RelayInfo{OriginModelName: "test-model", UsingGroup: "test-group", StartTime: time.Now(), IsStream: tc.stream}
			if tc.stream {
				info.StreamStatus = relaycommon.NewStreamStatus()
				info.StreamStatus.RequireTerminal()
				if tc.mark != nil {
					tc.mark(info.StreamStatus)
				}
				info.StreamStatus.SetEndReason(tc.end, nil)
			}

			RecordRelayResult(ctx, info, tc.err)

			events := QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events
			buckets := QueryKKAIGroupMinuteBuckets(info.StartTime.Add(-time.Minute).Unix(), time.Now().Unix(), []string{info.UsingGroup}).Buckets
			if tc.want == OutcomeIgnored {
				assert.Empty(t, events)
				assert.Empty(t, buckets)
				return
			}
			require.Len(t, events, 1)
			assert.Equal(t, tc.want == OutcomeSuccess, events[0].Success)
			require.Len(t, buckets, 1)
			assert.EqualValues(t, 1, buckets[0].RequestCount)
			if tc.want == OutcomeSuccess {
				assert.EqualValues(t, 1, buckets[0].SuccessCount)
			} else {
				assert.Zero(t, buckets[0].SuccessCount)
			}
		})
	}
}
