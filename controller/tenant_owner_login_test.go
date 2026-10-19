package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantOwnerLoginUsesExplicitNamespaceAndPreservesTwoFA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TenantMembership{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.AuthFlow{}, &model.UserSession{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousLogin, previousEncryption := common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled
	model.DB, common.RedisEnabled = db, false
	common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = true, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = previousLogin, previousEncryption
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	password := "SharedNamespacePassword123"
	hash, err := common.HashAccountPassword(password)
	require.NoError(t, err)
	owner := model.User{Username: "same-name", Email: "same@example.com", TenantID: 0, Password: hash, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "owner"}
	customer := model.User{Username: owner.Username, Email: owner.Email, TenantID: 42, Password: hash, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "customer"}
	customer.NormalizedUsername, customer.NormalizedEmail = &customer.Username, &customer.Email
	outsider := model.User{Username: "outsider", TenantID: 0, Password: hash, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "outsider"}
	for _, user := range []*model.User{&owner, &customer, &outsider} {
		user.Role = common.RoleCommonUser
		require.NoError(t, db.Create(user).Error)
		require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, IsEnabled: true, Secret: "enrolled-factor"}).Error)
	}
	require.NoError(t, db.Create(&model.TenantMembership{TenantID: 42, UserID: owner.Id, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantMembership{TenantID: 42, UserID: customer.Id, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive}).Error)
	for _, test := range []struct {
		name       string
		tenantID   int64
		identity   string
		ownerFlag  any
		wantUserID int
	}{
		{"default_customer_username", 42, owner.Username, nil, customer.Id},
		{"default_customer_email", 42, owner.Email, nil, customer.Id},
		{"explicit_customer", 42, owner.Username, false, customer.Id},
		{"owner_username", 42, owner.Username, true, owner.Id},
		{"owner_email", 42, owner.Email, true, owner.Id},
		{"owner_requires_membership", 42, outsider.Username, true, 0},
		{"customer_never_falls_back", 43, owner.Username, nil, 0},
		{"other_tenant_owner_rejected", 43, owner.Username, true, 0},
		{"platform_rejects_owner_mode", 0, owner.Username, true, 0},
		{"owner_flag_must_be_boolean", 42, owner.Username, "true", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := map[string]any{"username": test.identity, "password": password, "tenant_id": 0}
			if test.ownerFlag != nil {
				body["owner_login"] = test.ownerFlag
			}
			encoded, err := common.Marshal(body)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "https://tenant.example.com/api/user/login", bytes.NewReader(encoded))
			ctx.Set("login_method", "password")
			common.SetContextKey(ctx, constant.ContextKeyTenantContext, model.TenantContext{TenantID: test.tenantID, Hostname: "tenant.example.com"})
			Login(ctx)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					RequireVerification bool   `json:"require_verification"`
					FlowToken           string `json:"flow_token"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			if test.wantUserID == 0 {
				assert.False(t, response.Success, recorder.Body.String())
				assert.Empty(t, response.Data.FlowToken)
				return
			}
			require.True(t, response.Success, recorder.Body.String())
			require.True(t, response.Data.RequireVerification)
			flow, err := model.GetAuthFlow(response.Data.FlowToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeLoginVerification})
			require.NoError(t, err)
			assert.Equal(t, test.wantUserID, flow.UserId)
			assert.Equal(t, test.tenantID, flow.TenantID)
			_, err = service.RequireLoginVerification(response.Data.FlowToken, service.VerificationMethodTwoFA, test.tenantID)
			require.NoError(t, err)
			_, err = service.RequireLoginVerification(response.Data.FlowToken, service.VerificationMethodTwoFA, test.tenantID+1)
			assert.ErrorIs(t, err, model.ErrAuthFlowInvalid)
			assert.Empty(t, recorder.Header().Values("Set-Cookie"))
		})
	}
	var sessions int64
	require.NoError(t, db.Model(&model.UserSession{}).Count(&sessions).Error)
	assert.Zero(t, sessions, "the new owner mode must not bypass the enrolled factor")
}
