package openai

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These bytes were captured from Sub2's actual writer and physical-dispatch
// observer with capture_test.go.txt, rather than constructed by this consumer.
func TestManagedRecoveryCapturedSub2Responses(t *testing.T) {
	service.InitTokenEncoders()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, fixture := range []struct {
		name      string
		winner    string
		reported  bool
		wantTotal int
		failed    bool
	}{
		{"responses_auto_dispatch", "winner text", true, 5, false},
		{"responses_reasoning_recovery", "winner text", true, 5, false},
		{"responses_terminal_only", "winner text", false, 0, false},
		{"responses_strict", `{"ok":true}`, false, 0, false},
		{"responses_abandoned_failure", "", false, 0, true},
		{"responses_exhausted", "", false, 0, true},
		{"responses_refusal_recovery", "winner text", true, 5, false},
		{"responses_strict_refusal_done_only", "cannot comply", false, 0, false},
		{"responses_refusal_partial_done", "cannot comply with that request", false, 0, false},
		{"responses_refusal_item_snapshot", "cannot comply", false, 0, false},
		{"responses_multi_part_recovery", "winner text", true, 5, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "sub2_recovery", fixture.name+".sse"))
			require.NoError(t, err)
			body := string(data)
			publicEvents := 0
			createdEvents := 0
			controls := 0
			for _, line := range strings.Split(body, "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				event := strings.TrimPrefix(line, "data: ")
				if gjson.Get(event, "type").String() == recoveryControlType {
					controls++
					continue
				}
				if publicEvents == 0 {
					assert.Equal(t, "response.created", gjson.Get(event, "type").String())
				}
				if gjson.Get(event, "type").String() == "response.created" {
					createdEvents++
				}
				assert.Equal(t, int64(publicEvents), gjson.Get(event, "sequence_number").Int())
				publicEvents++
			}
			assert.Equal(t, 1, createdEvents)
			if fixture.name == "responses_multi_part_recovery" {
				assert.Contains(t, body, `"type":"response.refusal.done"`)
				assert.Contains(t, body, `"type":"response.output_item.done"`)
			}
			if fixture.name == "responses_exhausted" {
				assert.Equal(t, 45, controls)
			}
			for _, handler := range []struct {
				name   string
				stream bool
				format types.RelayFormat
				call   func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
			}{
				{"raw", true, types.RelayFormatOpenAIResponses, OaiResponsesStreamHandler},
				{"chat", true, types.RelayFormatOpenAI, OaiResponsesToChatStreamHandler},
				{"buffered", false, types.RelayFormatOpenAI, OaiResponsesToChatBufferedStreamHandler},
			} {
				t.Run(handler.name, func(t *testing.T) {
					c, recorder, resp, info := newResponsesChatTestContext(t, body, handler.stream)
					c.Set("kkai_managed_recovery_v1", true)
					info.RelayFormat = handler.format
					info.RelayMode = relayconstant.RelayModeResponses
					usage, apiErr := handler.call(c, info, resp)
					assert.NotContains(t, recorder.Body.String(), recoveryControlType)
					assert.NotContains(t, recorder.Body.String(), "abandoned cipher")
					if fixture.failed {
						require.NotNil(t, apiErr)
						assert.Equal(t, types.ErrorCode("recovery_exhausted"), apiErr.GetOriginalErrorCode())
						assert.Nil(t, usage)
						return
					}
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Equal(t, fixture.reported, info.BillingHasReportedUsage)
					if fixture.reported {
						assert.Equal(t, fixture.wantTotal, usage.TotalTokens)
					} else {
						want := service.ResponseText2Usage(c, fixture.winner, info.UpstreamModelName, 0).CompletionTokens
						if handler.name == "raw" {
							want = service.CountTextToken(fixture.winner, info.UpstreamModelName)
						}
						assert.Equal(t, want, usage.CompletionTokens)
					}
					assert.True(t, info.StreamStatus.IsNormalEnd())
					if fixture.name == "responses_refusal_partial_done" || fixture.name == "responses_refusal_item_snapshot" {
						if !handler.stream {
							assert.Equal(t, fixture.winner, gjson.Get(recorder.Body.String(), "choices.0.message.refusal").String())
						} else {
							var refusal strings.Builder
							for _, line := range strings.Split(recorder.Body.String(), "\n") {
								if !strings.HasPrefix(line, "data: ") {
									continue
								}
								event := strings.TrimPrefix(line, "data: ")
								if handler.name == "chat" {
									refusal.WriteString(gjson.Get(event, "choices.0.delta.refusal").String())
								} else if gjson.Get(event, "type").String() == "response.refusal.delta" {
									refusal.WriteString(gjson.Get(event, "delta").String())
								}
							}
							assert.Equal(t, fixture.winner, refusal.String())
						}
					}
					if (fixture.name == "responses_auto_dispatch" || fixture.name == "responses_reasoning_recovery" || fixture.name == "responses_multi_part_recovery") && !handler.stream {
						assert.NotContains(t, recorder.Body.String(), "abandoned text")
						assert.NotContains(t, recorder.Body.String(), "old refusal")
						assert.NotContains(t, recorder.Body.String(), "old summary")
						assert.NotContains(t, recorder.Body.String(), "Regenerating")
					}
				})
			}
		})
	}
}

