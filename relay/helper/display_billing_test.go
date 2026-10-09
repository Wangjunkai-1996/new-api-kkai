package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperFreezesDisplayExpression(t *testing.T) {
	const actual = `tier("base", p * 10 + cr * 2)`
	const display = `tier("base", p * 10 + cr * 0)`
	cfg := config.GlobalConfig.Get("billing_setting")
	previous, err := config.ConfigToMap(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(cfg, previous)) })
	actualMap, err := common.Marshal(map[string]string{"display-model": actual})
	require.NoError(t, err)
	displayMap, err := common.Marshal(map[string]string{"display-model": display})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{
		"billing_expr": string(actualMap), "display_billing_expr": string(displayMap),
	}))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{}
	_, err = modelPriceHelperTiered(ctx, info, "display-model", 1000, hosttypes.GroupRatioInfo{GroupRatio: 1})
	require.NoError(t, err)
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.Equal(t, actual, info.TieredBillingSnapshot.ExprString)
	assert.Equal(t, display, info.TieredBillingSnapshot.DisplayExprString)
	assert.Equal(t, billingexpr.ExprHashString(actual), info.TieredBillingSnapshot.ExprHash)

	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{"display_billing_expr": "{}"}))
	assert.Equal(t, display, info.TieredBillingSnapshot.DisplayExprString)
	result, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot, billingexpr.TokenParams{CR: 1000})
	require.NoError(t, err)
	assert.Equal(t, common.QuotaRound(2000/1_000_000.0*common.QuotaPerUnit), result.ActualQuotaAfterGroup)
}
