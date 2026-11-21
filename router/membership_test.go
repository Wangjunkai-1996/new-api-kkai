package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipAdminOrderRoutesRequireRootSession(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB })
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(append(model.MembershipModels(), &model.UserSession{}, &model.AuditLog{})...))
	engine := gin.New()
	registerMembershipRoutes(engine.Group("/api"), middleware.AnonymousRequestBodyLimit())
	order := model.MembershipOrder{UserID: 201, TradeNo: "membership-access", Status: model.MembershipOrderException, Terms: "{}", PaidAt: 1}
	require.NoError(t, model.DB.Create(&order).Error)

	for _, actor := range []struct {
		name     string
		id, role int
	}{
		{"member", 201, common.RoleCommonUser},
		{"admin", 202, common.RoleAdminUser},
		{"root", 203, common.RoleRootUser},
	} {
		t.Run(actor.name, func(t *testing.T) {
			user := model.User{Id: actor.id, Username: actor.name, AffCode: actor.name, Role: actor.role, Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, model.DB.Create(&user).Error)
			session, err := service.CreateLoginSession(user.Id, "test", "127.0.0.1", "membership-access")
			require.NoError(t, err)
			for _, route := range []struct{ method, path, body string }{
				{http.MethodGet, "/api/membership/admin/orders?status=paid_exception", ""},
				{http.MethodPost, "/api/membership/admin/orders/membership-access/resolve", `{"action":"dismiss","reason":"Verified receipt","operation_id":"membership-access-resolution"}`},
			} {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				req.Header.Set("Authorization", "Bearer "+session.AccessToken)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, req)
				if actor.role == common.RoleRootUser {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					assert.Contains(t, w.Body.String(), order.TradeNo)
				} else {
					require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
					assert.Contains(t, w.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")
				}
			}
			var stored model.MembershipOrder
			require.NoError(t, model.DB.First(&stored, order.ID).Error)
			assert.Equal(t, actor.role == common.RoleRootUser, stored.ResolvedAt > 0)
		})
	}
}