func TestManagedRecoveryCapturedSub2RefusalPreservesNativeSemantics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		stream  bool
		format  types.RelayFormat
		mode    int
		call    func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"responses_raw", "responses_strict_refusal", true, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, OaiResponsesStreamHandler},
		{"responses_to_chat", "responses_strict_refusal", true, types.RelayFormatOpenAI, relayconstant.RelayModeResponses, OaiResponsesToChatStreamHandler},
		{"responses_buffered", "responses_strict_refusal", false, types.RelayFormatOpenAI, relayconstant.RelayModeResponses, OaiResponsesToChatBufferedStreamHandler},
		{"responses_done_only_raw", "responses_strict_refusal_done_only", true, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, OaiResponsesStreamHandler},
		{"responses_done_only_to_chat", "responses_strict_refusal_done_only", true, types.RelayFormatOpenAI, relayconstant.RelayModeResponses, OaiResponsesToChatStreamHandler},
		{"responses_done_only_buffered", "responses_strict_refusal_done_only", false, types.RelayFormatOpenAI, relayconstant.RelayModeResponses, OaiResponsesToChatBufferedStreamHandler},
		{"chat_raw", "chat_strict_refusal", true, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, OaiStreamHandler},
		{"chat_to_responses", "chat_strict_refusal", true, types.RelayFormatOpenAIResponses, relayconstant.RelayModeChatCompletions, OaiChatToResponsesStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "sub2_recovery", tc.fixture+".sse"))
			require.NoError(t, err)
			c, recorder, resp, info := newResponsesChatTestContext(t, string(data), tc.stream)
			c.Set("kkai_managed_recovery_v1", true)
			info.RelayFormat, info.RelayMode = tc.format, tc.mode
			info.ChannelSetting.ForceFormat = true
			usage, apiErr := tc.call(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.True(t, info.StreamStatus.IsNormalEnd())
			body := recorder.Body.String()
			assert.Contains(t, body, `"refusal":"cannot comply"`)
			assert.NotContains(t, body, `"text":"cannot comply"`)
			assert.NotContains(t, body, `"content":"cannot comply"`)
			assert.NotContains(t, body, "Regenerating")
			assert.NotContains(t, body, recoveryControlType)
			if tc.format == types.RelayFormatOpenAIResponses {
				assert.Contains(t, body, "event: response.refusal.delta")
				assert.Contains(t, body, "event: response.refusal.done")
			}
		})
	}
}

func TestManagedRecoveryCapturedSub2Chat(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, fixture := range []struct {
		name  string
		n     int
		total int
	}{
		{"chat_multi", 2, 10}, {"chat_strict", 1, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "sub2_recovery", fixture.name+".sse"))
			require.NoError(t, err)
			c, recorder, resp, info := newResponsesChatTestContext(t, string(data), true)
			c.Set("kkai_managed_recovery_v1", true)
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.Request = &dto.GeneralOpenAIRequest{N: &fixture.n}
			usage, apiErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, fixture.total, usage.TotalTokens)
			assert.NotContains(t, recorder.Body.String(), recoveryControlType)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			assert.Equal(t, fixture.n, strings.Count(recorder.Body.String(), `"finish_reason":"stop"`))
			assert.True(t, info.StreamStatus.IsNormalEnd())
			if fixture.name == "chat_strict" {
				assert.NotContains(t, recorder.Body.String(), "invalid JSON")
				assert.NotContains(t, recorder.Body.String(), "Regenerating")
				t.Run("responses", func(t *testing.T) {
					c, recorder, resp, info := newResponsesChatTestContext(t, string(data), true)
					c.Set("kkai_managed_recovery_v1", true)
					info.RelayFormat = types.RelayFormatOpenAIResponses
					info.RelayMode = relayconstant.RelayModeChatCompletions
					usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Zero(t, usage.TotalTokens)
					assert.True(t, info.BillingHasReportedUsage)
					assert.True(t, info.StreamStatus.IsNormalEnd())
					body := recorder.Body.String()
					assert.NotContains(t, body, recoveryControlType)
					assert.NotContains(t, body, "invalid JSON")
					assert.NotContains(t, body, "Regenerating")
					assert.Equal(t, 1, strings.Count(body, "event: response.created\n"))
					assert.Equal(t, 1, strings.Count(body, "event: response.completed\n"))
					assert.Contains(t, body, `"text":"{\"ok\":true}"`)
				})
			}
		})
	}
}

func TestManagedRecoveryCapturedSub2ToolFailures(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, tc := range []struct {
		name, toolEvent string
		mode            int
		format          types.RelayFormat
		handler         func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"responses_tool_failure", "response.output_item.added", relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses, OaiResponsesStreamHandler},
		{"responses_tool_failure_to_chat", "tool_calls", relayconstant.RelayModeResponses, types.RelayFormatOpenAI, OaiResponsesToChatStreamHandler},
		{"chat_tool_failure", "tool_calls", relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI, OaiStreamHandler},
		{"chat_tool_failure_to_responses", "response.output_item.added", relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAIResponses, OaiChatToResponsesStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := strings.TrimSuffix(strings.TrimSuffix(tc.name, "_to_chat"), "_to_responses")
			data, err := os.ReadFile(filepath.Join("testdata", "sub2_recovery", fixture+".sse"))
			require.NoError(t, err)
			c, recorder, resp, info := newResponsesChatTestContext(t, string(data), true)
			c.Set("kkai_managed_recovery_v1", true)
			info.RelayMode = tc.mode
			info.RelayFormat = tc.format
			_, apiErr := tc.handler(c, info, resp)
			require.NotNil(t, apiErr)
			body := recorder.Body.String()
			assert.Contains(t, body, tc.toolEvent)
			assert.NotContains(t, body, recoveryControlType)
			assert.NotContains(t, body, "Regenerating")
			assert.NotContains(t, body, "response.completed")
			assert.NotContains(t, body, "data: [DONE]")
		})
	}
}
