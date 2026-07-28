package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func VideoStudioAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		if video_studio_setting.Get().AccessMode == video_studio_setting.AccessModeOff {
			denyVideoStudioAccess(c)
			return
		}
		user, err := model.GetUserById(c.GetInt("id"), false)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				denyVideoStudioAccess(c)
				return
			}
			common.SysError(fmt.Sprintf("video studio access user lookup failed: %v", err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "video studio access check failed",
				"code":    "video_studio_internal_error",
			})
			c.Abort()
			return
		}
		if user.Status != common.UserStatusEnabled || !video_studio_setting.CanAccess(user.Role) {
			denyVideoStudioAccess(c)
			return
		}

		c.Set("role", user.Role)
		c.Set("status", user.Status)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		user.ToBaseUser().WriteContext(c)
		c.Next()
	}
}

func denyVideoStudioAccess(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": "video studio is not available for this account",
		"code":    "video_studio_access_denied",
	})
	c.Abort()
}
