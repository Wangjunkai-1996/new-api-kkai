package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelayErrorHandlerLimitsImageStudioErrorBody(t *testing.T) {
	ctx := WithImageStudioResponseLimit(context.Background(), 32)
	response := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 33))),
	}

	apiErr := RelayErrorHandler(ctx, response, false)
	require.NotNil(t, apiErr)
	require.Equal(t, ErrImageStudioResponseTooLarge.Error(), apiErr.Error())
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
}
