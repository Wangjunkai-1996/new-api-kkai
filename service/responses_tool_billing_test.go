package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesToolSurchargeUsesActualCountsAndImageSpecifications(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	for _, tc := range []struct {
		name           string
		webTool        string
		webCalls       int
		fileCalls      int
		images         []relaycommon.ResponsesImageGenerationCall
		wantQuota      string
		wantWebPrice   float64
		wantImagePrice float64
	}{
		{name: "no_calls", wantQuota: "0"},
		{name: "standard_search", webTool: dto.BuildInToolWebSearch, webCalls: 2, fileCalls: 1, wantQuota: "14062.5", wantWebPrice: 10},
		{name: "preview_search", webTool: dto.BuildInToolWebSearchPreview, webCalls: 2, fileCalls: 1, wantQuota: "32812.5", wantWebPrice: 25},
		{name: "images_with_different_specs", images: []relaycommon.ResponsesImageGenerationCall{{Quality: "low", Size: "1024x1024"}, {Quality: "high", Size: "1536x1024"}}, wantQuota: "163125", wantImagePrice: 0.261},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newLogInfoTestContext()
			// Fresh observations must supersede an earlier attempt's image marker.
			ctx.Set("image_generation_call", true)
			ctx.Set("image_generation_call_quality", "high")
			ctx.Set("image_generation_call_size", "1024x1024")
			info := &relaycommon.RelayInfo{ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
				BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
					dto.BuildInToolWebSearchPreview: {ToolName: tc.webTool, CallCount: tc.webCalls},
					dto.BuildInToolFileSearch:       {CallCount: tc.fileCalls},
				},
				ImageGenerationCalls: append([]relaycommon.ResponsesImageGenerationCall{}, tc.images...),
			}}
			summary := textQuotaSummary{ModelName: "gpt-4o", GroupRatio: 1.25}
			quota := calculateTextToolCallSurcharge(ctx, info, &summary)
			require.Equal(t, tc.wantQuota, quota.String())
			assert.Equal(t, tc.wantWebPrice, summary.WebSearchPrice)
			assert.Equal(t, tc.wantImagePrice, summary.ImageGenerationCallPrice)
			assert.Equal(t, tc.webCalls, summary.WebSearchCallCount)
			assert.Equal(t, tc.fileCalls, summary.FileSearchCallCount)
		})
	}
}
