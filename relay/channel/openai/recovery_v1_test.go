package openai

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedRecoveryResponsesHandlersUseWinnerUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	frames := []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`,
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`{"type":"response.output_text.delta","delta":"old answer"}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_a","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_a","seq":3,"attempt":1}`,
		`{"type":"response.output_text.delta","delta":"[regenerating]"}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_a","seq":4,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":5,"attempt":2}`,
		`{"type":"response.created","response":{"id":"resp_2","model":"gpt-test"}}`,
		`{"type":"response.output_text.delta","delta":"new answer"}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":6,"attempt":2,"outcome":"completed","usage_format":"responses","usage_reported":true,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`,
		`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")
	for _, tc := range []struct {
		name    string
		stream  bool
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
		wantOld bool
	}{
		{"raw", true, OaiResponsesStreamHandler, true},
		{"converted", true, OaiResponsesToChatStreamHandler, true},
		{"buffered", false, OaiResponsesToChatBufferedStreamHandler, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newResponsesChatTestContext(t, body.String(), tc.stream)
			c.Set("kkai_managed_recovery_v1", true)
			if tc.name == "raw" {
				info.RelayFormat = types.RelayFormatOpenAIResponses
			}
			usage, apiErr := tc.handler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Zero(t, usage.TotalTokens)
			assert.NotContains(t, recorder.Body.String(), "kkai.recovery.v1")
			assert.Contains(t, recorder.Body.String(), "new answer")
			if tc.wantOld {
				assert.Contains(t, recorder.Body.String(), "old answer")
			} else {
				assert.NotContains(t, recorder.Body.String(), "old answer")
				assert.NotContains(t, recorder.Body.String(), "[regenerating]")
			}
		})
	}
}

func TestManagedRecoveryChatHandlersUseWinnerUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	frames := []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_b","seq":1,"attempt":1}`,
		`{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"old answer"}}]}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_b","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_b","seq":3,"attempt":1}`,
		`{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"[regenerating]"}}]}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_b","seq":4,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_b","seq":5,"attempt":2}`,
		`{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"new answer"}}]}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_b","seq":6,"attempt":2,"outcome":"completed","usage_format":"chat","usage_reported":true,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
		`{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")
	for _, tc := range []struct {
		name    string
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"raw", OaiStreamHandler},
		{"converted", OaiChatToResponsesStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newResponsesChatTestContext(t, body.String(), true)
			c.Set("kkai_managed_recovery_v1", true)
			if tc.name == "converted" {
				info.RelayFormat = types.RelayFormatOpenAIResponses
			}
			usage, apiErr := tc.handler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Zero(t, usage.TotalTokens)
			assert.NotContains(t, recorder.Body.String(), "kkai.recovery.v1")
			assert.Contains(t, recorder.Body.String(), "old answer")
			assert.Contains(t, recorder.Body.String(), "new answer")
		})
	}
}

