package service

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestWalletTrustThresholdPreservesInt64Balance(t *testing.T) {
	setting := operation_setting.GetQuotaSetting()
	oldUSD, oldUnit := setting.TrustQuotaUSD, common.QuotaPerUnit
	t.Cleanup(func() { setting.TrustQuotaUSD, common.QuotaPerUnit = oldUSD, oldUnit })
	common.QuotaPerUnit = 1
	for _, tc := range []struct {
		name       string
		threshold  float64
		balance    int64
		force      bool
		unlimited  bool
		tokenQuota int
		want       bool
	}{
		{name: "zero disables trust", threshold: 0, balance: 100, unlimited: true},
		{name: "exact threshold still reserves", threshold: 10, balance: 10, unlimited: true},
		{name: "above threshold trusts", threshold: 10, balance: 11, unlimited: true, want: true},
		{name: "token threshold still reserves", threshold: 10, balance: 100, tokenQuota: 10},
		{name: "sufficient token trusts", threshold: 10, balance: 100, tokenQuota: 11, want: true},
		{name: "forced reservation", threshold: 10, balance: 100, unlimited: true, force: true},
		{name: "invalid runtime configuration", threshold: math.Inf(1), balance: math.MaxInt64, unlimited: true},
		{name: "int64 balance is not rounded to float", threshold: 1 << 53, balance: 1<<53 + 1, unlimited: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.TrustQuotaUSD = tc.threshold
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("token_quota", tc.tokenQuota)
			session := BillingSession{
				relayInfo: &relaycommon.RelayInfo{UserQuota: tc.balance, TokenUnlimited: tc.unlimited, ForcePreConsume: tc.force},
				funding:   &WalletFunding{},
			}
			assert.Equal(t, tc.want, session.shouldTrust(ctx))
		})
	}
}
