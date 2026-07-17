package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

type DeliveryOutcome string

const (
	DeliveryOutcomeDelivered   DeliveryOutcome = "delivered"
	DeliveryOutcomeClientGone  DeliveryOutcome = "client_gone"
	DeliveryOutcomeWriteFailed DeliveryOutcome = "write_failed"
)

const responseDeliveryOutcomeKey = "response_delivery_outcome"

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. It returns false for Content-Length
// (managed separately) and X-Oneapi-Request-Id (to preserve the local instance
// ID). When the upstream header is X-Oneapi-Request-Id, the value is captured
// into the Gin context for later logging.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	if strings.EqualFold(k, "Content-Length") {
		return false
	}
	if strings.EqualFold(k, common.RequestIdKey) || strings.EqualFold(k, common.StandardRequestIdKey) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	return true
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) DeliveryOutcome {
	if c == nil {
		return DeliveryOutcomeWriteFailed
	}
	if c.Writer == nil {
		return recordDeliveryOutcome(c, DeliveryOutcomeWriteFailed)
	}
	if clientRequestCanceled(c) {
		return recordDeliveryOutcome(c, DeliveryOutcomeClientGone)
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	written, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
		return recordDeliveryOutcome(c, failedDeliveryOutcome(c))
	}
	if written != int64(len(data)) {
		logger.LogError(c, fmt.Sprintf("failed to copy complete response body: wrote %d of %d bytes", written, len(data)))
		return recordDeliveryOutcome(c, failedDeliveryOutcome(c))
	}
	c.Writer.Flush()
	if clientRequestCanceled(c) {
		return recordDeliveryOutcome(c, DeliveryOutcomeClientGone)
	}
	return recordDeliveryOutcome(c, DeliveryOutcomeDelivered)
}

func ResponseDeliveryOutcome(c *gin.Context) (DeliveryOutcome, bool) {
	if c == nil {
		return "", false
	}
	value, exists := c.Get(responseDeliveryOutcomeKey)
	if !exists {
		return "", false
	}
	outcome, ok := value.(DeliveryOutcome)
	return outcome, ok
}

func recordDeliveryOutcome(c *gin.Context, outcome DeliveryOutcome) DeliveryOutcome {
	if c != nil {
		c.Set(responseDeliveryOutcomeKey, outcome)
	}
	return outcome
}

func failedDeliveryOutcome(c *gin.Context) DeliveryOutcome {
	if clientRequestCanceled(c) {
		return DeliveryOutcomeClientGone
	}
	return DeliveryOutcomeWriteFailed
}

func clientRequestCanceled(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return c.Request.Context().Err() == context.Canceled
}
