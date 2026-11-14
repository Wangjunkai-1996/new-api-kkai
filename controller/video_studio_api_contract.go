package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

// Existing tabs do not send a version header. Only the new Studio requests API4.
func VideoStudioAPIContract(c *gin.Context) {
	c.Writer.Header().Add("Vary", "X-KKAI-Console-API")
	version := c.GetHeader("X-KKAI-Console-API")
	switch version {
	case "", "3":
		c.Request = c.Request.WithContext(service.WithVideoStudioAPI3(c.Request.Context()))
	case "4":
		path := strings.TrimRight(c.Request.URL.Path, "/")
		create := c.Request.Method == http.MethodPost && (path == "/pg/videos" || path == "/pg/videos/quote" || strings.HasSuffix(path, "/uploads"))
		// AdminAuth runs first; prepare model capabilities before opening creation.
		profile := path == "/api/admin/video-studio/model-profiles" || strings.HasPrefix(path, "/api/admin/video-studio/model-profiles/")
		configure := !profile && strings.HasPrefix(path, "/api/admin/video-studio/") && (c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut)
		if (create || configure) && !video_studio_setting.Get().MultimodalEnabled {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "video_multimodal_unavailable", "message": "multimodal video creation is not enabled"})
			return
		}
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "video_request_contract_invalid", "message": "unsupported Studio API contract"})
		return
	}
	c.Next()
}
