package openai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

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
	clientDone := c.Request.Context().Done()

	for {
		select {
		case <-clientDone:
			err := c.Request.Context().Err()
			state.clientGone = true
			state.clientGoneErr = err
			clientDone = nil
			keepalive = nil
		case _, ok := <-keepalive:
			if !ok {
				keepalive = nil
				continue
			}
			// Sending HTTP bytes here would commit a false 200 before the image is valid.
			helper.ExtendWriteDeadline(c)
		case result, ok := <-readResults:
			if !ok {
				return finishOpenAIImageJSONBridge(c, info, state, relaycommon.StreamEndReasonEOF)
			}
			if result.err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, result.err)
				logOpenAIImageJSONBridgeFailure(c, state, relaycommon.StreamEndReasonScannerErr)
				if len(state.completedImages) > 0 {
					logger.LogWarn(c, "OpenAI image JSON bridge read failed after completed output; settling completed generation")
					return finishOpenAIImageJSONBridge(c, info, state, relaycommon.StreamEndReasonScannerErr)
				}
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
				logOpenAIImageJSONBridgeFailure(c, state, info.StreamStatus.EndReason)
				if len(state.completedImages) > 0 {
					logger.LogWarn(c, "OpenAI image JSON bridge received a terminal error after completed output; settling completed generation")
					return finishOpenAIImageJSONBridge(c, info, state, relaycommon.StreamEndReasonHandlerStop)
				}
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
	if !state.clientGone {
		if err := c.Request.Context().Err(); err != nil {
			state.clientGone = true
			state.clientGoneErr = err
		}
	}
	if len(state.completedImages) == 0 {
		err := fmt.Errorf("upstream image stream ended without a completed image")
		info.StreamStatus.SetEndReason(reason, nil)
		info.StreamStatus.RecordError(err.Error())
		logOpenAIImageJSONBridgeFailure(c, state, reason)
		return nil, newOpenAIImageJSONBridgeError(err, types.ErrorCodeEmptyResponse)
	}

	usage := &dto.Usage{}
	if len(state.usageRaw) > 0 {
		if err := common.Unmarshal(state.usageRaw, usage); err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
			info.StreamStatus.RecordError(err.Error())
			logOpenAIImageJSONBridgeFailure(c, state, relaycommon.StreamEndReasonHandlerStop)
			logger.LogWarn(c, "OpenAI image JSON bridge could not decode usage after completed output; settling from completed image count")
			state.usageRaw = nil
			usage = &dto.Usage{}
		}
	}
	normalizeOpenAIUsage(usage)
	applyUsagePostProcessing(info, usage, state.usageRaw)
	updateOpenAIImageCount(info, int64(len(state.completedImages)))

	responseFormat := ""
	if request, ok := info.Request.(*dto.ImageRequest); ok {
		responseFormat = request.ResponseFormat
	}
	helper.ExtendWriteDeadline(c)
	info.SetFirstResponseTime()
	if err := writeOpenAIImageJSONResponse(c.Writer, state.completedImages, state.responseMeta, state.usageRaw, state.createdAt, responseFormat); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		info.StreamStatus.RecordError(err.Error())
		logger.LogWarn(c, "OpenAI image JSON bridge completed upstream but downstream write failed; settling completed generation: "+state.diagnosticSummary(relaycommon.StreamEndReasonClientGone))
		return usage, nil
	}
	if err := helper.FlushWriter(c); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		info.StreamStatus.RecordError(err.Error())
		logger.LogWarn(c, "OpenAI image JSON bridge completed upstream but downstream flush failed; settling completed generation: "+state.diagnosticSummary(relaycommon.StreamEndReasonClientGone))
		return usage, nil
	}
	if state.clientGone {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, state.clientGoneErr)
		logger.LogWarn(c, "OpenAI image JSON bridge completed after client disconnect; captured completed upstream generation for settlement: "+state.diagnosticSummary(relaycommon.StreamEndReasonClientGone))
		return usage, nil
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

func logOpenAIImageJSONBridgeFailure(c *gin.Context, state *openAIImageJSONBridgeState, reason relaycommon.StreamEndReason) {
	logger.LogWarn(c, "OpenAI image JSON bridge terminal failure: "+state.diagnosticSummary(reason))
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
