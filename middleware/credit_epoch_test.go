package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreditEpochGuardProtectsConsoleMoneyWithoutBlockingCallbacks(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMap[common.CreditEpochOption] = `{"version":1,"migration_id":"cutover-1","source_epoch":"legacy_075","target_epoch":"usd_credit_v1","numerator":3,"denominator":40,"plan_hash":"wallet-plan","pricing_plan_hash":"price-plan","legacy_max_ids":{},"applied_at":1,"legacy_topup_policy":"hold"}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		common.OptionMap = oldMap
	})
	for _, tt := range []struct {
		method, path, epoch string
		status              int
	}{
		{"POST", "/api/user/pay", "", 409},
		{"POST", "/api/user/pay", "legacy_075", 409},
		{"POST", "/api/user/pay", "usd_credit_v1:another-cutover", 409},
		{"POST", "/api/user/pay", "usd_credit_v1:cutover-1", 204},
		{"PUT", "/api/option/", "", 409},
		{"PUT", "/api/user/", "", 409},
		{"POST", "/api/token/", "", 409},
		{"POST", "/api/redemption/", "", 409},
		{"POST", "/api/subscription/balance/pay", "", 409},
		{"GET", "/api/user/topup", "", 204},
		{"POST", "/api/user/epay/notify", "", 204},
		{"POST", "/api/stripe/webhook", "", 204},
		{"POST", "/api/subscription/epay/notify", "", 204},
		{"POST", "/api/internal/balance-adjustments", "", 204},
		{"POST", "/api/user/login", "", 204},
		{"POST", "/v1/chat/completions", "", 204},
	} {
		t.Run(tt.method+tt.path+tt.epoch, func(t *testing.T) {
			r := gin.New()
			r.Use(CreditEpochGuard())
			r.Handle(tt.method, tt.path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.Header.Set("X-KKAI-Credit-Epoch", tt.epoch)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			assert.Equal(t, tt.status, response.Code)
		})
	}
	for _, raw := range []string{"", "malformed"} {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[common.CreditEpochOption] = raw
		common.OptionMapRWMutex.Unlock()
		r := gin.New()
		r.Use(CreditEpochGuard())
		r.POST("/api/user/pay", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("POST", "/api/user/pay", nil))
		if raw == "" {
			require.Equal(t, http.StatusNoContent, response.Code)
		} else {
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
		}
	}
}
