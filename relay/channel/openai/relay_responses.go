package openai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var (
	responsesStreamCredentialPattern = regexp.MustCompile(`(?i)\b(?:authorization|proxy-authorization|x-api-key|api[-_ ]?key|apikey|upstream[-_ ]?key|client[-_ ]?token|access[-_ ]?token|refresh[-_ ]?token|client[-_ ]?secret|secret[-_ ]?key)\b["']?\s*[:=]\s*["']?(?:bearer[ \t]+)?[^\s,;)}\]"']+`)
	responsesStreamBearerPattern     = regexp.MustCompile(`(?i)\bbearer[ \t]+[^\s,;)}\]]+`)
	responsesStreamSKPattern         = regexp.MustCompile(`(?i)\bsk-[a-z0-9][a-z0-9._~+/=-]*`)
)

const (
	responsesStreamDiagnosticMessageLimit = common.LocalLogContentLimit
	responsesStreamDiagnosticFieldLimit   = 256
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil {
		return nil, service.NewKKAIStructuredRelayError(oaiError)
	}
	if responsesResponse.IncompleteDetails != nil &&
		strings.EqualFold(strings.TrimSpace(responsesResponse.IncompleteDetails.Reason), "content_filter") {
		var responseStatus string
		if err := common.Unmarshal(responsesResponse.Status, &responseStatus); err == nil &&
			strings.EqualFold(strings.TrimSpace(responseStatus), "incomplete") {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_responses_incomplete_reason=content_filter")
		}
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var streamErr *types.NewAPIError
	sawSuccessfulTerminal := false
	resetResponsesStreamDiagnostics(c, resp.StatusCode)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamEventCount,
			common.GetContextKeyInt(c, constant.ContextKeyResponsesStreamEventCount)+1)

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			markResponsesStreamFailure(c, "stream.decode_error", "", err.Error(), true)
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if responsesStreamEventStartsSemanticOutput(&streamResponse, data) {
			common.SetContextKey(c, constant.ContextKeyResponsesStreamOutputStarted, true)
		}
		if strings.HasPrefix(streamResponse.Type, "response.") || streamResponse.Type == "error" {
			switch streamResponse.Type {
			case "response.failed", "response.incomplete", "response.error", "response.cancelled", "response.canceled", "error":
				common.SetContextKey(c, constant.ContextKeyResponsesStreamFailedEventType, sanitizeResponsesStreamDiagnosticField(streamResponse.Type))
				if openAIError := streamResponse.GetOpenAIError(); openAIError != nil {
					common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorCode, sanitizeResponsesStreamDiagnosticField(responsesStreamErrorCode(openAIError)))
					common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorMessage, sanitizeResponsesStreamDiagnosticMessage(openAIError.Message))
				}
			}
		}
		if upstreamErr := responsesStreamError(&streamResponse); upstreamErr != nil {
			markResponsesStreamFailure(c, streamResponse.Type, responsesStreamErrorCode(streamResponse.GetOpenAIError()), upstreamErr.Error(), true)
			streamErr = upstreamErr
			sr.Stop(upstreamErr)
			return
		}
		switch streamResponse.Type {
		case "response.completed", "response.done":
			sawSuccessfulTerminal = true
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CacheWriteTokens = streamResponse.Response.Usage.InputTokensDetails.CacheWriteTokens
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
			sr.Done()
		case "response.failed", "response.incomplete", "response.error", "response.cancelled", "response.canceled", "error":
			message := fmt.Sprintf("responses stream ended with %s", streamResponse.Type)
			if streamResponse.Response != nil && streamResponse.Response.IncompleteDetails != nil {
				if reason := strings.TrimSpace(streamResponse.Response.IncompleteDetails.Reason); reason != "" {
					message += fmt.Sprintf(" (reason=%s)", reason)
				}
			}
			markResponsesStreamFailure(c, streamResponse.Type, responsesStreamErrorCode(streamResponse.GetOpenAIError()), message, true)
			streamErr = types.NewOpenAIError(
				errors.New(message),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
				types.ErrOptionWithSkipRetry(),
			)
			sr.Stop(streamErr)
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
		if streamErr == nil {
			sendResponsesStreamData(c, streamResponse, data)
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if !sawSuccessfulTerminal {
		endReason := relaycommon.StreamEndReasonNone
		var endErr error
		if info.StreamStatus != nil {
			endReason = info.StreamStatus.EndReason
			endErr = info.StreamStatus.EndError
		}
		if endReason == relaycommon.StreamEndReasonClientGone {
			// A client disconnect is not an upstream response failure. Keep the
			// usage collected so far and let the normal billing path settle it.
		} else {
			message := fmt.Sprintf(
				"responses stream ended without a successful terminal event (reason=%s, received=%d)",
				endReason,
				info.ReceivedResponseCount,
			)
			if endErr != nil {
				message += fmt.Sprintf(": %v", endErr)
			}
			markResponsesStreamFailure(c, "stream.no_successful_terminal", "", message, true)
			streamErr = types.NewOpenAIError(
				errors.New(message),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
				types.ErrOptionWithSkipRetry(),
			)
			if info.StreamStatus != nil {
				info.StreamStatus.RecordError(streamErr.Error())
			}
			return nil, streamErr
		}
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
			common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

// responsesStreamEventStartsSemanticOutput uses the original event JSON rather
// than the narrow DTO. Responses has several output-bearing fields that are not
// represented by ResponsesStreamResponse (for example arguments, input,
// encrypted_content, and partial_image_b64). The detector is deliberately
// conservative: an unknown non-preamble event is treated as output so a later
// failure cannot accidentally be replayed after bytes that may be meaningful.
func responsesStreamEventStartsSemanticOutput(response *dto.ResponsesStreamResponse, rawData string) bool {
	if response == nil {
		return false
	}
	rawData = strings.TrimSpace(rawData)
	if rawData == "" || rawData == "[DONE]" || !gjson.Valid(rawData) {
		return false
	}
	eventType := strings.TrimSpace(response.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(gjson.Get(rawData, "type").String())
	}
	if eventType == "" {
		return false
	}

	switch eventType {
	case "response.created", "response.in_progress":
		return false
	case "response.failed", "response.incomplete", "response.error", "response.cancelled", "response.canceled", "error":
		// Error frames are terminal control messages. They must not turn a
		// pre-output failure into an apparent semantic response.
		return false
	case "response.output_item.added":
		return responsesStreamAddedItemStartsOutput(gjson.Get(rawData, "item"))
	case "response.reasoning_summary_part.added":
		return responsesStreamAddedPartStartsOutput(gjson.Get(rawData, "part"), true)
	case "response.content_part.added":
		return responsesStreamAddedPartStartsOutput(gjson.Get(rawData, "part"), false)
	case "response.output_item.done", "response.content_part.done":
		// A done event can carry the complete item/part even when no delta was
		// emitted. Keep the replay guard conservative for this boundary.
		return true
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.function_call_arguments.delta", "response.audio_transcript.delta", "response.audio.delta", "response.custom_tool_call_input.delta", "response.refusal.delta":
		return responsesStreamHasNonEmptyString(rawData, "delta")
	case "response.output_text.done", "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.audio_transcript.done":
		return responsesStreamHasNonEmptyString(rawData, "text")
	case "response.function_call_arguments.done":
		return responsesStreamHasNonEmptyString(rawData, "arguments")
	case "response.custom_tool_call_input.done":
		return responsesStreamHasNonEmptyString(rawData, "input")
	case "response.image_generation_call.partial_image":
		return responsesStreamHasNonEmptyString(rawData, "partial_image_b64")
	case "response.reasoning_summary_part.done":
		return responsesStreamPartHasText(gjson.Get(rawData, "part"))
	case "response.completed", "response.done":
		for _, item := range gjson.Get(rawData, "response.output").Array() {
			if responsesStreamItemHasOutput(item) {
				return true
			}
		}
		return false
	default:
		// Preserve the same safety direction as Sub2API's stream guard for
		// future Responses event types.
		return true
	}
}

func responsesStreamHasNonEmptyString(rawData, path string) bool {
	value := gjson.Get(rawData, path)
	return value.Exists() && value.String() != ""
}

func responsesStreamPartHasText(part gjson.Result) bool {
	if !part.Exists() || !part.IsObject() {
		return false
	}
	return part.Get("text").String() != "" ||
		part.Get("transcript").String() != "" ||
		part.Get("refusal").String() != ""
}

func responsesStreamAddedPartStartsOutput(part gjson.Result, reasoning bool) bool {
	if !part.Exists() || !part.IsObject() {
		return true
	}
	partType := strings.TrimSpace(part.Get("type").String())
	if reasoning && partType != "summary_text" {
		return true
	}
	switch partType {
	case "output_text":
		return part.Get("text").String() != ""
	case "refusal":
		return part.Get("refusal").String() != ""
	case "summary_text":
		return part.Get("text").String() != ""
	default:
		return true
	}
}

func responsesStreamAddedItemStartsOutput(item gjson.Result) bool {
	if !item.Exists() || !item.IsObject() {
		return true
	}
	switch strings.TrimSpace(item.Get("type").String()) {
	case "reasoning":
		if item.Get("encrypted_content").String() != "" {
			return true
		}
		summary := item.Get("summary")
		if !summary.IsArray() {
			return false
		}
		for _, part := range summary.Array() {
			if strings.TrimSpace(part.Get("type").String()) != "summary_text" ||
				part.Get("text").String() != "" {
				return true
			}
		}
		return false
	case "message":
		content := item.Get("content")
		if !content.IsArray() {
			return false
		}
		for _, part := range content.Array() {
			switch strings.TrimSpace(part.Get("type").String()) {
			case "output_text":
				if part.Get("text").String() != "" {
					return true
				}
			case "refusal":
				if part.Get("refusal").String() != "" {
					return true
				}
			default:
				return true
			}
		}
		return false
	case "function_call":
		return item.Get("arguments").String() != ""
	case "custom_tool_call":
		return item.Get("input").String() != ""
	case "compaction":
		return item.Get("encrypted_content").String() != ""
	default:
		return true
	}
}

func responsesStreamItemHasOutput(item gjson.Result) bool {
	if !item.Exists() || !item.IsObject() {
		return false
	}
	for _, path := range []string{"arguments", "input", "result", "encrypted_content"} {
		if item.Get(path).String() != "" {
			return true
		}
	}
	for _, path := range []string{"content", "summary"} {
		for _, part := range item.Get(path).Array() {
			if responsesStreamPartHasText(part) {
				return true
			}
		}
	}
	return false
}

func resetResponsesStreamDiagnostics(c *gin.Context, upstreamStatusCode int) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyResponsesStreamFailedEventType, "")
	common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorCode, "")
	common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorMessage, "")
	common.SetContextKey(c, constant.ContextKeyResponsesStreamOutputStarted, false)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamEventCount, 0)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamStatusCode, upstreamStatusCode)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamTerminalError, false)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamRetryAllowed, false)
}

