package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesCompactionHandlerRejectsPolicyErrorWithoutType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name          string
		code          types.ErrorCode
		wantStatus    int
		wantCausality string
	}{
		{name: "local audit unavailable", code: types.ErrorCodePolicyAuditUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "confirmed cyber", code: "cyber_policy", wantStatus: http.StatusForbidden, wantCausality: service.KKAIPolicyCausalityClientToken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"code":"` + string(test.code) + `","message":"request rejected"}}`,
				)),
			}

			usage, apiErr := OaiResponsesCompactionHandler(c, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, test.code, apiErr.GetErrorCode())
			require.Equal(t, test.wantStatus, apiErr.StatusCode)
			require.True(t, types.IsSkipRetryError(apiErr))
			classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
			require.Equal(t, test.wantCausality != "", classification.Detected)
			require.Equal(t, test.wantCausality, classification.Causality)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestOaiResponsesCompactionHandlerAllowsEmptyErrorObject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{"id":"resp_1","object":"response.compaction","output":[],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5},"error":{}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Equal(t, 5, usage.TotalTokens)
	require.JSONEq(t, body, recorder.Body.String())
}
