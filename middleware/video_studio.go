package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func VideoStudioAccess() gin.HandlerFunc {
	return studioAccess(
		func() bool { return video_studio_setting.Get().AccessMode != video_studio_setting.AccessModeOff },
		video_studio_setting.CanAccess,
		"video studio is not available for this account",
		"video_studio_access_denied",
		"video_studio_internal_error",
	)
}

func ImageStudioAccess() gin.HandlerFunc {
	return studioAccess(
		func() bool { return image_studio_setting.Get().AccessMode != image_studio_setting.AccessModeOff },
		image_studio_setting.CanAccess,
		"image studio is not available for this account",
		"image_studio_access_denied",
		"image_studio_internal_error",
	)
}

func studioAccess(
	enabled func() bool,
	canAccess func(int) bool,
	deniedMessage string,
	deniedCode string,
	internalCode string,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled() {
			denyStudioAccess(c, deniedMessage, deniedCode)
			return
		}
		user, err := model.GetUserById(c.GetInt("id"), false)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				denyStudioAccess(c, deniedMessage, deniedCode)
				return
			}
			common.SysError(fmt.Sprintf("studio access user lookup failed: %v", err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "studio access check failed",
				"code":    internalCode,
			})
			c.Abort()
			return
		}
		if user.Status != common.UserStatusEnabled || !canAccess(user.Role) {
			denyStudioAccess(c, deniedMessage, deniedCode)
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

func denyStudioAccess(c *gin.Context, message string, code string) {
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}
