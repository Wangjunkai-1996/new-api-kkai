package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantChatUsageDistinguishesMissingFromExplicitZero(t *testing.T) {
	service.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, usage string
			reported    bool
		}{
			{"missing", "", false},
			{"empty", `,"usage":{}`, false},
			{"missing completion", `,"usage":{"prompt_tokens":3}`, false},
			{"zero", `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`, true},
			{"positive", `,"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`, true},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				body := `{"id":"usage-test","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]` + tc.usage + `}`
				if stream {
					body = "data: " + `{"id":"usage-test","model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}` + "\n\n"
					if tc.usage != "" {
						body += "data: " + `{"id":"usage-test","choices":[]` + tc.usage + "}\n\n"
					}
					body += "data: [DONE]\n\n"
				}
				c, _, resp, info := newResponsesChatTestContext(t, body, stream)
				info.TenantID, info.TenantQuoteID = 42, 1
				info.RelayMode = relayconstant.RelayModeChatCompletions
				info.SetEstimatePromptTokens(17)
				handler := OpenaiHandler
				if stream {
					handler = OaiStreamHandler
				}
				usage, apiErr := handler(c, info, resp)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, tc.reported, info.BillingHasReportedUsage)
				assert.Equal(t, !tc.reported, info.BillingOutcomeUnknown)
				if tc.name == "zero" {
					assert.Zero(t, usage.PromptTokens)
					assert.Zero(t, usage.CompletionTokens)
					assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
				}
			})
		}
	}
}

func TestOpenaiHandlerMarksLocallyEstimatedUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-mini"},
	}
	info.SetEstimatePromptTokens(17)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OpenaiHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 17, usage.PromptTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestOaiStreamHandlerMarksContentFilteredChoice(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`,
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":0,"total_tokens":100,"prompt_tokens_details":{"cached_tokens":90}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, _, resp, info := newResponsesChatTestContext(t, body, true)
	info.RelayMode = relayconstant.RelayModeChatCompletions

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, "openai_finish_reason=content_filter", common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
}

func TestTenantEmbeddingUsageRequiresInputWithoutCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		reported    bool
		prompt      int
	}{
		{"missing", "", false, 0},
		{"empty", `,"usage":{}`, false, 0},
		{"total only", `,"usage":{"total_tokens":5}`, false, 0},
		{"zero", `,"usage":{"prompt_tokens":0,"total_tokens":0}`, true, 0},
		{"positive", `,"usage":{"prompt_tokens":5,"total_tokens":5}`, true, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"object":"list","model":"embedding-test","data":[{"object":"embedding","index":0,"embedding":[0.5]}]` + tc.usage + `}`
			c, _, resp, info := newResponsesChatTestContext(t, body, false)
			info.TenantID, info.TenantQuoteID = 42, 1
			info.RelayMode = relayconstant.RelayModeEmbeddings
			info.SetEstimatePromptTokens(17)
			usage, apiErr := OpenaiHandler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.reported, info.BillingHasReportedUsage)
			assert.Equal(t, !tc.reported, info.BillingOutcomeUnknown)
			if tc.reported {
				assert.Equal(t, tc.prompt, usage.PromptTokens)
				assert.Zero(t, usage.CompletionTokens)
				assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
			}
		})
	}
}
