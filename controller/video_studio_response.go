package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
)

const videoStudioAssetStoreContextKey = "video_studio_asset_store"

func videoStudioSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}

func respondVideoStudioError(c *gin.Context, err error) {
	status, code := videoStudioErrorStatus(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		common.SysError("video studio request failed: " + message)
		message = "video studio request failed"
	}
	c.JSON(status, gin.H{"success": false, "message": message, "code": code})
}

func videoStudioErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrInvalidVideoStudioSubmission),
		errors.Is(err, service.ErrInvalidVideoModelSpec),
		errors.Is(err, service.ErrInvalidVideoParameters),
		errors.Is(err, service.ErrInvalidVideoSample),
		errors.Is(err, service.ErrInvalidVideoOutboxEvent),
		errors.Is(err, service.ErrInvalidVideoAssetUpload),
		errors.Is(err, service.ErrInvalidVideoGenerationFilter),
		errors.Is(err, service.ErrInvalidIdempotencyRequest):
		return http.StatusBadRequest, "invalid_video_studio_request"
	case errors.Is(err, service.ErrVideoAssetAccessDenied):
		return http.StatusForbidden, "video_asset_access_denied"
	case errors.Is(err, service.ErrVideoAssetUploadExpired):
		return http.StatusGone, "video_upload_expired"
	case errors.Is(err, service.ErrVideoModelProfileNotFound),
		errors.Is(err, service.ErrVideoSampleNotFound),
		errors.Is(err, service.ErrVideoGenerationNotFound),
		errors.Is(err, service.ErrVideoOutboxEventNotFound),
		errors.Is(err, service.ErrVideoAssetNotFound):
		return http.StatusNotFound, "video_studio_resource_not_found"
	case errors.Is(err, service.ErrVideoModelProfileInUse),
		errors.Is(err, service.ErrVideoModelNeedsSample),
		errors.Is(err, service.ErrVideoSampleNotPublishable),
		errors.Is(err, service.ErrVideoGenerationDeleted),
		errors.Is(err, service.ErrVideoAssetInUse),
		errors.Is(err, service.ErrVideoAssetUploadCompleted),
		errors.Is(err, service.ErrVideoOutboxRedriveConflict),
		errors.Is(err, service.ErrIdempotencyConflict),
		errors.Is(err, service.ErrIdempotencyInProgress),
		errors.Is(err, service.ErrIdempotencyBindingConflict):
		return http.StatusConflict, "video_studio_conflict"
	case errors.Is(err, video_studio_setting.ErrR2NotConfigured):
		return http.StatusServiceUnavailable, "video_storage_unavailable"
	case errors.Is(err, service.ErrVideoMultipartUnavailable):
		return http.StatusServiceUnavailable, "video_storage_unavailable"
	default:
		return http.StatusInternalServerError, "video_studio_internal_error"
	}
}

func videoStudioAssetStore(c *gin.Context) (service.VideoAssetStore, error) {
	if value, exists := c.Get(videoStudioAssetStoreContextKey); exists {
		if store, ok := value.(service.VideoAssetStore); ok && store != nil {
			return store, nil
		}
	}
	return service.NewR2VideoAssetStoreFromEnvironment(c.Request.Context())
}

func videoStudioID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, service.ErrInvalidVideoStudioSubmission
	}
	return id, nil
}