func TestManagedRecoveryUnreportedUsageEstimatesWinnerOnly(t *testing.T) {
	service.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	frames := []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_c","seq":1,"attempt":1}`,
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`{"type":"response.output_text.delta","delta":"a long abandoned answer with several tokens"}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_c","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_c","seq":3,"attempt":1}`,
		`{"type":"response.output_text.delta","delta":"the gateway is regenerating this answer"}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_c","seq":4,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_c","seq":5,"attempt":2}`,
		`{"type":"response.output_text.delta","delta":"new"}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_c","seq":6,"attempt":2,"outcome":"completed","usage_format":"responses","usage_reported":false,"usage":null}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")
	for _, tc := range []struct {
		name    string
		stream  bool
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"raw", true, OaiResponsesStreamHandler},
		{"converted", true, OaiResponsesToChatStreamHandler},
		{"buffered", false, OaiResponsesToChatBufferedStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newResponsesChatTestContext(t, body.String(), tc.stream)
			c.Set("kkai_managed_recovery_v1", true)
			if tc.name == "raw" {
				info.RelayFormat = types.RelayFormatOpenAIResponses
			}
			want := service.ResponseText2Usage(c, "new", info.UpstreamModelName, info.GetEstimatePromptTokens())
			if tc.name == "raw" {
				want.CompletionTokens = service.CountTextToken("new", info.UpstreamModelName)
			}
			usage, apiErr := tc.handler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, want.CompletionTokens, usage.CompletionTokens)
			assert.False(t, info.BillingHasReportedUsage)
		})
	}
}

func TestManagedRecoveryRefusalDoneUsageIsWinnerOnly(t *testing.T) {
	service.InitTokenEncoders()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	frames := []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_refusal","seq":1,"attempt":1}`,
		`{"type":"response.created","response":{"id":"resp_refusal","model":"gpt-test"}}`,
		`{"type":"response.refusal.delta","item_id":"msg_old","output_index":0,"content_index":0,"delta":"an abandoned refusal that must not be billed"}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_refusal","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_refusal","seq":3,"attempt":1}`,
		`{"type":"response.output_text.delta","delta":"the gateway is regenerating this answer"}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_refusal","seq":4,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_refusal","seq":5,"attempt":2}`,
		`{"type":"response.refusal.delta","item_id":"msg_new","output_index":2,"content_index":0,"delta":"cannot"}`,
		`{"type":"response.refusal.done","item_id":"msg_new","output_index":2,"content_index":0,"refusal":"cannot comply"}`,
		`{"type":"response.refusal.done","item_id":"msg_new","output_index":2,"content_index":0,"refusal":"cannot comply"}`,
		`{"type":"response.refusal.done","item_id":"msg_new","output_index":2,"content_index":1,"refusal":" with that request"}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_refusal","seq":6,"attempt":2,"outcome":"completed","usage_format":"responses","usage_reported":false,"usage":null}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")
	for _, tc := range []struct {
		name    string
		stream  bool
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"raw", true, OaiResponsesStreamHandler},
		{"converted", true, OaiResponsesToChatStreamHandler},
		{"buffered", false, OaiResponsesToChatBufferedStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newResponsesChatTestContext(t, body.String(), tc.stream)
			c.Set("kkai_managed_recovery_v1", true)
			if tc.name == "raw" {
				info.RelayFormat = types.RelayFormatOpenAIResponses
			}
			winner := "cannot comply with that request"
			want := service.ResponseText2Usage(c, winner, info.UpstreamModelName, info.GetEstimatePromptTokens()).CompletionTokens
			if tc.name == "raw" {
				want = service.CountTextToken(winner, info.UpstreamModelName)
			}
			usage, apiErr := tc.handler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, want, usage.CompletionTokens)
			assert.False(t, info.BillingHasReportedUsage)
		})
	}
}

func TestRecoveryV1StateSeparatesAttemptsAndAcceptsZeroUsage(t *testing.T) {
	s := &recoveryV1State{enabled: true}
	frames := []struct {
		data    string
		attempt int
	}{
		{`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`, 1},
		{`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_a","seq":2,"attempt":1}`, 0},
		{`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_a","seq":3,"attempt":1}`, 0},
		{`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_a","seq":4,"attempt":1}`, 0},
		{`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":5,"attempt":2}`, 2},
		{`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":6,"attempt":2,"outcome":"completed","usage_format":"responses","usage_reported":true,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`, 0},
	}
	for _, frame := range frames {
		consumed, err := s.filter(frame.data)
		require.NoError(t, err)
		assert.True(t, consumed)
		assert.Equal(t, frame.attempt, s.meterAttempt())
	}
	_, err := s.filter(`{"type":"response.completed","response":{"status":"completed"}}`)
	require.NoError(t, err)
	require.NoError(t, s.checkEnd())
	assert.True(t, s.usageReported)
	require.NotNil(t, s.usage)
	assert.Zero(t, s.usage.TotalTokens)
}

func TestRecoveryV1StateRejectsInvalidUsageAndUnnegotiatedControl(t *testing.T) {
	for _, data := range []string{
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"chat","usage_reported":true,"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"chat","usage_reported":false,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	} {
		s := &recoveryV1State{enabled: true}
		_, err := s.filter(`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`)
		require.NoError(t, err)
		_, err = s.filter(data)
		require.Error(t, err)
	}
	_, err := (&recoveryV1State{}).filter(`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`)
	require.Error(t, err)
}

