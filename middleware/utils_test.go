package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbortWithAffinityChannelDisabledUsesServiceUnavailable(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 123)
	ctx.Set(common.RequestIdKey, "req-affinity-disabled")

	abortWithAffinityChannelDisabled(ctx)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), string(types.ErrorCodeGetChannelFailed))
	assert.Contains(t, recorder.Body.String(), "req-affinity-disabled")
	assert.True(t, ctx.IsAborted())
}

func TestPublicMiddlewareAPIErrorNoAvailableKeyUsesUnavailable(t *testing.T) {
	apiErr := types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)

	statusCode, message, code := publicMiddlewareAPIError(apiErr)

	assert.Equal(t, http.StatusServiceUnavailable, statusCode)
	assert.Equal(t, types.PublicMessageUpstreamUnavailable, message)
	assert.Equal(t, types.ErrorCodeUpstreamUnavailable, code)
}
