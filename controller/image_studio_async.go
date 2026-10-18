package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func QuoteAsyncImageStudioGeneration(c *gin.Context) {
	var request service.ImageAsyncSubmissionRequest
	if !readAsyncImageRequest(c, &request) {
		return
	}
	if !service.ImageAsyncAdmissionEnabled(c.Request.Context(), model.DB) {
		respondImageStudioUploadError(c, service.ErrImageUploadUnavailable)
		return
	}
	normalized, err := service.NormalizeAsyncImageSubmission(c.Request.Context(), model.DB, c.GetInt("id"), request)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	if err := prepareAsyncImageContext(c, normalized); err != nil {
		respondImageStudioError(c, err)
		return
	}
	defer common.CleanupBodyStorage(c)
	if err := replaceImageStudioRelayBody(c, normalized.RelayRequest); err != nil {
		respondImageStudioError(c, err)
		return
	}
	if err := selectAsyncImageChannel(c, normalized.Model); err != nil {
		respondImageStudioError(c, err)
		return
	}
	price, err := calculateImageStudioQuote(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	if price.ImagePricingSnapshot == nil {
		respondImageStudioError(c, service.ErrImageModelBillingUnsupported)
		return
	}
	quote, err := service.NewImageStudioQuote(normalized, price.Quota, price.OtherRatios, price.ImagePricingSnapshot, price.MembershipPricing)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, quote)
}

func SubmitAsyncImageStudioGeneration(c *gin.Context) {
	started := time.Now()
	var request service.ImageAsyncSubmissionRequest
	if !readAsyncImageRequest(c, &request) {
		return
	}
	userID := c.GetInt("id")
	hash, err := request.IdentityHash()
	if err != nil {
		respondImageStudioError(c, service.ErrInvalidImageStudioSubmission)
		return
	}
	generation, found, err := service.ReplayAsyncImageSubmission(c.Request.Context(), model.DB, userID, c.GetHeader("Idempotency-Key"), hash)
	if err != nil {
		respondImageStudioSubmitGuardError(c, err)
		return
	}
	if !found {
		if !service.ImageAsyncAdmissionEnabled(c.Request.Context(), model.DB) {
			respondImageStudioUploadError(c, service.ErrImageUploadUnavailable)
			return
		}
		normalized, normalizeErr := service.NormalizeAsyncImageSubmission(c.Request.Context(), model.DB, userID, request)
		if normalizeErr != nil {
			respondImageStudioUploadError(c, normalizeErr)
			return
		}
		requestID := common.GetContextKeyString(c, common.RequestIdKey)
		if requestID == "" {
			requestID = common.NewRequestId()
		}
		generation, err = service.AcceptAsyncImageSubmission(c.Request.Context(), model.DB, request, normalized, c.GetHeader("Idempotency-Key"), requestID, c.ClientIP())
		if err != nil {
			if errors.Is(err, service.ErrImageUploadConflict) || errors.Is(err, service.ErrImageUploadUnavailable) {
				respondImageStudioUploadError(c, err)
				return
			}
			respondImageStudioSubmitGuardError(c, err)
			return
		}
	}
	view, err := service.GetImageGenerationView(c.Request.Context(), model.DB, userID, generation.ID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Retry-After", "2")
	c.Header("Location", fmt.Sprintf("/api/image-studio/generations/%d", generation.ID))
	imageStudioSuccess(c, http.StatusAccepted, view)
	logger.LogInfo(c, fmt.Sprintf("event=image_async_accepted generation_id=%d request_id=%q elapsed_ms=%d input_count=%d replay=%t", generation.ID, generation.RequestID, time.Since(started).Milliseconds(), len(request.AssetIDs), found))
}

func readAsyncImageRequest(c *gin.Context, request *service.ImageAsyncSubmissionRequest) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ContentType() != "application/json" || common.DecodeJson(c.Request.Body, request) != nil {
		respondImageStudioError(c, service.ErrInvalidImageStudioSubmission)
		return false
	}
	return true
}

// This context is constructed from the authenticated user and a validated Studio
// token. Never copy authorization headers or channel credentials into job payloads.
func prepareAsyncImageContext(c *gin.Context, normalized *service.NormalizedImageStudioSubmission) error {
	if _, err := applyImageStudioTokenContext(c, normalized.TokenID, normalized.Model); err != nil {
		return err
	}
	var user model.User
	if err := model.DB.WithContext(c.Request.Context()).First(&user, normalized.UserID).Error; err != nil {
		return err
	}
	c.Set("username", user.Username)
	common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, int64(user.Quota))
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
	common.SetContextKey(c, constant.ContextKeyOriginalModel, normalized.Model)
	common.SetContextKey(c, constant.ContextKeyIsPlayground, true)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	service.SetImageStudioReferenceCount(c, len(normalized.References))
	c.Request.URL.Path = "/v1/images/generations"
	if normalized.Mode == service.ImageStudioModeEdit {
		c.Request.URL.Path = "/v1/images/edits"
	}
	c.Request.URL.RawPath = ""
	return nil
}

func selectAsyncImageChannel(c *gin.Context, modelName string) error {
	service.GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: c.Request.URL.Path})
	service.AppendTaskPluginIdentityFilter(c, "")
	channel, _, selectErr := service.SelectChannelForRequest(c, modelName, &service.RetryParam{Ctx: c, TokenGroup: common.GetContextKeyString(c, constant.ContextKeyUsingGroup), ModelName: modelName, RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0)})
	if selectErr != nil || channel == nil {
		return errors.New("image channel is unavailable")
	}
	if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return err
	}
	return nil
}
