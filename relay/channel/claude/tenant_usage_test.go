package claude

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantClaudeUsageDistinguishesMissingFromExplicitZero(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, start, final string
			reported           bool
		}{
			{"missing", "", "", false},
			{"empty", `,"usage":{}`, `,"usage":{}`, false},
			{"missing completion", `,"usage":{"input_tokens":3}`, `,"usage":{}`, false},
			{"zero", `,"usage":{"input_tokens":0,"output_tokens":0}`, `,"usage":{"output_tokens":0}`, true},
			{"positive", `,"usage":{"input_tokens":3,"output_tokens":2}`, `,"usage":{"output_tokens":2}`, true},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				body := `{"id":"msg_usage","type":"message","model":"claude-test","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"` + tc.start + `}`
				if stream {
					body = "data: " + `{"type":"message_start","message":{"id":"msg_usage","model":"claude-test","role":"assistant","content":[]` + tc.start + "}}\n\n"
					body += "data: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}` + "\n\n"
					body += "data: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"}` + tc.final + "}\n\n"
					body += "data: " + `{"type":"message_stop"}` + "\n\n"
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				info := &relaycommon.RelayInfo{TenantID: 42, TenantQuoteID: 1, IsStream: stream, DisablePing: true, RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				info.SetEstimatePromptTokens(17)
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
				handler := ClaudeHandler
				if stream {
					handler = ClaudeStreamHandler
				}
				usage, apiErr := handler(c, resp, info)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, tc.reported, info.BillingHasReportedUsage)
				assert.Equal(t, !tc.reported, info.BillingOutcomeUnknown)
				if tc.name == "zero" {
					assert.Zero(t, usage.PromptTokens)
					assert.Zero(t, usage.CompletionTokens)
				}
			})
		}
	}
}
