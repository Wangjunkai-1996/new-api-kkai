package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestWalletPaymentRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerWalletPaymentRoutes(engine.Group("/api"))
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/user/topup/current"},
		{"POST", "/api/user/topup/current/restore"},
		{"POST", "/api/user/topup/quote"},
		{"POST", "/api/user/topup/orders"},
		{"GET", "/api/user/topup/orders/private"},
		{"POST", "/api/user/topup/orders/private/resume"},
		{"POST", "/api/user/topup/orders/private/cancel"},
		{"POST", "/api/user/topup/orders/private/end"},
		{"POST", "/api/user/topup/orders/private/reconcile"},
		{"POST", "/api/user/topup/current/acknowledge"},
		{"GET", "/api/user/topup/orders/private/admin"},
		{"POST", "/api/user/topup/orders/private/admin-reconcile"},
		{"POST", "/api/user/topup/reconcile-runs/report"},
		{"POST", "/api/user/topup/reconcile-runs"},
		{"GET", "/api/user/topup/reconcile-runs/private"},
		{"POST", "/api/user/topup/reconcile-runs/private/pause"},
		{"POST", "/api/user/topup/reconcile-runs/private/resume"},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code, endpoint.method+" "+endpoint.path)
	}
}
