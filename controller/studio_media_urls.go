package controller

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/gin-gonic/gin"
)

// SignStudioMediaURLs exchanges authenticated asset paths for short-lived object
// URLs. Browser media elements cannot attach the dashboard's bearer credential.
func SignStudioMediaURLs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request struct {
		Paths []string `json:"paths"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Paths) == 0 || len(request.Paths) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_paths", "message": "Provide between 1 and 100 studio media paths."})
		return
	}
	// Read current identity from the database just as the Studio route guards do.
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil || user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_UNAUTHORIZED"})
		return
	}
	isAdmin := user.Role >= common.RoleAdminUser
	urls := make(map[string]string, len(request.Paths))
	expiresAt := time.Now().Unix() + int64(min(image_studio_setting.Get().SignedURLSeconds, video_studio_setting.Get().SignedURLSeconds))
	for _, path := range request.Paths {
		if _, exists := urls[path]; exists {
			continue
		}
		parsed, err := url.Parse(path)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawPath != "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
			return
		}
		segments := strings.Split(parsed.Path, "/")
		query, queryErr := url.ParseQuery(parsed.RawQuery)
		validQuery := queryErr == nil && (len(query) == 0 || (len(query) == 1 && len(query["variant"]) == 1))
		if len(segments) != 6 || segments[0] != "" || segments[1] != "api" || segments[3] != "assets" ||
			(segments[5] != "content" && segments[5] != "download") || !validQuery {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
			return
		}
		id, err := strconv.ParseInt(segments[4], 10, 64)
		variant := query.Get("variant")
		attachment := segments[5] == "download"
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != segments[4] || (attachment && variant != "") {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
			return
		}
		switch segments[2] {
		case "image-studio":
			if variant != "" && variant != "thumbnail" {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
				return
			}
			if !image_studio_setting.CanAccess(user.Role) {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "image_studio_access_denied"})
				return
			}
			store, err := imageStudioAssetStore(c)
			if err != nil {
				respondImageStudioError(c, err)
				return
			}
			location, err := service.SignAuthorizedImageAsset(c.Request.Context(), model.DB, store, user.Id, isAdmin, id, variant == "thumbnail", attachment)
			if err != nil {
				respondImageStudioError(c, err)
				return
			}
			urls[path] = location
		case "video-studio":
			if variant != "" && variant != "poster" && variant != "preview" {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
				return
			}
			if !video_studio_setting.CanAccess(user.Role) {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "video_studio_access_denied"})
				return
			}
			store, err := videoStudioAssetStore(c)
			if err != nil {
				respondVideoStudioError(c, err)
				return
			}
			location, err := service.SignAuthorizedVideoAsset(c.Request.Context(), model.DB, store, user.Id, isAdmin, id, variant, attachment)
			if err != nil {
				respondVideoStudioError(c, err)
				return
			}
			urls[path] = location
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_studio_media_path"})
			return
		}
	}
	common.ApiSuccess(c, gin.H{"urls": urls, "expires_at": expiresAt})
}
