package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipResponsesFailureReturnsProviderUsage(t *testing.T) {
	testDurableResponsesFailureUsage(t, false)
}

func TestTenantResponsesFailureReturnsProviderUsage(t *testing.T) {
	testDurableResponsesFailureUsage(t, true)
}

func testDurableResponsesFailureUsage(t *testing.T, tenant bool) {
	t.Helper()
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			usage    string
			reported bool
		}{
			{"", false},
			{`,"usage":{}`, false},
			{`,"usage":{"input_tokens":0}`, false},
			{`,"usage":{"output_tokens":0}`, false},
			{`,"usage":{"input_tokens":null,"output_tokens":0}`, false},
			{`,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, true},
			{`,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}`, true},
		} {
			usage := tc.usage
			response := `{"status":"failed","error":{"type":"server_error","message":"generation failed"}` + usage + `}`
			body := response
			if stream {
				body = "data: " + `{"type":"response.failed","response":` + response + "}\n\n"
			}
			c, resp, info := newResponsesStreamHandlerTest(t, body)
			info.MembershipBillingRequestID = "failed-response"
			if tenant {
				info.MembershipBillingRequestID = ""
				info.TenantID, info.TenantQuoteID = 42, 1
			}
			info.IsStream = stream
			handler := OaiResponsesHandler
			if !stream {
				resp.Header.Set("Content-Type", "application/json")
			} else {
				handler = OaiResponsesStreamHandler
			}
			result, apiErr := handler(c, info, resp)
			require.NotNil(t, apiErr)
			if !tc.reported {
				assert.Nil(t, result)
				assert.False(t, info.BillingHasReportedUsage)
			} else {
				require.NotNil(t, result)
				assert.True(t, info.BillingHasReportedUsage)
			}
			service.CaptureFailedRequestUsage(c, info, result, apiErr)
			assert.Equal(t, !tc.reported, info.BillingOutcomeUnknown)
		}
	}
}
