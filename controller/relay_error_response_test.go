package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	openaiRelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRelayHTTPErrorWritesIncompleteImageBridgeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"type":"image_generation.partial_image","b64_json":"partial-secret"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		Request:     &dto.ImageRequest{},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	usage, bridgeErr := openaiRelay.OpenaiImageJSONBridgeHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, bridgeErr)
	assert.False(t, c.Writer.Written())
	require.NoError(t, writeRelayHTTPError(c, types.RelayFormatOpenAI, bridgeErr.StatusCode, gin.H{
		"error": bridgeErr.ToOpenAIError(),
	}))
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	assert.NotContains(t, recorder.Body.String(), "partial-secret")
	var errorBody struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &errorBody))
	assert.Equal(t, "upstream image stream ended without a completed image", errorBody.Error.Message)
	assert.Equal(t, string(types.ErrorCodeEmptyResponse), errorBody.Error.Code)
}

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
