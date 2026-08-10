package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func abortLiveSessionValidation(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn)})
	} else {
		common.SysLog("session user validation failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgDatabaseError)})
	}
	c.Abort()
}

func loadLiveSessionUser(session sessions.Session) (*model.User, error) {
	if session == nil || model.DB == nil {
		return nil, errors.New("session user store is unavailable")
	}
	userID, ok := session.Get("id").(int)
	if !ok || userID <= 0 {
		return nil, errors.New("session user id is invalid")
	}
	return model.GetUserById(userID, false)
}

// SessionAuth authenticates browser session-only endpoints without requiring
// the New-Api-User header used by dashboard API calls and OAuth redirects.
func SessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("id") == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn)})
			c.Abort()
			return
		}
		user, err := loadLiveSessionUser(session)
		if err != nil {
			abortLiveSessionValidation(c, err)
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthUserBanned)})
			c.Abort()
			return
		}
		if user.Role < common.RoleCommonUser || !validUserInfo(user.Username, user.Role) {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid)})
			c.Abort()
			return
		}
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("id", user.Id)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		c.Set("use_access_token", false)
		c.Next()
	}
}
