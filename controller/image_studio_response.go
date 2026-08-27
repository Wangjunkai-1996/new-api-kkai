package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
)

const (
	imageStudioAssetPipelineContextKey = "image_studio_asset_pipeline"
	imageStudioAssetStoreContextKey    = "image_studio_asset_store"
)

func imageStudioSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}

func respondImageStudioError(c *gin.Context, err error) {
	status, code := imageStudioErrorStatus(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		common.SysError("image studio request failed: " + message)
		message = "image studio request failed"
	}
	c.JSON(status, gin.H{"success": false, "message": message, "code": code})
}

func imageStudioErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrInvalidImageStudioSubmission),
		errors.Is(err, service.ErrInvalidImageModelSpec),
		errors.Is(err, service.ErrInvalidImageParameters),
		errors.Is(err, service.ErrInvalidImageSample),
		errors.Is(err, service.ErrInvalidImageOutboxEvent),
		errors.Is(err, service.ErrInvalidIdempotencyRequest),
		errors.Is(err, service.ErrInvalidImageGenerationFilter):
		return http.StatusBadRequest, "invalid_image_studio_request"
	case errors.Is(err, imagepricing.ErrUnsupportedSize):
		return http.StatusBadRequest, "invalid_image_size"
	case errors.Is(err, service.ErrImageStudioTokenRequired):
		return http.StatusBadRequest, "image_token_required"
	case errors.Is(err, service.ErrImageStudioTokenInvalid):
		return http.StatusForbidden, "image_token_invalid"
	case errors.Is(err, service.ErrImageStudioTokenGroupInvalid):
		return http.StatusForbidden, "image_token_group_invalid"
	case errors.Is(err, service.ErrImageStudioTokenModelForbidden):
		return http.StatusForbidden, "image_token_model_forbidden"
	case errors.Is(err, service.ErrImageStudioTokenIPForbidden):
		return http.StatusForbidden, "image_token_ip_forbidden"
	case errors.Is(err, service.ErrImageStudioTokenGroupUnavailable):
		return http.StatusForbidden, "image_token_group_unavailable"
	case errors.Is(err, service.ErrImageModelProfileNotFound),
		errors.Is(err, service.ErrImageSampleNotFound),
		errors.Is(err, service.ErrImageGenerationNotFound),
		errors.Is(err, service.ErrImageAssetNotFound):
		return http.StatusNotFound, "image_studio_resource_not_found"
	case errors.Is(err, service.ErrImageOutboxEventNotFound):
		return http.StatusNotFound, "image_studio_resource_not_found"
	case errors.Is(err, service.ErrImageModelProfileInUse),
		errors.Is(err, service.ErrImageModelProfileDuplicate),
		errors.Is(err, service.ErrImageModelProfileModelImmutable),
		errors.Is(err, service.ErrImageModelProfileConflict),
		errors.Is(err, service.ErrImageModelAbilityUnavailable),
		errors.Is(err, service.ErrImageModelBillingUnsupported),
		errors.Is(err, service.ErrImageGenerationConflict),
		errors.Is(err, service.ErrImageSampleConflict),
		errors.Is(err, service.ErrImageSampleNotPublishable),
		errors.Is(err, service.ErrImageSampleImmutable),
		errors.Is(err, service.ErrImageOutboxRedriveConflict),
		errors.Is(err, service.ErrIdempotencyConflict),
		errors.Is(err, service.ErrIdempotencyInProgress),
		errors.Is(err, service.ErrIdempotencyBindingConflict):
		return http.StatusConflict, "image_studio_conflict"
	case errors.Is(err, service.ErrImageAssetAccessDenied):
		return http.StatusForbidden, "image_asset_access_denied"
	case errors.Is(err, service.ErrImageStudioQuoteMismatch),
		errors.Is(err, service.ErrImageStudioQuoteExpired),
		errors.Is(err, service.ErrImageStudioQuoteStale):
		return http.StatusConflict, "quote_stale"
	case errors.Is(err, service.ErrImageStudioTokenLimitReached):
		return http.StatusConflict, "image_token_limit_reached"
	case errors.Is(err, service.ErrImageStudioTokenModelsUnavailable):
		return http.StatusServiceUnavailable, "image_token_models_unavailable"
	case errors.Is(err, service.ErrNoChannelSupportsImageOutputCount):
		return http.StatusServiceUnavailable, "image_output_count_unavailable"
	case errors.Is(err, service.ErrImageStudioCapacityExceeded):
		return http.StatusTooManyRequests, "image_studio_busy"
	case errors.Is(err, service.ErrImageArchiveTooLarge):
		return http.StatusRequestEntityTooLarge, "image_asset_too_large"
	case errors.Is(err, service.ErrImageArchiveSourceRejected),
		errors.Is(err, service.ErrImageArchiveResponseRejected),
		errors.Is(err, service.ErrImageArchiveMIMERejected),
		errors.Is(err, service.ErrImageArchivePixelsExceeded):
		return http.StatusBadRequest, "invalid_image_asset"
	case errors.Is(err, service.ErrImageTemporaryStorageUnavailable):
		return http.StatusServiceUnavailable, "image_temporary_storage_unavailable"
	case errors.Is(err, video_studio_setting.ErrR2NotConfigured):
		return http.StatusServiceUnavailable, "image_storage_unavailable"
	default:
		return http.StatusInternalServerError, "image_studio_internal_error"
	}
}

func imageStudioAssetStore(c *gin.Context) (service.ImageAssetStore, error) {
	if value, exists := c.Get(imageStudioAssetStoreContextKey); exists {
		if store, ok := value.(service.ImageAssetStore); ok && store != nil {
			return store, nil
		}
	}
	return service.ImageStudioR2AssetStore(c.Request.Context())
}

func imageStudioAssetPipeline(c *gin.Context) (*service.ImageAssetPipeline, error) {
	if value, exists := c.Get(imageStudioAssetPipelineContextKey); exists {
		if pipeline, ok := value.(*service.ImageAssetPipeline); ok && pipeline != nil {
			return pipeline, nil
		}
	}
	return service.NewRuntimeImageAssetPipeline(c.Request.Context(), model.DB)
}

func imageStudioID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, service.ErrInvalidImageStudioSubmission
	}
	return id, nil
}
