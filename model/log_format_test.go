package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestLogProjectionProtectsTenantPricingAndStreamDiagnostics(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"expr_b64": "display-expression",
		"admin_info": map[string]any{
			"actual_expr_b64": "actual-expression",
			"stream_status": map[string]any{
				"status": "error", "end_reason": "scanner_error", "response_status": "failed", "error_count": 1,
				"end_error": "secret endpoint", "errors": []string{"private upstream response"}, "unexpected": "secret",
			},
		},
		"root_info": map[string]any{"secret": "root-only"},
	})
	for _, tenantID := range []int64{PlatformTenantID, 1} {
		t.Run(fmt.Sprintf("tenant_%d", tenantID), func(t *testing.T) {
			logs := []*Log{{TenantID: tenantID, ChannelId: 12, ChannelName: "internal", Other: other}}
			formatUserLogs(logs, 0)
			parsed, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			assert.NotContains(t, parsed, "admin_info")
			assert.NotContains(t, parsed, "root_info")
			assert.Equal(t, map[string]any{"status": "error", "end_reason": "scanner_error", "response_status": "failed", "error_count": float64(1)}, parsed["stream_status"])
			assert.Empty(t, logs[0].ChannelName)
			if tenantID > PlatformTenantID {
				assert.Equal(t, "actual-expression", parsed["expr_b64"])
				assert.Zero(t, logs[0].ChannelId)
			} else {
				assert.Equal(t, "display-expression", parsed["expr_b64"])
				assert.Equal(t, 12, logs[0].ChannelId)
			}
		})
	}
	logs := []*Log{{Other: other}}
	FormatAdminLogs(logs)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Contains(t, parsed, "admin_info")
	assert.NotContains(t, parsed, "root_info")
	assert.Equal(t, "display-expression", parsed["expr_b64"])
}

func TestLegacyStreamProjectionPreservesOperatorDiagnostics(t *testing.T) {
	other := `{"stream_status":{"status":"error","end_reason":"private endpoint","response_status":"private response","error_count":-1,"end_error":"secret","errors":["private"]}}`
	for _, visibility := range []logOtherVisibility{logOtherVisibilityUser, logOtherVisibilityAdmin, logOtherVisibilityRoot} {
		formatted := formatLogOtherJSON(other, visibility, false)
		parsed, err := common.StrToMap(formatted)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"status": "error"}, parsed["stream_status"])
		if visibility == logOtherVisibilityUser {
			assert.NotContains(t, parsed, "admin_info")
			assert.NotContains(t, formatted, "secret")
		} else {
			assert.Contains(t, parsed, "admin_info")
			assert.Contains(t, formatted, "secret")
		}
	}
	assert.JSONEq(t, `{}`, formatLogOtherJSON(`{"stream_status":{"status":"private value","errors":["secret"]}}`, logOtherVisibilityUser, false))
}
