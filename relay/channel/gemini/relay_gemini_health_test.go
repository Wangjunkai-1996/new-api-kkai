package gemini

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiStreamHealthRequiresEveryCandidate(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, tc := range []struct {
		name     string
		count    int
		finished []int
		complete bool
	}{
		{"single candidate", 1, []int{0}, true},
		{"second candidate unfinished", 2, []int{0}, false},
		{"duplicate first candidate", 2, []int{0, 0}, false},
		{"all candidates finish separately", 2, []int{1, 0}, true},
		{"invalid index cannot complete request", 2, []int{0, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body strings.Builder
			for _, index := range tc.finished {
				fmt.Fprintf(&body, "data: {\"candidates\":[{\"index\":%d,\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n", index)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:streamGenerateContent", nil)
			info := &relaycommon.RelayInfo{
				IsStream:    true,
				DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
				Request:     &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{CandidateCount: common.GetPointer(tc.count)}},
			}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(body.String()))}

			usage, apiErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool { return true })
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			closed, cancel := context.WithCancel(context.Background())
			cancel()
			if tc.complete {
				assert.Equal(t, perfmetrics.OutcomeSuccess, perfmetrics.ClassifyRelayOutcome(closed, info, nil))
			} else {
				assert.Equal(t, perfmetrics.OutcomeFailure, perfmetrics.ClassifyRelayOutcome(context.Background(), info, nil), "EOF before all candidates finish is incomplete")
				assert.Equal(t, perfmetrics.OutcomeIgnored, perfmetrics.ClassifyRelayOutcome(closed, info, nil), "cancelled partial output must not become a successful request")
			}
		})
	}
}
