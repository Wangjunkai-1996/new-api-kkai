package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShopRoutesRejectUntrustedSiteBeforeDatabaseOrAuthentication(t *testing.T) {
	for _, tenant := range []int64{0, 42} {
		engine := gin.New()
		if tenant != 0 {
			engine.Use(func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenant, Hostname: "tenant.invalid"})
			})
		}
		registerShopRoutes(engine.Group("/api"))
		for _, route := range []struct{ method, path string }{{"GET", "/api/shop/products"}, {"GET", "/api/shop/covers/example.jpg"}, {"POST", "/api/admin/shop/covers"}, {"POST", "/api/shop/orders"}, {"POST", "/api/shop/orders/so_test/payment-attempts"}, {"PUT", "/api/shop/orders/so_test/buyer-input"}, {"POST", "/api/shop/payments/epay/notify"}, {"POST", "/api/admin/shop/evidence"}, {"POST", "/api/admin/shop/receipts/spr_test/refunds"}, {"POST", "/api/admin/shop/orders/so_test/deliveries"}} {
			r := httptest.NewRecorder()
			engine.ServeHTTP(r, httptest.NewRequest(route.method, route.path, nil))
			assert.Equal(t, http.StatusForbidden, r.Code, route.path)
			assert.Contains(t, r.Body.String(), "SHOP_MAIN_SITE_ONLY", route.path)
			assert.Equal(t, "no-store", r.Header().Get("Cache-Control"))
		}
	}
}
