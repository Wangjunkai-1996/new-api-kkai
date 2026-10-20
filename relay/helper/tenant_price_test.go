package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantPriceRetryKeepsReservedTariff(t *testing.T) {
	saved := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(saved)) })
	for _, test := range []struct {
		name  string
		ratio float64
	}{{"paid", 2}, {"free", 0}} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-retry-text":1}`))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{OriginModelName: "tenant-retry-text", TenantID: 42, TenantSupplySnapshot: "frozen platform pricing", TenantRetailMultiplier: test.ratio}
			first, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
			require.NoError(t, err)
			assert.Equal(t, test.ratio, first.GroupRatioInfo.GroupRatio, "inherited prices have no saved version but still use the frozen tenant ratio")
			if test.ratio > 0 {
				info.TenantQuoteID = 1
			} else {
				info.TenantBudget = &hosttypes.MembershipPricing{Price: first}
				info.Billing = &priceTestReservation{}
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-retry-text":100}`))
			second, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{})
			require.NoError(t, err)
			assert.Equal(t, first, second)
			assert.Equal(t, 1.0, second.ModelRatio)
			assert.Equal(t, test.ratio, second.GroupRatioInfo.GroupRatio)
		})
	}
}
