package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func writeRelayHTTPError(c *gin.Context, relayFormat types.RelayFormat, status int, payload any) error {
	if c == nil || c.Writer == nil {
		return nil
	}
	if c.Writer.Written() {
		if !strings.Contains(strings.ToLower(c.Writer.Header().Get("Content-Type")), "text/event-stream") {
			return nil
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		if relayFormat == types.RelayFormatClaude || relayFormat == types.RelayFormatOpenAIResponses {
			return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "error"}, string(data))
		}
		return helper.StringData(c, string(data))
	}

	helper.ClearEventStreamHeaders(c)
	if status < http.StatusContinue || status > 599 {
		status = http.StatusInternalServerError
	}
	c.JSON(status, payload)
	return nil
}