func markResponsesStreamFailure(c *gin.Context, eventType, code, message string, terminal bool) {
	if c == nil {
		return
	}
	if eventType = sanitizeResponsesStreamDiagnosticField(eventType); eventType != "" {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamFailedEventType, eventType)
	}
	if code = sanitizeResponsesStreamDiagnosticField(code); code != "" && code != "<nil>" {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorCode, code)
	}
	if message = sanitizeResponsesStreamDiagnosticMessage(message); message != "" {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamUpstreamErrorMessage, message)
	}
	common.SetContextKey(c, constant.ContextKeyResponsesStreamTerminalError, terminal)
	// Responses terminal errors are intentionally marked SkipRetry by the
	// existing relay contract; keep this diagnostic explicit for operators.
	common.SetContextKey(c, constant.ContextKeyResponsesStreamRetryAllowed, false)
}

func responsesStreamErrorCode(openAIError *types.OpenAIError) string {
	if openAIError == nil || openAIError.Code == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(openAIError.Code))
}

func sanitizeResponsesStreamDiagnosticField(value string) string {
	value = maskResponsesStreamCredentials(strings.TrimSpace(value))
	return truncateResponsesStreamDiagnostic(value, responsesStreamDiagnosticFieldLimit)
}

func sanitizeResponsesStreamDiagnosticMessage(value string) string {
	value = common.MaskSensitiveInfo(strings.TrimSpace(value))
	value = maskResponsesStreamCredentials(value)
	return truncateResponsesStreamDiagnostic(value, responsesStreamDiagnosticMessageLimit)
}

func maskResponsesStreamCredentials(value string) string {
	if value == "" {
		return value
	}
	value = responsesStreamCredentialPattern.ReplaceAllString(value, "[redacted]")
	value = responsesStreamBearerPattern.ReplaceAllString(value, "[redacted]")
	value = responsesStreamSKPattern.ReplaceAllString(value, "[redacted]")
	return value
}

func truncateResponsesStreamDiagnostic(value string, limit int) string {
	if value == "" {
		return ""
	}
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const truncatedSuffix = "... [truncated]"
	if len(truncatedSuffix) >= limit {
		return value[:limit]
	}
	prefixLimit := limit - len(truncatedSuffix)
	for prefixLimit > 0 && !utf8.RuneStart(value[prefixLimit]) {
		prefixLimit--
	}
	return value[:prefixLimit] + truncatedSuffix
}
