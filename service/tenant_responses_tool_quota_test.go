package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantResponsesImageToolsUseFrozenQuotaConversion(t *testing.T) {
	originalUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalUnit })
	common.QuotaPerUnit = 999999
	for _, detailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy call marker", true: "actual image calls"}[detailed], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("image_generation_call", true)
			c.Set("image_generation_call_quality", "low")
			c.Set("image_generation_call_size", "1024x1024")
			info := &relaycommon.RelayInfo{TenantID: 42, OriginModelName: "tenant-responses", TenantBudget: &types.MembershipPricing{QuotaPerUnit: 10000}}
			summary := textQuotaSummary{ModelName: "tenant-responses", GroupRatio: 2}
			want := operation_setting.GPTImage1Low1024x1024 * 10000 * 2
			if detailed {
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{ImageGenerationCalls: []relaycommon.ResponsesImageGenerationCall{{Quality: "low", Size: "1024x1024"}, {Quality: "high", Size: "1536x1024"}}}
				want += operation_setting.GPTImage1High1536x1024 * 10000 * 2
			}
			quota := calculateTextToolCallSurcharge(c, info, &summary)
			assert.InDelta(t, want, quota.InexactFloat64(), 1e-9)
			require.NotEmpty(t, summary.ToolSurchargeItems)
			assert.Equal(t, dto.BuildInToolImageGeneration, summary.ToolSurchargeItems[0].Name)
			common.QuotaPerUnit *= 2
			assert.True(t, quota.Equal(calculateTextToolCallSurcharge(c, info, &summary)))
		})
	}
}
