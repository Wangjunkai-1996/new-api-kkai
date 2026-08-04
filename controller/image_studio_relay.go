package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/gin-gonic/gin"
)

const (
	imageStudioNormalizedSubmissionContextKey   = "image_studio_normalized_submission"
	imageStudioIdempotencyReservationContextKey = "image_studio_idempotency_reservation"
	imageStudioPreRelayHookContextKey           = "image_studio_pre_relay_hook"
)

type imageStudioPreRelayHook struct {
	run      func() error
	executed bool
	err      error
}

type imageStudioQuotePrice struct {
	Quota                int
	OtherRatios          map[string]float64
	ImagePricingSnapshot *imagepricing.Snapshot
}

func PrepareImageStudioRequest(c *gin.Context) {
	settings := image_studio_setting.Get()
	c.Request = c.Request.WithContext(service.WithImageStudioResponseLimit(
		c.Request.Context(), settings.MaxResponseBytes,
	))
	var request service.ImageStudioSubmissionRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageStudioSubmission)
		c.Abort()
		return
	}
	_, err := applyImageStudioTokenContext(c, request.TokenID, request.Model)
	if err != nil {
		respondImageStudioError(c, err)
		c.Abort()
		return
	}
	normalized, err := service.NormalizeImageStudioSubmission(
		c.Request.Context(), model.DB, c.GetInt("id"), request,
	)
	if err != nil {
		respondImageStudioError(c, err)
		c.Abort()
		return
	}
	if _, err = applyImageStudioTokenContext(c, normalized.TokenID, normalized.Model); err != nil {
		respondImageStudioError(c, err)
		c.Abort()
		return
	}
	if err := replaceImageStudioRelayBody(c, normalized.RelayRequest); err != nil {
		respondImageStudioError(c, err)
		c.Abort()
		return
	}
	c.Set(imageStudioNormalizedSubmissionContextKey, normalized)

	if isImageStudioSubmitRequest(c) {
		if err := service.ValidateImageStudioSubmitRequest(request); err != nil {
			respondImageStudioSubmitGuardError(c, err)
			c.Abort()
			return
		}
		reservation, err := reserveImageStudioIdempotency(c, normalized)
		if err != nil {
			respondImageStudioSubmitGuardError(c, err)
			c.Abort()
			return
		}
		if !reservation.Created {
			respondImageStudioIdempotentReplay(c, reservation.Record.ResourceID)
			c.Abort()
			return
		}
		c.Set(imageStudioIdempotencyReservationContextKey, reservation)
		defer func() {
			releaseContext, cancel := context.WithTimeout(
				context.WithoutCancel(c.Request.Context()), 10*time.Second,
			)
			defer cancel()
			if err := service.ReleaseUnboundIdempotencyReservation(
				releaseContext, model.DB, reservation.Record.ID,
			); err != nil {
				common.SysError("release image studio idempotency reservation: " + err.Error())
			}
		}()
	}
	// The Gin route has already been selected. From this point onward the relay
	// stack must see the canonical OpenAI image path so endpoint-aware channel
	// selection and Advanced Custom route matching use the real upstream contract.
	common.SetContextKey(c, constant.ContextKeyIsPlayground, true)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	c.Request.URL.Path = "/v1/images/generations"
	c.Request.URL.RawPath = ""
	c.Next()
}

