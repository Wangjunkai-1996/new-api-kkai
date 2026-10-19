package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantSessionRefreshCannotCrossTenant(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TenantMembership{}))
	require.NoError(t, model.DB.Create(&model.TenantMembership{UserID: user.Id, TenantID: 42, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	bundle, err := CreateLoginSession(user.Id, "password", "127.0.0.1", "tenant-test", 42)
	require.NoError(t, err)
	identity, err := ParseAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	session, _, err := ValidateLoginSession(identity)
	require.NoError(t, err)
	assert.Equal(t, int64(42), session.TenantID)
	_, _, err = RefreshLoginSession(bundle.RefreshToken, session.SID, "127.0.0.1", "tenant-test", 43)
	assert.ErrorIs(t, err, ErrLoginSessionMismatch)
	_, _, err = RefreshLoginSession(bundle.RefreshToken, session.SID, "127.0.0.1", "tenant-test", 42)
	require.NoError(t, err)

	require.NoError(t, model.DB.Model(&model.TenantMembership{}).Where("tenant_id = ? AND user_id = ?", 42, user.Id).Update("status", model.TenantMembershipStatusDisabled).Error)
	_, _, err = ValidateLoginSession(identity)
	assert.ErrorIs(t, err, ErrLoginSessionRevoked)
}

func TestTenantRefreshCookiesRemainSecureWhenPlatformUsesHTTP(t *testing.T) {
	oldSecure := common.SessionCookieSecure
	common.SessionCookieSecure = false
	t.Cleanup(func() { common.SessionCookieSecure = oldSecure })
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	common.SetContextKey(context, constant.ContextKeyTenantID, int64(42))
	WriteRefreshCookie(context, "invalid-test-token")
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 2)
	for _, cookie := range cookies {
		assert.True(t, cookie.Secure)
		assert.Empty(t, cookie.Domain)
		assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
		if cookie.Name == RefreshCookieName {
			assert.True(t, cookie.HttpOnly)
		}
	}
}
