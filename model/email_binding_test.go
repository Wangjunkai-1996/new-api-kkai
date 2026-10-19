package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantEmailBindingPreservesScopedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name              string
		tenantID          int64
		otherTenantID     int64
		claimBeforeFinish bool
		wantConflict      bool
	}{
		{name: "customer can reuse platform email", tenantID: 42, otherTenantID: 0},
		{name: "customer can reuse another tenant email", tenantID: 42, otherTenantID: 43},
		{name: "platform can reuse customer email", tenantID: 0, otherTenantID: 42},
		{name: "customer rejects own tenant email", tenantID: 42, otherTenantID: 42, wantConflict: true},
		{name: "platform rejects own email", tenantID: 0, otherTenantID: 0, wantConflict: true},
		{name: "completion rechecks tenant ownership", tenantID: 42, otherTenantID: 42, claimBeforeFinish: true, wantConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenantIdentityTestDB(t)
			require.NoError(t, DB.AutoMigrate(&UserSession{}, &AuthFlow{}))
			username, oldEmail, newEmail := "binder", "old@example.com", "new@example.com"
			user := User{
				TenantID: tc.tenantID, Username: username, NormalizedUsername: &username,
				Email: oldEmail, NormalizedEmail: &oldEmail, AffCode: "binder",
				Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1,
			}
			require.NoError(t, DB.Create(&user).Error)
			session := UserSession{
				SID: "email-binding-session", TenantID: tc.tenantID, UserID: user.Id,
				Version: 1, UserAuthVersion: 1, Status: UserSessionStatusActive,
				RefreshHash: "test-refresh-hash", ExpiresAt: time.Now().Add(time.Hour).Unix(),
			}
			require.NoError(t, DB.Create(&session).Error)
			identity := AuthSessionIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1}
			codeHash, err := common.Password2Hash("123456")
			require.NoError(t, err)
			state := EmailBindingState{
				Authorization: &AuthFlowAuthorization{
					AuthSessionIdentity: identity, ProofID: 1, Scope: "account.binding.bind", ContextHash: "email-binding-context", Method: "2fa",
				},
				CurrentEmail: oldEmail, Email: newEmail, NewCodeHash: codeHash,
			}
			otherUsername := "other"
			other := User{
				TenantID: tc.otherTenantID, Username: otherUsername, NormalizedUsername: &otherUsername,
				Email: newEmail, NormalizedEmail: &newEmail, AffCode: "other",
			}
			if !tc.claimBeforeFinish {
				require.NoError(t, DB.Create(&other).Error)
			}
			token, flow, err := CreateEmailBinding(identity, state)
			if tc.wantConflict && !tc.claimBeforeFinish {
				require.ErrorIs(t, err, ErrEmailAlreadyTaken)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.tenantID, flow.TenantID)
			if tc.claimBeforeFinish {
				require.NoError(t, DB.Create(&other).Error)
			}
			_, err = CompleteEmailBinding(identity, token, newEmail, "123456", "")
			var updated User
			if tc.wantConflict {
				require.ErrorIs(t, err, ErrEmailAlreadyTaken)
				require.NoError(t, DB.First(&updated, user.Id).Error)
				assert.Equal(t, oldEmail, updated.Email)
				require.NotNil(t, updated.NormalizedEmail)
				assert.Equal(t, oldEmail, *updated.NormalizedEmail)
				require.NoError(t, DB.First(flow, flow.Id).Error)
				assert.Nil(t, flow.ConsumedAt)
				return
			}
			require.NoError(t, err)
			require.NoError(t, DB.First(&updated, user.Id).Error)
			assert.Equal(t, newEmail, updated.Email)
			require.NotNil(t, updated.NormalizedEmail)
			assert.Equal(t, newEmail, *updated.NormalizedEmail)
			login, err := GetUniqueUserByEmailForTenant(newEmail, tc.tenantID)
			require.NoError(t, err)
			assert.Equal(t, user.Id, login.Id)
			require.NoError(t, DB.First(flow, flow.Id).Error)
			assert.NotNil(t, flow.ConsumedAt)
			_, err = CompleteEmailBinding(identity, token, newEmail, "123456", "")
			assert.ErrorIs(t, err, ErrAuthFlowConsumed)
			// Releasing the old address must release its canonical UNIQUE key too.
			releasedUsername := "released"
			released := User{TenantID: tc.tenantID, Username: releasedUsername, NormalizedUsername: &releasedUsername, Email: oldEmail, NormalizedEmail: &oldEmail, AffCode: "released"}
			assert.NoError(t, DB.Create(&released).Error)
		})
	}
}
