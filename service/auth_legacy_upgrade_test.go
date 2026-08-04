package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	gsessions "github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func legacySessionCookie(t *testing.T, secret string, values map[interface{}]interface{}) *http.Cookie {
	t.Helper()
	store := gsessions.NewCookieStore([]byte(secret))
	store.Options = &gsessions.Options{
		Path:     "/",
		MaxAge:   legacySessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	store.MaxAge(legacySessionMaxAge)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	session, err := store.New(request, LegacySessionCookieName)
	require.NoError(t, err)
	for key, value := range values {
		session.Values[key] = value
	}
	require.NoError(t, store.Save(request, recorder, session))
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func requestWithLegacyCookie(cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
	request.AddCookie(cookie)
	return request
}

func TestLegacySessionUpgradeUsesDatabaseIdentityAndCreatesOneSession(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	cookie := legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{
		"id":       user.Id,
		"username": "forged-root",
		"role":     common.RoleRootUser,
		"status":   common.UserStatusEnabled,
		"group":    "forged-group",
	})

	bundle, upgradedUser, err := UpgradeLegacyLoginSession(
		requestWithLegacyCookie(cookie),
		"127.0.0.1",
		"legacy-browser",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, bundle.AccessToken)
	assert.Equal(t, user.Id, upgradedUser.Id)
	assert.Equal(t, common.RoleCommonUser, upgradedUser.Role)
	assert.Equal(t, "default", upgradedUser.Group)
	assert.Equal(t, "legacy_cookie_upgrade", bundle.Session.LoginMethod)

	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
	assert.EqualValues(t, 1, sessionCount)
	var flow model.AuthFlow
	require.NoError(t, model.DB.Where("purpose = ?", model.AuthFlowPurposeLegacySession).Take(&flow).Error)
	assert.Equal(t, user.Id, flow.UserId)
	assert.Equal(t, bundle.Session.SID, flow.SessionId)
	assert.NotContains(t, flow.TokenHash, cookie.Value)
}

func TestLegacySessionUpgradeRejectsCookieAfterAuthVersionAdvanceWithoutCreatingState(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	cookie := legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id})
	require.NoError(t, model.DB.Model(&model.User{}).
		Where("id = ?", user.Id).
		Update("auth_version", model.LegacyUserSessionAuthVersion+1).Error)

	_, _, err := UpgradeLegacyLoginSession(
		requestWithLegacyCookie(cookie),
		"127.0.0.1",
		"legacy-browser",
	)
	assert.ErrorIs(t, err, ErrLoginSessionRevoked)

	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
	assert.Zero(t, sessionCount)
	var flowCount int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).
		Where("purpose = ?", model.AuthFlowPurposeLegacySession).
		Count(&flowCount).Error)
	assert.Zero(t, flowCount)
}

func TestLegacySessionUpgradeRejectsTamperingWrongKeyAndDisabledUser(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	valid := legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id})
	tampered := *valid
	tampered.Value = valid.Value[:len(valid.Value)-1] + "x"
	wrongKey := legacySessionCookie(t, "different-session-secret-with-enough-entropy", map[interface{}]interface{}{"id": user.Id})

	for name, cookie := range map[string]*http.Cookie{
		"tampered":  &tampered,
		"wrong key": wrongKey,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := UpgradeLegacyLoginSession(requestWithLegacyCookie(cookie), "127.0.0.1", "legacy-browser")
			assert.ErrorIs(t, err, ErrLegacySessionInvalid)
		})
	}

	require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusDisabled).Error)
	_, _, err := UpgradeLegacyLoginSession(requestWithLegacyCookie(valid), "127.0.0.1", "legacy-browser")
	assert.ErrorIs(t, err, ErrLoginSessionRevoked)
}

func TestConcurrentLegacySessionUpgradeReusesOneSession(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	cookie := legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id})

	const callers = 8
	type result struct {
		sid string
		err error
	}
	results := make(chan result, callers)
	var waitGroup sync.WaitGroup
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			bundle, _, err := UpgradeLegacyLoginSession(
				requestWithLegacyCookie(cookie),
				"127.0.0.1",
				"legacy-browser",
			)
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{sid: bundle.Session.SID}
		}()
	}
	waitGroup.Wait()
	close(results)

	var expectedSID string
	for result := range results {
		require.NoError(t, result.err)
		if expectedSID == "" {
			expectedSID = result.sid
		}
		assert.Equal(t, expectedSID, result.sid)
	}
	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
	assert.EqualValues(t, 1, sessionCount)
	var flowCount int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).
		Where("purpose = ?", model.AuthFlowPurposeLegacySession).
		Count(&flowCount).Error)
	assert.EqualValues(t, 1, flowCount)
}

