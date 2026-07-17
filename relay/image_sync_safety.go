package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	imageSyncDeadline             = 110 * time.Second
	statusClientClosedRequest     = 499
	cloudflareOriginTimeoutStatus = 524
)

func startImageSyncRequest(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	gate *imageSyncAdmissionGate,
) (func(), *types.NewAPIError) {
	if info.IsStream {
		return func() {}, nil
	}
	lease, admitted := gate.TryAcquire(imageSyncAccountID(info))
	if !admitted {
		c.Header("Retry-After", "1")
		return nil, types.NewErrorWithStatusCode(
			errors.New("synchronous image capacity is saturated"),
			types.ErrorCodeImageSyncConcurrencyExceeded,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	cancel := startImageSyncDeadline(c, imageSyncDeadline)
	return func() {
		cancel()
		lease.Release()
	}, nil
}

func startImageSyncDeadline(c *gin.Context, deadline time.Duration) context.CancelFunc {
	ctx, cancel := context.WithTimeout(c.Request.Context(), deadline)
	c.Request = c.Request.WithContext(ctx)
	return cancel
}

func imageUpstreamRequestError(c *gin.Context, err error) *types.NewAPIError {
	if contextErr := imageContextError(c); contextErr != nil {
		return contextErr
	}
	return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
}

func imageContextError(c *gin.Context) *types.NewAPIError {
	if c != nil && c.Request != nil {
		switch c.Request.Context().Err() {
		case context.DeadlineExceeded:
			return imageDeadlineError()
		case context.Canceled:
			return imageClientGoneError("client disconnected before image generation completed")
		}
	}
	return nil
}

func imageHTTPStatusError(statusCode int) *types.NewAPIError {
	if statusCode == cloudflareOriginTimeoutStatus {
		return imageDeadlineError()
	}
	return nil
}

func imageDeliveryError(c *gin.Context) *types.NewAPIError {
	outcome, tracked := service.ResponseDeliveryOutcome(c)
	if !tracked || outcome == service.DeliveryOutcomeDelivered {
		return nil
	}
	logger.LogError(c, fmt.Sprintf(
		"image response delivery failed outcome=%s upstream_request_id=%s",
		outcome,
		c.GetString(common.UpstreamRequestIdKey),
	))
	if outcome == service.DeliveryOutcomeClientGone {
		return imageClientGoneError("client disconnected before image response was delivered")
	}
	return types.NewErrorWithStatusCode(
		errors.New("image response delivery failed"),
		types.ErrorCodeImageDeliveryFailed,
		http.StatusInternalServerError,
		types.ErrOptionWithSkipRetry(),
	)
}

func imageDeadlineError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("synchronous image request exceeded its deadline"),
		types.ErrorCodeImageSyncDeadlineExceeded,
		http.StatusGatewayTimeout,
		types.ErrOptionWithSkipRetry(),
	)
}

func imageClientGoneError(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(message),
		types.ErrorCodeImageClientGone,
		statusClientClosedRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
