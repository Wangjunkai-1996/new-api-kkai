package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestHeaderNavModuleUserAuthRejectsStaleEnabledSession(t *testing.T) {
	withHeaderNavModules(t, `{"pricing":{"enabled":false,"requireAuth":false}}`)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), true, common.UserStatusDisabled)

	require.NotContains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestTokenOrUserAuthRejectsStaleEnabledSession(t *testing.T) {
	recorder := performHeaderNavRequest(t, TokenOrUserAuth(), true, common.UserStatusDisabled)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestHeaderNavModuleUserAuthFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	withHeaderNavModules(t, `{"pricing":{"enabled":false,"requireAuth":false}}`)

	recorder := performHeaderNavRequestWithDB(t, HeaderNavModulePublicOrUserAuth("pricing"), true, nil)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestTokenOrUserAuthFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	recorder := performHeaderNavRequestWithDB(t, TokenOrUserAuth(), true, nil)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestTryUserAuthReloadsLiveSessionStatus(t *testing.T) {
	recorder := performHeaderNavRequest(t, TryUserAuth(), true, common.UserStatusDisabled)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestTryUserAuthFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	recorder := performHeaderNavRequestWithDB(t, TryUserAuth(), true, nil)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestSessionAuthRejectsAnonymousSession(t *testing.T) {
	recorder := performHeaderNavRequest(t, SessionAuth(), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestSessionAuthRejectsDisabledLiveUser(t *testing.T) {
	recorder := performHeaderNavRequest(t, SessionAuth(), true, common.UserStatusDisabled)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestSessionAuthFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	recorder := performHeaderNavRequestWithDB(t, SessionAuth(), true, nil)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestSessionAuthRefreshesLiveGroupWithoutUserHeader(t *testing.T) {
	recorder := performHeaderNavRequest(t, SessionAuth(), true)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "live-group")
}

func TestTryUserAuthAllowsAnonymousSession(t *testing.T) {
	recorder := performHeaderNavRequest(t, TryUserAuth(), false)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}