func TestConcurrentDistinctLegacySessionUpgradesEnforceSessionLimits(t *testing.T) {
	tests := []struct {
		name            string
		activeLimit     int
		issuanceLimit   int
		seedStatus      string
		seedExpiresAt   func(int64) int64
		wantRejectedErr error
	}{
		{
			name:            "active limit",
			activeLimit:     2,
			issuanceLimit:   100,
			seedStatus:      model.UserSessionStatusActive,
			seedExpiresAt:   func(now int64) int64 { return now + 3600 },
			wantRejectedErr: model.ErrUserSessionLimit,
		},
		{
			name:            "issuance limit",
			activeLimit:     100,
			issuanceLimit:   2,
			seedStatus:      model.UserSessionStatusRevoked,
			seedExpiresAt:   func(now int64) int64 { return now - 1 },
			wantRejectedErr: model.ErrUserSessionIssuanceLimit,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useTestSessionSecret(t)
			user := setupAuthSessionTestDB(t)
			common.UserSessionActiveLimit = test.activeLimit
			common.UserSessionIssuanceLimit = test.issuanceLimit
			now := time.Now().Unix()
			seed := model.UserSession{
				SID:             "existing-session",
				UserID:          user.Id,
				Version:         1,
				UserAuthVersion: model.LegacyUserSessionAuthVersion,
				Status:          test.seedStatus,
				RefreshHash:     "existing-refresh-hash",
				LoginMethod:     "password",
				CreatedAt:       now,
				LastActiveAt:    now,
				ExpiresAt:       test.seedExpiresAt(now),
			}
			if test.seedStatus == model.UserSessionStatusRevoked {
				seed.RevokedAt = now
				seed.RevokedReason = "test"
			}
			require.NoError(t, model.DB.Create(&seed).Error)

			cookies := []*http.Cookie{
				legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id, "nonce": "first"}),
				legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id, "nonce": "second"}),
			}
			start := make(chan struct{})
			results := make(chan error, len(cookies))
			var waitGroup sync.WaitGroup
			for _, cookie := range cookies {
				waitGroup.Add(1)
				go func(cookie *http.Cookie) {
					defer waitGroup.Done()
					<-start
					_, _, err := UpgradeLegacyLoginSession(
						requestWithLegacyCookie(cookie),
						"127.0.0.1",
						"legacy-browser",
					)
					results <- err
				}(cookie)
			}
			close(start)
			waitGroup.Wait()
			close(results)

			succeeded, rejected := 0, 0
			for err := range results {
				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, test.wantRejectedErr):
					rejected++
				default:
					require.NoError(t, err)
				}
			}
			assert.Equal(t, 1, succeeded)
			assert.Equal(t, 1, rejected)

			var sessionCount int64
			require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
			assert.EqualValues(t, 2, sessionCount)
			var flowCount int64
			require.NoError(t, model.DB.Model(&model.AuthFlow{}).
				Where("purpose = ?", model.AuthFlowPurposeLegacySession).
				Count(&flowCount).Error)
			assert.EqualValues(t, 1, flowCount)
		})
	}
}

func TestLegacySessionCannotBeUpgradedAgainAfterLogout(t *testing.T) {
	useTestSessionSecret(t)
	user := setupAuthSessionTestDB(t)
	cookie := legacySessionCookie(t, common.SessionSecret, map[interface{}]interface{}{"id": user.Id})
	request := requestWithLegacyCookie(cookie)

	_, _, err := UpgradeLegacyLoginSession(request, "127.0.0.1", "legacy-browser")
	require.NoError(t, err)
	require.NoError(t, RevokeLegacyLoginSession(request, "127.0.0.1", "legacy-browser"))
	var revoked model.UserSession
	require.NoError(t, model.DB.Take(&revoked).Error)
	assert.Equal(t, model.UserSessionStatusRevoked, revoked.Status)
	require.NoError(t, model.DB.Delete(&model.UserSession{}, "sid = ?", revoked.SID).Error)

	_, _, err = UpgradeLegacyLoginSession(request, "127.0.0.1", "legacy-browser")
	assert.ErrorIs(t, err, ErrLoginSessionRevoked)
	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
	assert.Zero(t, sessionCount, "a retained legacy assertion must prevent session resurrection")
}

func TestMissingLegacySessionIsDistinctFromInvalidSession(t *testing.T) {
	useTestSessionSecret(t)
	setupAuthSessionTestDB(t)
	request := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
	_, _, err := UpgradeLegacyLoginSession(request, "127.0.0.1", "legacy-browser")
	assert.True(t, errors.Is(err, ErrLegacySessionMissing))
}
