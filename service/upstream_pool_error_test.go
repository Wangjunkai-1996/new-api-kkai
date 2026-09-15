package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamPoolExhaustedRequiresAdmissionContract(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"account pool", 503, `{"error":{"code":"account_pool_exhausted","type":"api_error","message":"unavailable"}}`, true},
		{"egress capacity", 429, `{"error":{"code":"egress_capacity_exhausted","type":"rate_limit_error","message":"full"}}`, true},
		{"account pool cooling", 429, `{"error":{"code":"account_pool_rate_limited","type":"rate_limit_error","message":"cooling"}}`, true},
		{"legacy unavailable", 503, `{"error":{"type":"api_error","message":"Service temporarily unavailable"}}`, false},
		{"ordinary key limit", 429, `{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"account_pool_exhausted"}}`, false},
		{"wrong status", 502, `{"error":{"code":"account_pool_exhausted","type":"api_error"}}`, false},
		{"wrong type", 503, `{"error":{"code":"account_pool_exhausted","type":"authentication_error"}}`, false},
		{"wrong egress status", 503, `{"error":{"code":"egress_capacity_exhausted","type":"rate_limit_error"}}`, false},
		{"unstructured", 503, `account_pool_exhausted`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := RelayErrorHandler(context.Background(), &http.Response{
				StatusCode: tt.status,
				Header:     http.Header{"Retry-After": []string{"90"}},
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}, false)
			require.NotNil(t, apiErr)
			assert.Equal(t, tt.want, IsUpstreamPoolExhausted(apiErr))
			ResetStatusCode(apiErr, `{"502":503,"503":502,"429":502}`)
			assert.Equal(t, tt.want, IsUpstreamPoolExhausted(apiErr), "local mapping cannot manufacture admission evidence")
			if tt.want {
				assert.Equal(t, "90", apiErr.RetryAfter)
			} else {
				assert.Empty(t, apiErr.RetryAfter)
			}
		})
	}
	local := types.WithOpenAIError(types.OpenAIError{Code: "account_pool_exhausted", Type: "api_error"}, 503)
	assert.False(t, IsUpstreamPoolExhausted(local))
	assert.False(t, IsUpstreamPoolExhausted(nil))
}

func TestUpstreamPoolRetryAfter(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	for _, tt := range []struct{ name, value, want string }{
		{"seconds", "90", "90"},
		{"date", future, future},
		{"negative", "-1", ""},
		{"overflow", "99999999999999999999", ""},
		{"secret text", "refresh_token=must-not-leak", ""},
		{"header injection", "90\r\nX-Secret: value", ""},
		{"past", "Wed, 01 Jan 2020 00:00:00 GMT", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := RelayErrorHandler(context.Background(), &http.Response{
				StatusCode: 503,
				Header:     http.Header{"Retry-After": []string{tt.value}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"account_pool_exhausted","type":"api_error","message":"unavailable"}}`)),
			}, false)
			require.NotNil(t, apiErr)
			assert.Equal(t, tt.want, apiErr.RetryAfter)
		})
	}
}