func QuoteImageStudioGeneration(c *gin.Context) {
	normalized, ok := imageStudioNormalizedSubmission(c)
	if !ok {
		respondImageStudioError(c, service.ErrInvalidImageStudioSubmission)
		return
	}
	price, err := calculateImageStudioQuote(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	quote, err := service.NewImageStudioQuote(
		normalized, price.Quota, price.OtherRatios, price.ImagePricingSnapshot,
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, quote)
}

func SubmitImageStudioGeneration(c *gin.Context) {
	normalized, ok := imageStudioNormalizedSubmission(c)
	if !ok {
		respondImageStudioSubmitGuardError(c, service.ErrInvalidImageStudioSubmission)
		return
	}
	quote, err := service.ValidateImageStudioQuote(normalized, time.Now())
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	if quote.ImagePricingSnapshot != nil {
		if err := service.SetTrustedImagePricingSnapshot(c, *quote.ImagePricingSnapshot); err != nil {
			respondImageStudioError(c, err)
			return
		}
	}
	if err := service.SetImageStudioBillingGuard(c, quote.Quota); err != nil {
		respondImageStudioError(c, err)
		return
	}
	reservation, exists := imageStudioIdempotencyReservation(c)
	if !exists || !reservation.Created {
		respondImageStudioError(c, service.ErrInvalidIdempotencyRequest)
		return
	}
	pipeline, err := imageStudioAssetPipeline(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	settings := image_studio_setting.Get()
	userID := c.GetInt("id")
	if !imageStudioCapacity.acquire(
		userID, settings.MaxConcurrentSubmissions, settings.MaxConcurrentSubmissionsPerUser,
	) {
		respondImageStudioError(c, service.ErrImageStudioCapacityExceeded)
		return
	}
	defer imageStudioCapacity.release(userID)
	capture, err := newImageRelayCaptureWriter(c.Writer, service.ImageStudioTempDirectory(), settings.MaxResponseBytes)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	defer capture.Remove()

	var generation *model.KKAIImageGeneration
	var heartbeatCancel context.CancelFunc
	defer func() {
		if heartbeatCancel != nil {
			heartbeatCancel()
		}
	}()
	setImageStudioPreRelayHook(c, func() error {
		requestID := common.GetContextKeyString(c, common.RequestIdKey)
		if requestID == "" {
			requestID = common.NewRequestId()
			c.Set(common.RequestIdKey, requestID)
		}
		created, err := service.CreateImageGeneration(
			c.Request.Context(), model.DB, normalized, requestID, reservation.Record.ID,
		)
		generation = created
		if err != nil {
			return err
		}
		if err := service.SetImageStudioGenerationID(c, created.ID); err != nil {
			return err
		}
		heartbeatContext, cancel := context.WithCancel(context.WithoutCancel(c.Request.Context()))
		heartbeatCancel = cancel
		heartbeatInterval := time.Duration(settings.SubmissionTimeoutSecs) * time.Second / 3
		if heartbeatInterval > 30*time.Second {
			heartbeatInterval = 30 * time.Second
		}
		go service.MaintainImageGenerationHeartbeat(
			heartbeatContext, model.DB, created.ID, heartbeatInterval,
		)
		return nil
	})
	originalWriter := c.Writer
	c.Writer = capture
	relayContext, relayCancel := context.WithTimeout(
		c.Request.Context(), time.Duration(settings.SubmissionTimeoutSecs)*time.Second,
	)
	c.Request = c.Request.WithContext(relayContext)
	Relay(c, types.RelayFormatOpenAIImage)
	relayCancel()
	c.Writer = originalWriter
	if err := capture.Close(); err != nil && capture.err == nil {
		capture.err = err
	}

	finalizeContext, cancel := context.WithTimeout(
		context.WithoutCancel(c.Request.Context()),
		time.Duration(settings.SubmissionTimeoutSecs)*time.Second,
	)
	defer cancel()
	if generation == nil {
		if capture.Status() == http.StatusConflict {
			respondImageStudioError(c, service.ErrImageStudioQuoteStale)
			return
		}
		respondImageStudioRelayFailure(c, capture.Status(), nil)
		return
	}
	finalQuota, settled, quoteExceeded := service.ImageStudioFinalQuota(c)
	if capture.err != nil {
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusArchiveFailed,
			finalQuota, "capture", "response_too_large", "image response could not be captured",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioGenerationFailure(c, finalizeContext, generation.ID)
		return
	}
	if capture.Status() < http.StatusOK || capture.Status() >= http.StatusMultipleChoices {
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusFailed,
			finalQuota, "relay", "relay_failed", "image provider request failed",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		view, viewErr := service.GetImageGenerationView(finalizeContext, model.DB, c.GetInt("id"), generation.ID)
		if viewErr != nil {
			respondImageStudioError(c, viewErr)
			return
		}
		respondImageStudioRelayFailure(c, capture.Status(), view)
		return
	}
	if quoteExceeded {
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusFailed,
			0, "billing", "provider_usage_exceeded_quote", "image provider usage exceeded the confirmed quote",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioRelayFailure(c, http.StatusBadGateway, nil)
		return
	}
	if !settled {
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusFailed,
			0, "billing", "billing_not_settled", "image generation billing was not settled",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioRelayFailure(c, http.StatusBadGateway, nil)
		return
	}
	results, err := service.ParseImageRelayResponseFile(capture.Path(), settings.MaxResponseBytes, normalized.RequestedCount)
	if err != nil {
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusArchiveFailed,
			0, "capture", "invalid_provider_response", "image provider response could not be archived",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioGenerationFailure(c, finalizeContext, generation.ID)
		return
	}
	archive, err := pipeline.ArchiveGeneration(finalizeContext, *generation, results)
	if err != nil {
		if discardErr := service.DiscardSubmittingImageGenerationAssets(
			finalizeContext, model.DB, generation.ID,
		); discardErr != nil {
			respondImageStudioError(c, errors.Join(err, discardErr))
			return
		}
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusArchiveFailed,
			0, "archive", "archive_failed", "image results could not be archived",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioGenerationFailure(c, finalizeContext, generation.ID)
		return
	}
	if archive.Ready != normalized.RequestedCount {
		if err := service.DiscardSubmittingImageGenerationAssets(
			finalizeContext, model.DB, generation.ID,
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		if err := finishImageStudioGenerationFailure(
			finalizeContext, generation.ID, model.ImageGenerationStatusArchiveFailed,
			0, "archive", "partial_archive_rejected", "image provider returned an incomplete deliverable set",
		); err != nil {
			respondImageStudioError(c, err)
			return
		}
		respondImageStudioGenerationFailure(c, finalizeContext, generation.ID)
		return
	}
	if err := service.CommitImageStudioBilling(c); err != nil {
		// The exact settlement intent and full asset set are durable. Leave the
		// generation submitting so the reconciler can retry without refunding a
		// delivered result or charging the maximum reservation.
		respondImageStudioGenerationFailure(c, finalizeContext, generation.ID)
		return
	}
	if err := service.FinishImageGeneration(
		finalizeContext, model.DB, generation.ID, model.ImageGenerationStatusSucceeded,
		archive.Ready, finalQuota, "", "", "",
	); err != nil {
		respondImageStudioError(c, err)
		return
	}
	respondImageStudioCreatedGeneration(c, finalizeContext, generation.ID)
}

func applyImageStudioTokenContext(c *gin.Context, tokenID int, modelName string) (*model.Token, error) {
	token, err := service.ValidateImageStudioToken(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, modelName, c.ClientIP(),
	)
	if err != nil {
		return nil, err
	}
	common.SetContextKey(c, constant.ContextKeyUsingGroup, token.Group)
	if err := middleware.SetupContextForToken(c, token); err != nil {
		return nil, err
	}
	return token, nil
}

func calculateImageStudioQuote(c *gin.Context) (*imageStudioQuotePrice, error) {
	request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
	if err != nil {
		return nil, err
	}
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, request, nil)
	if err != nil {
		return nil, err
	}
	if err := service.PrepareImagePricing(c, relayInfo); err != nil {
		return nil, err
	}
	meta := request.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		return nil, err
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		return nil, err
	}
	if err := service.ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &priceData, tokens, meta,
	); err != nil {
		return nil, err
	}
	var pricingSnapshot *imagepricing.Snapshot
	if relayInfo.ImagePricingSnapshot != nil {
		cloned := *relayInfo.ImagePricingSnapshot
		pricingSnapshot = &cloned
	}
	return &imageStudioQuotePrice{
		Quota: priceData.QuotaToPreConsume, OtherRatios: priceData.OtherRatios(),
		ImagePricingSnapshot: pricingSnapshot,
	}, nil
}

