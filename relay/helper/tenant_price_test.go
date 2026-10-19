package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantPriceRetryKeepsReservedTariff(t *testing.T) {
	saved := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(saved)) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-retry-text":1}`))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "tenant-retry-text", TenantID: 42, TenantPriceVersionID: 1, TenantRetailMultiplier: 2}
	first, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.NoError(t, err)
	info.TenantQuoteID = 1
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-retry-text":100}`))
	second, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1.0, second.ModelRatio)
	assert.Equal(t, 2.0, second.GroupRatioInfo.GroupRatio)
}
