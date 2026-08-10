package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteRelayHTTPErrorRestoresJSONBeforeFirstStreamFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	helper.SetEventStreamHeaders(c)

	require.NoError(t, writeRelayHTTPError(c, types.RelayFormatOpenAIResponses, http.StatusServiceUnavailable, gin.H{
		"error": gin.H{"code": types.ErrorCodePolicyAuditUnavailable},
	}))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.JSONEq(t, `{"error":{"code":"policy_audit_unavailable"}}`, recorder.Body.String())
}

func TestWriteRelayHTTPErrorKeepsStartedStreamProtocolValid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	helper.SetEventStreamHeaders(c)
	require.NoError(t, helper.StringData(c, `{"choices":[{"delta":{"content":"ok"}}]}`))

	require.NoError(t, writeRelayHTTPError(c, types.RelayFormatOpenAI, http.StatusForbidden, gin.H{
		"error": gin.H{"code": types.ErrorCodeRequestPolicyWarning, "message": "request stopped"},
	}))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.Equal(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"error\":{\"code\":\"request_policy_warning\",\"message\":\"request stopped\"}}\n\n",
		recorder.Body.String(),
	)
}
