package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopSensitiveResponseAuditForwardsWithoutCopyingSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var capture *auditResponseWriter
	router.POST("/secret", SensitiveResponseAudit(), func(c *gin.Context) {
		capture = beginAdminAudit(c)
		c.String(http.StatusOK, "sensitive-account-password")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/secret", nil))
	require.NotNil(t, capture)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "sensitive-account-password", response.Body.String())
	assert.Zero(t, capture.body.Len(), "audit must never buffer secret-returning responses")
	assert.True(t, auditResponseSuccess(response.Code, capture.body.Bytes()))
}

func TestShopGuardsRejectMissingTrustedTenantAndSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, guard := range []gin.HandlerFunc{ShopMainSiteGuard(), ShopSessionGuard(false), ShopSessionGuard(true)} {
		router := gin.New()
		called := false
		router.GET("/shop", guard, func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/shop", nil))
		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.False(t, called)
	}
}
