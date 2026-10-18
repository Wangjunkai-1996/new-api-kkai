package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipResponsesFailureReturnsProviderUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, usage := range []string{"", `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, `,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}`} {
			response := `{"status":"failed","error":{"type":"server_error","message":"generation failed"}` + usage + `}`
			body := response
			if stream {
				body = "data: " + `{"type":"response.failed","response":` + response + "}\n\n"
			}
			c, resp, info := newResponsesStreamHandlerTest(t, body)
			info.MembershipBillingRequestID = "failed-response"
			info.IsStream = stream
			handler := OaiResponsesHandler
			if !stream {
				resp.Header.Set("Content-Type", "application/json")
			} else {
				handler = OaiResponsesStreamHandler
			}
			result, apiErr := handler(c, info, resp)
			require.NotNil(t, apiErr)
			if usage == "" {
				assert.Nil(t, result)
				assert.False(t, info.MembershipHasReportedUsage)
			} else {
				require.NotNil(t, result)
				assert.True(t, info.MembershipHasReportedUsage)
			}
			service.SettleMembershipPartialUsage(c, info, result, apiErr)
			assert.Equal(t, usage == "", info.MembershipOutcomeUnknown)
		}
	}
}