func reserveImageStudioIdempotency(
	c *gin.Context,
	normalized *service.NormalizedImageStudioSubmission,
) (*service.IdempotencyReservation, error) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		return nil, service.ErrInvalidIdempotencyRequest
	}
	return service.ReserveIdempotencyKey(c.Request.Context(), model.DB, service.IdempotencyReservationRequest{
		UserID: c.GetInt("id"), Operation: model.ImageIdempotencyOperationSubmit,
		Key: key, RequestHash: normalized.RequestHash,
	})
}

func replaceImageStudioRelayBody(c *gin.Context, request any) error {
	body, err := common.Marshal(request)
	if err != nil || len(body) == 0 {
		return service.ErrInvalidImageStudioSubmission
	}
	common.CleanupBodyStorage(c)
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return err
	}
	c.Set(common.KeyBodyStorage, storage)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return nil
}

func imageStudioNormalizedSubmission(c *gin.Context) (*service.NormalizedImageStudioSubmission, bool) {
	value, exists := c.Get(imageStudioNormalizedSubmissionContextKey)
	if !exists {
		return nil, false
	}
	normalized, ok := value.(*service.NormalizedImageStudioSubmission)
	return normalized, ok && normalized != nil
}

func imageStudioIdempotencyReservation(c *gin.Context) (*service.IdempotencyReservation, bool) {
	value, exists := c.Get(imageStudioIdempotencyReservationContextKey)
	if !exists {
		return nil, false
	}
	reservation, ok := value.(*service.IdempotencyReservation)
	return reservation, ok && reservation != nil
}

