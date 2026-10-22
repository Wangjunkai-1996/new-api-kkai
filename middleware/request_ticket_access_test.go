package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequestTicketRoutesRejectScopedAndLegacyAccessTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/requests"},
		{http.MethodPost, "/api/requests"},
		{http.MethodGet, "/api/requests/:id"},
		{http.MethodGet, "/api/requests/notifications"},
		{http.MethodPost, "/api/requests/notifications/:id/read"},
		{http.MethodPost, "/api/requests/notifications/read-all"},
		{http.MethodGet, "/api/requests/admin"},
		{http.MethodGet, "/api/requests/admin/:id"},
		{http.MethodPatch, "/api/requests/admin/:id/status"},
	} {
		for _, legacy := range []bool{false, true} {
			engine := gin.New()
			engine.Handle(endpoint.method, endpoint.path, func(c *gin.Context) {
				lookup := &accessTokenLookup{legacy: legacy, token: &model.UserAccessToken{Scopes: `["profile:read","user:write"]`}}
				if enforceAccessTokenRoute(c, lookup, true) {
					c.Status(http.StatusNoContent)
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
			assert.Equal(t, http.StatusForbidden, response.Code, endpoint.method+" "+endpoint.path)
			assert.Contains(t, response.Body.String(), "AUTH_SESSION_REQUIRED")
		}
	}
}
