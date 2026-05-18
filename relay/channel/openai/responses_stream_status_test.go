package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupResponsesStreamTest(t *testing.T, body string, path string, relayFormat types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(commonpkg.RequestIdKey, "req-test")

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(body)),
	}

	info := &relaycommon.RelayInfo{
		IsStream:           true,
		RelayFormat:        relayFormat,
		ShouldIncludeUsage: false,
		ChannelMeta:        &relaycommon.ChannelMeta{},
	}

	return c, recorder, resp, info
}

func TestOaiResponsesStreamHandler_StopsAtCompleted(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`,
		`data: {"type":"response.output_text.delta","delta":"late"}`,
		`data: [DONE]`,
		"",
	}, "\n")

	c, recorder, resp, info := setupResponsesStreamTest(t, body, "/v1/responses", types.RelayFormatOpenAIResponses)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.StreamStatus)

	assert.Equal(t, 11, usage.PromptTokens)
	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 18, usage.TotalTokens)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Contains(t, recorder.Body.String(), `response.completed`)
	assert.Contains(t, recorder.Body.String(), `"hello"`)
	assert.NotContains(t, recorder.Body.String(), `"late"`)
}

func TestOaiResponsesToChatStreamHandler_StopsAtCompleted(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"model":"gpt-5.5","created_at":123}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"model":"gpt-5.5","created_at":123,"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`,
		`data: {"type":"response.output_text.delta","delta":"late"}`,
		`data: [DONE]`,
		"",
	}, "\n")

	c, recorder, resp, info := setupResponsesStreamTest(t, body, "/v1/chat/completions", types.RelayFormatOpenAI)

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.StreamStatus)

	assert.Equal(t, 11, usage.PromptTokens)
	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 18, usage.TotalTokens)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Contains(t, recorder.Body.String(), `"hello"`)
	assert.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	assert.Contains(t, recorder.Body.String(), `[DONE]`)
	assert.NotContains(t, recorder.Body.String(), `"late"`)
}
