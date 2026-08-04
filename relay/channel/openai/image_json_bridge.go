package openai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const openAIImageJSONKeepaliveInterval = 10 * time.Second

func OpenaiImageJSONBridgeHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid image response"), types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return OpenaiImageHandler(c, info, resp)
	}

	ticker := time.NewTicker(openAIImageJSONKeepaliveInterval)
	defer ticker.Stop()
	return openaiImageJSONBridge(c, info, resp, ticker.C)
}

func openaiImageJSONBridge(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, keepalive <-chan time.Time) (*dto.Usage, *types.NewAPIError) {
	info.StreamStatus = relaycommon.NewStreamStatus()
	setOpenAIImageJSONBridgeHeaders(c)
	c.Status(http.StatusOK)
	if err := writeOpenAIImageJSONBridgeChunk(c, []byte(" ")); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponse)
	}
	info.SetFirstResponseTime()

	readCtx, cancelRead := context.WithCancel(context.Background())
	readResults := make(chan openAIImageSSERead)
	readerDone := make(chan struct{})
	go readOpenAIImageSSE(readCtx, resp.Body, readResults, readerDone)
	defer func() {
		cancelRead()
		service.CloseResponseBodyGracefully(resp)
		<-readerDone
	}()

	state := &openAIImageJSONBridgeState{}

	for {
		select {
		case <-c.Request.Context().Done():
			err := c.Request.Context().Err()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponse)
		case <-keepalive:
			if err := writeOpenAIImageJSONBridgeChunk(c, []byte("\n")); err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
				return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponse)
			}
		case result, ok := <-readResults:
			if !ok {
				return finishOpenAIImageJSONBridge(c, info, state, relaycommon.StreamEndReasonEOF)
			}
			if result.err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, result.err)
				return nil, newOpenAIImageJSONBridgeError(result.err, types.ErrorCodeReadResponseBodyFailed)
			}
			if result.done || result.eof {
				reason := relaycommon.StreamEndReasonEOF
				if result.done {
					reason = relaycommon.StreamEndReasonDone
				}
				return finishOpenAIImageJSONBridge(c, info, state, reason)
			}
			if bridgeErr := state.consume(info, result.data); bridgeErr != nil {
				return nil, bridgeErr
			}
		}
	}
}

func finishOpenAIImageJSONBridge(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	state *openAIImageJSONBridgeState,
	reason relaycommon.StreamEndReason,
) (*dto.Usage, *types.NewAPIError) {
	if len(state.completedImages) == 0 {
		err := fmt.Errorf("upstream image stream ended without a completed image")
		info.StreamStatus.SetEndReason(reason, nil)
		info.StreamStatus.RecordError(err.Error())
		return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeEmptyResponse)
	}

	usage := &dto.Usage{}
	if len(state.usageRaw) > 0 {
		if err := common.Unmarshal(state.usageRaw, usage); err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
			info.StreamStatus.RecordError(err.Error())
			return nil, newOpenAIImageJSONBridgeError(fmt.Errorf("decode upstream image usage failed: %w", err), types.ErrorCodeBadResponseBody)
		}
	}
	normalizeOpenAIUsage(usage)
	applyUsagePostProcessing(info, usage, state.usageRaw)
	updateOpenAIImageCount(info, int64(len(state.completedImages)))

	responseFormat := ""
	if request, ok := info.Request.(*dto.ImageRequest); ok {
		responseFormat = request.ResponseFormat
	}
	if err := writeOpenAIImageJSONResponse(c.Writer, state.completedImages, state.responseMeta, state.usageRaw, state.createdAt, responseFormat); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		info.StreamStatus.RecordError(err.Error())
		return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponse)
	}
	if err := helper.FlushWriter(c); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		info.StreamStatus.RecordError(err.Error())
		return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponse)
	}
	info.StreamStatus.SetEndReason(reason, nil)
	return usage, nil
}

func setOpenAIImageJSONBridgeHeaders(c *gin.Context) {
	header := c.Writer.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	header.Del("Content-Length")
	header.Del("Transfer-Encoding")
}

func writeOpenAIImageJSONBridgeChunk(c *gin.Context, data []byte) error {
	helper.ExtendWriteDeadline(c)
	if _, err := c.Writer.Write(data); err != nil {
		return err
	}
	return helper.FlushWriter(c)
}

func openAIImageJSONBridgeUpstreamError(data []byte) *types.NewAPIError {
	var openAIError types.OpenAIError
	if errorObject := gjson.GetBytes(data, "error"); errorObject.IsObject() {
		_ = common.Unmarshal([]byte(errorObject.Raw), &openAIError)
	}
	if openAIError.Message == "" {
		openAIError.Message = extractOpenAIImageStreamErrorMessage(data)
	}
	if openAIError.Type == "" {
		eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
		if eventType == "" || eventType == "error" {
			eventType = "upstream_error"
		}
		openAIError.Type = eventType
	}
	return types.WithOpenAIError(openAIError, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func newOpenAIImageJSONBridgeError(err error, code types.ErrorCode) *types.NewAPIError {
	return types.NewOpenAIError(err, code, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}