func setImageStudioPreRelayHook(c *gin.Context, run func() error) {
	c.Set(imageStudioPreRelayHookContextKey, &imageStudioPreRelayHook{run: run})
}

func runImageStudioPreRelayHook(c *gin.Context) error {
	value, exists := c.Get(imageStudioPreRelayHookContextKey)
	if !exists {
		return nil
	}
	hook, ok := value.(*imageStudioPreRelayHook)
	if !ok || hook == nil || hook.run == nil {
		return service.ErrInvalidImageStudioSubmission
	}
	if !hook.executed {
		hook.executed = true
		hook.err = hook.run()
	}
	return hook.err
}

func isImageStudioSubmitRequest(c *gin.Context) bool {
	return c.Request.Method == http.MethodPost && strings.TrimRight(c.Request.URL.Path, "/") == "/pg/images"
}

func respondImageStudioSubmitGuardError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrInvalidIdempotencyRequest) && strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "message": "Idempotency-Key is required", "code": "idempotency_key_required",
		})
		return
	}
	if errors.Is(err, service.ErrInvalidImageStudioSubmission) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "message": "quote_token is required", "code": "image_quote_required",
		})
		return
	}
	respondImageStudioError(c, err)
}

func respondImageStudioIdempotentReplay(c *gin.Context, resourceID string) {
	generationID, err := strconv.ParseInt(resourceID, 10, 64)
	if err != nil || generationID <= 0 {
		respondImageStudioError(c, service.ErrIdempotencyInProgress)
		return
	}
	view, err := service.GetImageGenerationView(c.Request.Context(), model.DB, c.GetInt("id"), generationID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	status := http.StatusOK
	if view.Status == model.ImageGenerationStatusSubmitting {
		status = http.StatusAccepted
		c.Header("Retry-After", "2")
	}
	imageStudioSuccess(c, status, view)
}

func finishImageStudioGenerationFailure(
	ctx context.Context,
	generationID int64,
	status string,
	finalQuota int,
	stage string,
	code string,
	message string,
) error {
	if finalQuota == 0 {
		if _, err := model.RefundImageGenerationBilling(ctx, model.DB, generationID); err != nil {
			return err
		}
	}
	if err := service.FinishImageGeneration(
		ctx, model.DB, generationID, status, 0, finalQuota, stage, code, message,
	); err != nil {
		return err
	}
	return nil
}

func respondImageStudioCreatedGeneration(c *gin.Context, ctx context.Context, generationID int64) {
	view, err := service.GetImageGenerationView(ctx, model.DB, c.GetInt("id"), generationID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusCreated, view)
}

func respondImageStudioGenerationFailure(c *gin.Context, ctx context.Context, generationID int64) {
	view, err := service.GetImageGenerationView(ctx, model.DB, c.GetInt("id"), generationID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	respondImageStudioRelayFailure(c, http.StatusBadGateway, view)
}

func respondImageStudioRelayFailure(c *gin.Context, status int, generation any) {
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{
		"success": false, "message": "image generation request failed", "code": "image_generation_failed",
		"data": generation,
	})
}