func TestRecoveryV1StateChatUsage(t *testing.T) {
	s := &recoveryV1State{enabled: true}
	_, err := s.filter(`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`)
	require.NoError(t, err)
	_, err = s.filter(`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"chat","usage_reported":true,"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
	require.NoError(t, err)
	assert.Equal(t, 7, s.usage.PromptTokens)
	assert.Equal(t, 3, s.usage.CompletionTokens)
}

func TestRecoveryV1RejectsAbandonedDeltasAndIncompletePublicTerminal(t *testing.T) {
	begin := `{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`
	for _, tc := range []struct {
		name   string
		frames []string
	}{
		{"abandoned_delta", []string{
			`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_a","seq":2,"attempt":1}`,
			`{"type":"response.output_text.delta","delta":"must not leak"}`,
		}},
		{"mismatched_outcome", []string{
			`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"responses","usage_reported":false,"usage":null}`,
			`{"type":"response.failed","response":{"status":"failed"}}`,
		}},
		{"missing_public_terminal", []string{
			`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"responses","usage_reported":false,"usage":null}`,
		}},
		{"chat_missing_done", []string{
			`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":2,"attempt":1,"outcome":"completed","usage_format":"chat","usage_reported":false,"usage":null}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recoveryV1State{enabled: true}
			_, err := s.filter(begin)
			require.NoError(t, err)
			for _, frame := range tc.frames {
				_, err = s.filter(frame)
				if err != nil {
					break
				}
			}
			assert.Error(t, s.checkEnd())
		})
	}
}

func TestRecoveryV1AllowsFinalFailureAfterAbandonmentWithoutNewDispatch(t *testing.T) {
	s := &recoveryV1State{enabled: true}
	for _, frame := range []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_a","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_a","seq":3,"attempt":1,"outcome":"failed","usage_format":"responses","usage_reported":false,"usage":null}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","message":"exhausted"}}}`,
	} {
		_, err := s.filter(frame)
		require.NoError(t, err)
	}
	require.NoError(t, s.checkEnd())
	assert.False(t, s.usageReported)
}

func TestRecoveryV1AllowsAllElevenAbandonedAttemptsBeforeFinalFailure(t *testing.T) {
	s := &recoveryV1State{enabled: true}
	seq := 0
	for attempt := 1; attempt <= 11; attempt++ {
		for _, kind := range []string{"attempt_begin", "attempt_abandoned", "synthetic_begin", "synthetic_end"} {
			seq++
			_, err := s.filter(fmt.Sprintf(`{"type":"kkai.recovery.v1","kind":%q,"logical_id":"lr_full","seq":%d,"attempt":%d}`, kind, seq, attempt))
			require.NoError(t, err)
		}
	}
	_, err := s.filter(`{"type":"kkai.recovery.v1","kind":"terminal","logical_id":"lr_full","seq":45,"attempt":11,"outcome":"failed","usage_format":"responses","usage_reported":false,"usage":null}`)
	require.NoError(t, err)
	_, err = s.filter(`{"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","message":"exhausted"}}}`)
	require.NoError(t, err)
	require.NoError(t, s.checkEnd())
}

func TestRecoveryV1AbandonedSummaryClosureCannotReleaseCipherOrToolCalls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"text_done", `{"type":"response.reasoning_summary_text.done","text":"old summary"}`, true},
		{"part_done", `{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":"old summary"}}`, true},
		{"item_done", `{"type":"response.output_item.done","item":{"type":"reasoning","summary":[{"type":"summary_text","text":"old summary"}]}}`, true},
		{"cipher", `{"type":"response.output_item.done","item":{"type":"reasoning","summary":[],"encrypted_content":"old cipher"}}`, false},
		{"part_cipher", `{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","encrypted_content":"old cipher"}}`, false},
		{"tool_call", `{"type":"response.output_item.done","item":{"type":"function_call","call_id":"old_call","name":"charge","arguments":"{}"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recoveryV1State{enabled: true}
			_, err := s.filter(`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_summary","seq":1,"attempt":1}`)
			require.NoError(t, err)
			_, err = s.filter(`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_summary","seq":2,"attempt":1}`)
			require.NoError(t, err)
			_, err = s.filter(tc.data)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestRecoveryV1RealTextMetricExcludesControlsAndSyntheticText(t *testing.T) {
	c, _, _, info := newResponsesChatTestContext(t, "", true)
	c.Set("kkai_managed_recovery_v1", true)
	info.StartTime = time.Unix(1_700_000_000, 0)
	s := newRecoveryV1State(c, info)
	for _, frame := range []string{
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":1,"attempt":1}`,
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_abandoned","logical_id":"lr_a","seq":2,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_begin","logical_id":"lr_a","seq":3,"attempt":1}`,
		`{"type":"response.output_text.delta","delta":"[regenerating]"}`,
		`{"type":"kkai.recovery.v1","kind":"synthetic_end","logical_id":"lr_a","seq":4,"attempt":1}`,
		`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_a","seq":5,"attempt":2}`,
	} {
		_, err := s.filter(frame)
		require.NoError(t, err)
		assert.True(t, s.firstTextAt.IsZero())
	}
	_, err := s.filter(`{"type":"response.output_text.delta","delta":"actual text"}`)
	require.NoError(t, err)
	assert.False(t, s.firstTextAt.IsZero())
	diagnostics, exists := c.Get("kkai_recovery_v1_diagnostics")
	require.True(t, exists)
	assert.Contains(t, diagnostics, "first_real_text_delta_ms")
}

func TestRecoveryV1RealTextMetricIncludesVisibleRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, frame string
	}{
		{"responses", `{"type":"response.refusal.delta","delta":"cannot comply"}`},
		{"chat", `{"choices":[{"index":0,"delta":{"refusal":"cannot comply"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, info := newResponsesChatTestContext(t, "", true)
			c.Set("kkai_managed_recovery_v1", true)
			info.StartTime = time.Now().Add(-time.Second)
			s := newRecoveryV1State(c, info)
			_, err := s.filter(`{"type":"kkai.recovery.v1","kind":"attempt_begin","logical_id":"lr_refusal","seq":1,"attempt":1}`)
			require.NoError(t, err)
			assert.True(t, s.firstTextAt.IsZero())
			_, err = s.filter(tc.frame)
			require.NoError(t, err)
			assert.False(t, s.firstTextAt.IsZero())
			diagnostics, exists := c.Get("kkai_recovery_v1_diagnostics")
			require.True(t, exists)
			assert.Contains(t, diagnostics, "first_real_text_delta_ms")
		})
	}
}
