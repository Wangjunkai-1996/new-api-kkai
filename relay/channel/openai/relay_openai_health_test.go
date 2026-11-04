package openai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHealthRequiresEveryChoice(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, tc := range []struct {
		name     string
		count    int
		finished []int
		complete bool
	}{
		{"single choice", 1, []int{0}, true},
		{"second choice unfinished", 2, []int{0}, false},
		{"duplicate first choice", 2, []int{0, 0}, false},
		{"all choices finish separately", 2, []int{1, 0}, true},
		{"invalid index cannot complete request", 2, []int{0, 2}, false},
	} {
		for _, format := range []string{"openai", "gemini"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				var body strings.Builder
				for _, index := range tc.finished {
					fmt.Fprintf(&body, "data: {\"id\":\"health\",\"choices\":[{\"index\":%d,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n", index)
				}
				c, _, resp, info := newResponsesChatTestContext(t, body.String(), true)
				if format == "openai" {
					info.Request = &dto.GeneralOpenAIRequest{N: common.GetPointer(tc.count)}
				} else {
					info.Request = &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{CandidateCount: common.GetPointer(tc.count)}}
				}

				usage, apiErr := OaiStreamHandler(c, info, resp)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				closed, cancel := context.WithCancel(context.Background())
				cancel()
				if tc.complete {
					assert.Equal(t, perfmetrics.OutcomeSuccess, perfmetrics.ClassifyRelayOutcome(closed, info, nil))
				} else {
					assert.Equal(t, perfmetrics.OutcomeFailure, perfmetrics.ClassifyRelayOutcome(context.Background(), info, nil), "EOF before all choices finish is incomplete")
					assert.Equal(t, perfmetrics.OutcomeIgnored, perfmetrics.ClassifyRelayOutcome(closed, info, nil), "cancelled partial output must not become a successful request")
				}
			})
		}
	}
}

func TestOaiStreamHealthAfterDownstreamCancellation(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	const first = `data: {"id":"health","choices":[{"index":0,"delta":{"content":"first"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	const completed = `data: {"id":"health","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	for _, tc := range []struct {
		name    string
		tail    string
		want    perfmetrics.Outcome
		apiFail bool
	}{
		{"complete upstream response", completed, perfmetrics.OutcomeSuccess, false},
		{"upstream closes before terminal", "", perfmetrics.OutcomeIgnored, false},
		{"explicit upstream failure", `data: {"error":{"type":"server_error","code":"server_error","message":"upstream failed"}}`, perfmetrics.OutcomeFailure, true},
		{"invalid JSON before completion", "data: {invalid\n\n" + completed, perfmetrics.OutcomeFailure, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newResponsesChatTestContext(t, first+"\n\n"+tc.tail+"\n\n", true)
			info.RelayMode = relayconstant.RelayModeChatCompletions
			requestContext, cancel := context.WithCancel(c.Request.Context())
			t.Cleanup(cancel)
			c.Request = c.Request.WithContext(requestContext)
			c.Writer = &cancelAfterWriter{ResponseWriter: c.Writer, needle: "first", cancel: cancel}

			_, apiErr := OaiStreamHandler(c, info, resp)

			require.ErrorIs(t, requestContext.Err(), context.Canceled)
			if tc.apiFail {
				require.NotNil(t, apiErr)
			} else {
				require.Nil(t, apiErr)
			}
			assert.Equal(t, tc.want, perfmetrics.ClassifyRelayOutcome(requestContext, info, apiErr))
		})
	}
}
