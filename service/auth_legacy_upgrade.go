package service

import (
	"crypto/hmac"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gsessions "github.com/gorilla/sessions"
	"gorm.io/gorm"
)

const (
	LegacySessionCookieName = "session"
	legacySessionMaxAge     = 30 * 24 * 60 * 60
)

var (
	ErrLegacySessionMissing = errors.New("legacy login session is missing")
	ErrLegacySessionInvalid = errors.New("legacy login session is invalid")
)

// UpgradeLegacyLoginSession converts a valid legacy signed cookie into the
// stateless-authentication bundle used by the current dashboard.
func UpgradeLegacyLoginSession(request *http.Request, ip, userAgent string) (*AuthBundle, *model.User, error) {
	session, userBase, rawRefreshToken, err := resolveLegacyLoginSession(request, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}
	if rawRefreshToken == "" {
		return nil, nil, ErrRefreshTokenInvalid
	}
	bundle, err := issueAuthBundle(session, rawRefreshToken, true)
	if err != nil {
		return nil, nil, err
	}
	user, err := model.GetUserById(userBase.Id, false)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion != session.UserAuthVersion {
		return nil, nil, ErrLoginSessionRevoked
	}
	return bundle, user, nil
}

// AuthenticateLegacyDashboardRequest keeps an already-open legacy dashboard
// tab usable during the bounded v8 transition window.
func AuthenticateLegacyDashboardRequest(request *http.Request, ip, userAgent string) (*model.UserBase, AuthIdentity, string, error) {
	session, user, rawRefreshToken, err := resolveLegacyLoginSession(request, ip, userAgent)
	if err != nil {
		return nil, AuthIdentity{}, "", err
	}
	identity := AuthIdentity{
		UserID:          session.UserID,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	return user, identity, rawRefreshToken, nil
}

func RevokeLegacyLoginSession(request *http.Request, ip, userAgent string) error {
	session, _, _, err := resolveLegacyLoginSession(request, ip, userAgent)
	if errors.Is(err, ErrLegacySessionMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = model.RevokeUserSession(session.UserID, session.SID, "legacy_logout")
	return err
}

func ClearLegacySessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     LegacySessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func resolveLegacyLoginSession(request *http.Request, ip, userAgent string) (*model.UserSession, *model.UserBase, string, error) {
	userID, assertion, err := decodeLegacySessionCookie(request)
	if err != nil {
		return nil, nil, "", err
	}
	digest := common.GenerateHMACWithKey([]byte("legacy-session-upgrade-v1:"+common.SessionSecret), assertion)
	sid := uuid.NewSHA1(uuid.NameSpaceOID, []byte(digest)).String()
	refreshSecret := common.GenerateHMACWithKey([]byte("legacy-session-refresh-v1:"+common.SessionSecret), assertion)
	now := time.Now().Unix()

	fullUser, err := model.GetUserById(userID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, "", ErrLegacySessionInvalid
		}
		return nil, nil, "", err
	}
	user := fullUser.ToBaseUser()
	if user.Status != common.UserStatusEnabled || user.AuthVersion != model.LegacyUserSessionAuthVersion {
		return nil, nil, "", ErrLoginSessionRevoked
	}

	candidate := &model.UserSession{
		SID:             sid,
		UserID:          user.Id,
		Version:         1,
		UserAuthVersion: model.LegacyUserSessionAuthVersion,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     hashRefreshSecret(refreshSecret),
		LoginMethod:     "legacy_cookie_upgrade",
		IP:              truncateAuthMetadata(ip, 64),
		UserAgent:       truncateAuthMetadata(userAgent, 512),
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       time.Unix(now, 0).Add(LoginSessionTTL).Unix(),
	}
	resolved, err := model.CreateOrGetLegacyUserSession(candidate, assertion)
	if err != nil {
		if errors.Is(err, model.ErrUserSessionInactive) {
			return nil, nil, "", ErrLoginSessionRevoked
		}
		return nil, nil, "", err
	}
	rawRefreshToken := ""
	if hmac.Equal([]byte(resolved.RefreshHash), []byte(candidate.RefreshHash)) {
		rawRefreshToken = sid + "." + refreshSecret
	}
	return resolved, user, rawRefreshToken, nil
}

func decodeLegacySessionCookie(request *http.Request) (int, string, error) {
	if request == nil {
		return 0, "", ErrLegacySessionMissing
	}
	cookie, err := request.Cookie(LegacySessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return 0, "", ErrLegacySessionMissing
	}
	if err != nil || cookie.Value == "" {
		return 0, "", ErrLegacySessionInvalid
	}
	store := gsessions.NewCookieStore([]byte(common.SessionSecret))
	store.Options = &gsessions.Options{
		Path:     "/",
		MaxAge:   legacySessionMaxAge,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	store.MaxAge(legacySessionMaxAge)
	legacySession, err := store.New(request, LegacySessionCookieName)
	if err != nil || legacySession.IsNew {
		return 0, "", ErrLegacySessionInvalid
	}
	userID, ok := legacySession.Values["id"].(int)
	if !ok || userID <= 0 || int64(userID) > math.MaxInt32 {
		return 0, "", ErrLegacySessionInvalid
	}
	return userID, cookie.Value, nil
}
