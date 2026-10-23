package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestInvoiceFinancialRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerInvoiceRoutes(engine.Group("/api"))
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/invoices/policy"}, {"GET", "/api/invoices"}, {"POST", "/api/invoices"},
		{"POST", "/api/invoices/quote"}, {"GET", "/api/invoices/summary"}, {"GET", "/api/invoices/eligible-orders"},
		{"GET", "/api/invoices/notifications"}, {"POST", "/api/invoices/notifications/read-all"}, {"POST", "/api/invoices/notifications/event/read"},
		{"GET", "/api/invoices/private"}, {"GET", "/api/invoices/private/files/file"}, {"POST", "/api/invoices/private/attachments"},
		{"POST", "/api/invoices/private/confirm"}, {"POST", "/api/invoices/private/disputes"}, {"POST", "/api/invoices/private/disputes/dispute/messages"},
		{"GET", "/api/invoice-profiles"}, {"POST", "/api/invoice-profiles"}, {"GET", "/api/invoice-profiles/profile"}, {"PATCH", "/api/invoice-profiles/profile"}, {"DELETE", "/api/invoice-profiles/profile"},
		{"GET", "/api/admin/invoices"}, {"GET", "/api/admin/invoices/summary"}, {"GET", "/api/admin/invoices/private"},
		{"GET", "/api/admin/invoices/notifications"}, {"POST", "/api/admin/invoices/notifications/read-all"}, {"POST", "/api/admin/invoices/notifications/event/read"},
		{"GET", "/api/admin/invoices/private/files/file"}, {"POST", "/api/admin/invoices/private/attachments"},
		{"POST", "/api/admin/invoices/private/approve"}, {"POST", "/api/admin/invoices/private/reject"}, {"POST", "/api/admin/invoices/private/hold"}, {"POST", "/api/admin/invoices/private/resume"},
		{"POST", "/api/admin/invoices/private/deliveries"}, {"POST", "/api/admin/invoices/private/disputes/dispute/reply"}, {"POST", "/api/admin/invoices/private/disputes/dispute/reissue"},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code, endpoint.method+" "+endpoint.path)
	}
}
