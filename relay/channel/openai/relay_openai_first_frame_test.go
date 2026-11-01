package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type firstFrameFlushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan string
}

func (r *firstFrameFlushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.flushed <- r.Body.String()
}

func TestOaiStreamHandlerFlushesOrdinaryFirstFrameBeforeNextFrame(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	for _, tc := range []struct {
		name  string
		delta string
	}{
		{"role", `{"role":"assistant"}`},
		{"content", `{"content":"hello"}`},
		{"reasoning", `{"reasoning_content":"thinking"}`},
		{"tool call", `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = writer.Close() })
			recorder := &firstFrameFlushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan string, 8)}
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat:        types.RelayFormatOpenAI,
				RelayMode:          relayconstant.RelayModeChatCompletions,
				ShouldIncludeUsage: false,
				DisablePing:        true,
				ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
			}
			resp := &http.Response{Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			type result struct {
				usagePrompt int
				usageOutput int
				err         error
			}
			finished := make(chan result, 1)
			go func() {
				usage, apiErr := OaiStreamHandler(c, info, resp)
				var got result
				if usage != nil {
					got.usagePrompt, got.usageOutput = usage.PromptTokens, usage.CompletionTokens
				}
				if apiErr != nil {
					got.err = apiErr
				}
				finished <- got
			}()

			first := `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":` + tc.delta + `}]}`
			_, err := io.WriteString(writer, "data: "+first+"\n\n")
			require.NoError(t, err)
			select {
			case snapshot := <-recorder.flushed:
				assert.Equal(t, "data: "+first+"\n\n", snapshot)
			case <-time.After(2 * time.Second):
				t.Fatal("first frame was not flushed while the next upstream frame was blocked")
			}
			select {
			case <-finished:
				t.Fatal("handler finished before the terminal frame")
			default:
			}

			terminal := `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
			_, err = io.WriteString(writer, "data: "+terminal+"\n\ndata: [DONE]\n\n")
			require.NoError(t, err)
			_ = writer.Close()
			select {
			case got := <-finished:
				require.NoError(t, got.err)
				assert.Equal(t, 3, got.usagePrompt)
				assert.Equal(t, 2, got.usageOutput)
			case <-time.After(2 * time.Second):
				t.Fatal("handler did not finish after terminal frame")
			}
			output := recorder.Body.String()
			assert.Equal(t, 1, strings.Count(output, "data: "+first+"\n\n"))
			assert.NotContains(t, output, "data: "+terminal)
			assert.Equal(t, 1, strings.Count(output, "data: [DONE]\n\n"))
		})
	}
}

func TestOaiStreamHandlerFlushesConsecutiveDeltasAndKeepsUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	recorder := &firstFrameFlushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan string, 8)}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:        types.RelayFormatOpenAI,
		RelayMode:          relayconstant.RelayModeChatCompletions,
		ShouldIncludeUsage: true,
		DisablePing:        true,
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	resp := &http.Response{Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	type result struct {
		prompt int
		output int
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		usage, apiErr := OaiStreamHandler(c, info, resp)
		got := result{}
		if usage != nil {
			got.prompt, got.output = usage.PromptTokens, usage.CompletionTokens
		}
		if apiErr != nil {
			got.err = apiErr
		}
		finished <- got
	}()

	first := `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant"}}]}`
	second := `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"}}]}`
	for _, frame := range []string{first, second} {
		_, err := io.WriteString(writer, "data: "+frame+"\n\n")
		require.NoError(t, err)
		select {
		case snapshot := <-recorder.flushed:
			assert.Contains(t, snapshot, "data: "+frame+"\n\n")
		case <-time.After(2 * time.Second):
			t.Fatalf("frame was not flushed before the next upstream frame: %s", frame)
		}
	}

	terminal := `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	_, err := io.WriteString(writer, "data: "+terminal+"\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	_ = writer.Close()
	select {
	case got := <-finished:
		require.NoError(t, got.err)
		assert.Equal(t, 3, got.prompt)
		assert.Equal(t, 2, got.output)
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish after terminal frame")
	}
	output := recorder.Body.String()
	assert.Equal(t, 1, strings.Count(output, "data: "+first+"\n\n"))
	assert.Equal(t, 1, strings.Count(output, "data: "+second+"\n\n"))
	assert.Equal(t, 1, strings.Count(output, "data: "+terminal+"\n\n"))
	assert.Equal(t, 1, strings.Count(output, "data: [DONE]\n\n"))
}

func TestOrdinaryChatStreamDeltaExcludesTerminalAndUsageFrames(t *testing.T) {
	assert.False(t, ordinaryChatStreamDelta(`{"choices":[{"delta":{"content":"hello"}},{"delta":{},"finish_reason":"stop"}]}`))
	assert.False(t, ordinaryChatStreamDelta(`{"choices":[{"delta":{"content":"hello"}}],"usage":{"prompt_tokens":1}}`))
	assert.False(t, ordinaryChatStreamDelta(`{"choices":[{"delta":{}}]}`))
	assert.False(t, ordinaryChatStreamDelta(`{"choices":[{"delta":{"content":"hello"}}]`))
}
