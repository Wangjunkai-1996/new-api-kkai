package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestResellerFinanceAndApplicationReviewRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerResellerRoutes(engine.Group("/api"), func(c *gin.Context) { c.Next() })

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	assert.True(t, routes[http.MethodPatch+" /api/admin/resellers/applications/:id"])
	assert.False(t, routes[http.MethodPatch+" /api/admin/resellers/:id/application"])
	assert.True(t, routes[http.MethodGet+" /api/admin/resellers/withdrawals"])
	assert.True(t, routes[http.MethodPatch+" /api/admin/resellers/withdrawals/:id"])
	assert.True(t, routes[http.MethodPost+" /api/reseller/withdrawals"])
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPatch, "/api/admin/resellers/applications/1"},
		{http.MethodPatch, "/api/admin/resellers/withdrawals/1"},
		{http.MethodPost, "/api/admin/resellers/1/settlements/reverse"},
		{http.MethodPost, "/api/admin/resellers/1/payments/2/reverse"},
		{http.MethodPost, "/api/admin/resellers/1/consumptions/request-1/reconcile"},
		{http.MethodPost, "/api/reseller/withdrawals"},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, endpoint.path)
	}
}
