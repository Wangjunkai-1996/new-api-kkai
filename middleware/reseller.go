package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const resellerMembershipContextKey = "reseller_membership"

// TenantAdminAuth authorizes the authenticated dashboard user as an owner of
// the Host-selected tenant. Routes should install UserAuth before this
// middleware. It never accepts a tenant ID from request data.
func TenantAdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		context, ok := GetTenantContext(c)
		if !ok || context.TenantID <= model.PlatformTenantID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"code":    "TENANT_OWNER_REQUIRED",
				"message": "a reseller tenant is required",
			})
			return
		}
		userID := c.GetInt("id")
		if userID <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"code":    "AUTH_UNAUTHORIZED",
				"message": "authentication required",
			})
			return
		}
		if model.DB == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"code":    "TENANT_STORE_UNAVAILABLE",
				"message": "tenant store is unavailable",
			})
			return
		}
		membership, err := (model.TenantScope{TenantID: context.TenantID}).Membership(model.DB, userID, model.TenantMembershipRoleOwner)
		if err != nil {
			status := http.StatusForbidden
			code := "TENANT_OWNER_REQUIRED"
			if !errors.Is(err, model.ErrTenantMembershipMissing) {
				status = http.StatusInternalServerError
				code = "TENANT_MEMBERSHIP_ERROR"
			}
			c.AbortWithStatusJSON(status, gin.H{"success": false, "code": code, "message": "reseller owner access required"})
			return
		}
		c.Set(resellerMembershipContextKey, membership)
		c.Next()
	}
}

func GetResellerMembership(c *gin.Context) (*model.TenantMembership, bool) {
	value, ok := c.Get(resellerMembershipContextKey)
	if !ok {
		return nil, false
	}
	membership, ok := value.(*model.TenantMembership)
	return membership, ok && membership != nil
}
