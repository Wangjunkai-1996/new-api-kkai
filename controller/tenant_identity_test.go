package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantRegistrationCreatesAtomicCustomerIdentity(t *testing.T) {
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousRegister, previousPasswordRegister, previousVerify := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled
	previousToken, previousGift := constant.GenerateDefaultToken, common.QuotaForNewUser
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.TenantMembership{}, &model.TenantCatalogEntry{}))
	model.DB, common.RedisEnabled = db, false
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, true
	constant.GenerateDefaultToken, common.QuotaForNewUser = true, 500
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = previousRegister, previousPasswordRegister, previousVerify
		constant.GenerateDefaultToken, common.QuotaForNewUser = previousToken, previousGift
	})

	for _, tenantID := range []int64{41, 42} {
		require.NoError(t, db.Create(&model.TenantCatalogEntry{TenantID: tenantID, Model: "gpt-test", SalesGroup: "default", Status: model.TenantCatalogStatusEnabled, IsDefault: true}).Error)
		body := []byte(`{"username":"same-user","email":"same@example.com","password":"Password12345","verification_code":"123456"}`)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "https://tenant.example.com/api/user/register", bytes.NewReader(body))
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID, Hostname: "tenant.example.com"})
		common.RegisterVerificationCodeWithKey(tenantEmailVerificationKey(c, "same@example.com"), "123456", common.EmailVerificationPurpose)
		Register(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"success":true`)
		var user model.User
		require.NoError(t, db.Where("tenant_id = ? AND username = ?", tenantID, "same-user").First(&user).Error)
		assert.Zero(t, user.Quota)
		assert.Zero(t, user.InviterId)
		membership, err := (model.TenantScope{TenantID: tenantID}).Membership(db, user.Id, model.TenantMembershipRoleCustomer)
		require.NoError(t, err)
		assert.Equal(t, tenantID, membership.TenantID)
		var token model.Token
		require.NoError(t, db.Where("user_id = ?", user.Id).First(&token).Error)
		assert.Equal(t, tenantID, token.TenantID)
		assert.Equal(t, "default", token.Group)
		assert.False(t, token.CrossGroupRetry)
	}

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail-default-token", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(fmt.Errorf("injected token insert failure"))
		}
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewBufferString(`{"username":"atomic-user","email":"atomic@example.com","password":"Password12345","verification_code":"123456"}`))
	common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 41, Hostname: "tenant.example.com"})
	common.RegisterVerificationCodeWithKey(tenantEmailVerificationKey(c, "atomic@example.com"), "123456", common.EmailVerificationPurpose)
	Register(c)
	assert.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "atomic-user").Count(&count).Error)
	assert.Zero(t, count)
}

func TestTenantHostDoesNotExposePlatformOwnerBusinessProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := &model.User{Id: 7, TenantID: model.PlatformTenantID, Username: "owner", Role: common.RoleRootUser, Group: "vip", Quota: 5000, UsedQuota: 1000, RequestCount: 3, AffCode: "platform-code", AffQuota: 400, InviterId: 2, StripeCustomer: "cus_123", Setting: `{"webhook_secret":"secret"}`, GitHubId: "github-123"}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42, Hostname: "tenant.example.com"})
	data := buildSelfUserData(owner, c)
	assert.True(t, data["tenant_owner"].(bool))
	for _, key := range []string{"quota", "used_quota", "request_count", "group", "aff_code", "aff_quota", "inviter_id", "stripe_customer", "setting", "github_id"} {
		assert.NotContains(t, data, key)
	}
	assert.Equal(t, common.RoleCommonUser, data["role"])
}

func TestTenantPasswordResetCodeCannotCrossHost(t *testing.T) {
	source, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(source, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42, Hostname: "first.example.com"})
	codeKey := tenantEmailVerificationKey(source, "same@example.com")
	common.RegisterVerificationCodeWithKey(codeKey, "one-tenant-secret", common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(codeKey, common.PasswordResetPurpose) })
	for _, target := range []int64{0, 43} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", bytes.NewBufferString(`{"email":"same@example.com","token":"one-tenant-secret"}`))
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: target, Hostname: "other.example.com"})
		ResetPassword(c)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
}
