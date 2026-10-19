package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantDashboardOwnerCannotUseCustomerOrAdminRoutes(t *testing.T) {
	db := newResellerMiddlewareTestDB(t)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	require.NoError(t, db.Create(&model.TenantMembership{UserID: 7, TenantID: 42, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	owner := &model.UserBase{Id: 7, TenantID: model.PlatformTenantID, Role: common.RoleRootUser}
	for _, test := range []struct {
		path    string
		minRole int
		allowed bool
	}{
		{path: "/api/reseller/profile", minRole: common.RoleCommonUser, allowed: true},
		{path: "/api/pricing", minRole: common.RoleCommonUser, allowed: true},
		{path: "/api/models", minRole: common.RoleCommonUser, allowed: true},
		{path: "/api/token/", minRole: common.RoleCommonUser},
		{path: "/api/user/self", minRole: common.RoleCommonUser, allowed: true},
		{path: "/api/option/", minRole: common.RoleRootUser},
	} {
		t.Run(test.path, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "https://tenant.example.com"+test.path, nil)
			common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42, Hostname: "tenant.example.com"})
			err := authorizeDashboardTenant(c, owner, test.minRole)
			if test.allowed {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, service.ErrLoginSessionMismatch)
			}
			if test.allowed {
				setDashboardAuthContext(c, owner, service.AuthIdentity{UserID: owner.Id, SessionID: "sid", UserAuthVersion: 1, SessionVersion: 1}, false)
				assert.Equal(t, common.RoleCommonUser, c.GetInt("role"))
			}
		})
	}
}
