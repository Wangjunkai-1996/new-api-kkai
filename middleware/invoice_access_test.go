package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestInvoiceRoutesRejectScopedAndLegacyTokensAndTenantHosts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/invoices/policy"}, {"GET", "/api/invoices"}, {"POST", "/api/invoices"}, {"POST", "/api/invoices/quote"},
		{"GET", "/api/invoices/:public_id/files/:file_id"}, {"POST", "/api/invoices/:public_id/attachments"}, {"POST", "/api/invoices/:public_id/confirm"},
		{"POST", "/api/invoices/:public_id/disputes"}, {"POST", "/api/invoices/:public_id/disputes/:dispute_id/messages"},
		{"POST", "/api/invoices/notifications/:event_id/read"}, {"POST", "/api/invoices/notifications/read-all"},
		{"GET", "/api/invoice-profiles"}, {"GET", "/api/invoice-profiles/:id"}, {"PATCH", "/api/invoice-profiles/:id"}, {"DELETE", "/api/invoice-profiles/:id"},
		{"POST", "/api/admin/invoices/:public_id/approve"}, {"POST", "/api/admin/invoices/:public_id/reject"}, {"POST", "/api/admin/invoices/:public_id/deliveries"},
		{"GET", "/api/admin/invoices/:public_id/files/:file_id"}, {"POST", "/api/admin/invoices/:public_id/disputes/:dispute_id/reply"},
		{"POST", "/api/admin/invoices/:public_id/disputes/:dispute_id/reissue"},
	} {
		for _, legacy := range []bool{false, true} {
			engine := gin.New()
			engine.Handle(endpoint.method, endpoint.path, func(c *gin.Context) {
				lookup := &accessTokenLookup{legacy: legacy, token: &model.UserAccessToken{Scopes: `["billing:write","billing:read","profile:read"]`}}
				if enforceAccessTokenRoute(c, lookup, true) {
					c.Status(http.StatusNoContent)
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
			assert.Equal(t, http.StatusForbidden, response.Code, endpoint.method+" "+endpoint.path)
			assert.Contains(t, response.Body.String(), "AUTH_SESSION_REQUIRED")
		}
		assert.False(t, tenantRouteEnabled(endpoint.method, endpoint.path, endpoint.path), "tenant invoicing must remain disabled")
	}
	for _, path := range []string{"/invoices", "/invoices/new", "/invoice-admin", "/invoice-admin/any"} {
		assert.False(t, tenantRouteEnabled(http.MethodGet, "", path))
	}
}
