package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
)

func VideoStudioAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		if video_studio_setting.CanAccess(c.GetInt("role")) {
			c.Next()
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "video studio is not available for this account",
			"code":    "video_studio_access_denied",
		})
		c.Abort()
	}
}
