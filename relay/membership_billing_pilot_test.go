package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipPilotTextHTTPRequests(t *testing.T) {
	const chat = `{"model":"test","messages":[{"role":"user","content":"hello"}]}`
	for _, tc := range []struct {
		name, path, body string
		format           types.RelayFormat
		eligible         bool
	}{
		{"chat", "/v1/chat/completions", chat, types.RelayFormatOpenAI, true},
		{"streaming claude text blocks", "/v1/messages", `{"stream":true,"max_tokens":4000,"system":[{"type":"text","text":"hello"}],"messages":[{"role":"user","content":[{"type":"text","text":"world"}]}]}`, types.RelayFormatClaude, true},
		{"responses string", "/v1/responses", `{"input":"hello","max_output_tokens":32768}`, types.RelayFormatOpenAIResponses, true},
		{"responses history", "/v1/responses", `{"input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"role":"user","content":"hi"}],"tools":[]}`, types.RelayFormatOpenAIResponses, true},
		{"responses async", "/v1/responses", `{"input":"hello","background":true}`, types.RelayFormatOpenAIResponses, false},
		{"responses file ID", "/v1/responses", `{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_x"}]}]}`, types.RelayFormatOpenAIResponses, false},
		{"responses previous context", "/v1/responses", `{"input":"hello","previous_response_id":"resp_x"}`, types.RelayFormatOpenAIResponses, false},
		{"responses extra field", "/v1/responses", `{"input":"hello","messages":[]}`, types.RelayFormatOpenAIResponses, false},
		{"responses ambiguous body", "/v1/responses", `{"input":[{"role":"user","content":[{"type":"input_file","file_id":"f"}],"content":"hello"}]}`, types.RelayFormatOpenAIResponses, false},
		{"chat image", "/v1/chat/completions", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:abc"}}]}]}`, types.RelayFormatOpenAI, false},
		{"chat tools", "/v1/chat/completions", `{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search"}]}`, types.RelayFormatOpenAI, false},
		{"claude tools history", "/v1/messages", `{"messages":[{"role":"user","content":[{"type":"tool_result","content":"hello"}]}]}`, types.RelayFormatClaude, false},
		{"compact", "/v1/responses/compact", `{"input":"hello"}`, types.RelayFormatOpenAIResponses, false},
		{"playground", "/pg/chat/completions", chat, types.RelayFormatOpenAI, false},
		{"mismatched format", "/v1/messages", chat, types.RelayFormatOpenAI, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{MembershipHTTPRelay: true, RelayFormat: tc.format}
			meta := &types.TokenCountMeta{}
			assert.Equal(t, tc.eligible, membershipTextHTTPRequest(c, info, meta))
			if tc.eligible {
				info.MembershipHTTPRelay = false
				assert.False(t, membershipTextHTTPRequest(c, info, meta), "alternate transports")
				info.MembershipHTTPRelay = true
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"tools": []any{}})
				assert.False(t, membershipTextHTTPRequest(c, info, meta), "channel overrides")
			}
		})
	}
}

func TestMembershipPilotFinalReviewUsesStoppedTransportOnly(t *testing.T) {
	t.Setenv("KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", "9")
	for _, tc := range []struct {
		name   string
		modify func(*relaycommon.RelayInfo, *gin.Context, *types.NewAPIError)
	}{
		{"eligible", nil},
		{"ordinary scope", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) {
			i.MembershipPilotTextHTTP = false
		}},
		{"wallet", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) {
			i.BillingSource = model.MembershipWalletSource
		}},
		{"outside cohort", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) { i.UserId = 10 }},
		{"different failure", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) {
			i.MembershipPilotStoppedError = types.NewError(errors.New("other attempt"), types.ErrorCodeDoRequestFailed)
		}},
		{"keepalive committed", func(_ *relaycommon.RelayInfo, c *gin.Context, _ *types.NewAPIError) { c.Writer.WriteHeaderNow() }},
		{"upstream bytes", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) { i.ReceivedResponseCount = 1 }},
		{"trusted zero usage", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) { i.BillingHasReportedUsage = true }},
		{"partial usage", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) {
			i.BillingFailureUsage = struct{}{}
		}},
		{"local fee", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) { i.MembershipViolationFee = 5 }},
		{"policy evidence", func(_ *relaycommon.RelayInfo, _ *gin.Context, e *types.NewAPIError) {
			e.SetPolicyEvidence("local policy decision")
		}},
		{"tool use", func(i *relaycommon.RelayInfo, _ *gin.Context, _ *types.NewAPIError) {
			i.SetBillableToolCount("web_search", 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			e := types.NewError(errors.New("connection closed"), types.ErrorCodeDoRequestFailed)
			i := &relaycommon.RelayInfo{MembershipPilotTextHTTP: true, MembershipPilotStoppedError: e, BillingSource: model.MembershipSource, UserId: 9}
			if tc.modify == nil {
				require.Equal(t, model.MembershipPilotEarlyReviewReason, membershipFinalReviewReason(c, i, e))
				return
			}
			tc.modify(i, c, e)
			assert.Equal(t, "upstream_outcome_unknown", membershipFinalReviewReason(c, i, e))
		})
	}
}
