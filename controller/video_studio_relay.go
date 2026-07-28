package controller

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const videoStudioNormalizedSubmissionContextKey = "video_studio_normalized_submission"
const videoStudioIdempotencyReservationContextKey = "video_studio_idempotency_reservation"

func PrepareVideoStudioTaskRequest(c *gin.Context) {
	var request service.VideoStudioSubmissionRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoStudioSubmission)
		c.Abort()
		return
	}
	token, err := applyVideoStudioTokenContext(c, request.TokenID, request.Model)
	if err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	request.Group = token.Group
	if isVideoStudioSubmitRequest(c) {
		if err := prepareVideoStudioIdempotency(c, request); err != nil {
			respondVideoStudioSubmitGuardError(c, err)
			c.Abort()
			return
		}
		if c.IsAborted() {
			return
		}
		reservation, _ := videoStudioIdempotencyReservation(c)
		defer func() {
			if reservation == nil || !reservation.Created {
				return
			}
			if err := service.ReleaseUnboundIdempotencyReservation(c.Request.Context(), model.DB, reservation.Record.ID); err != nil {
				common.SysError("release video studio idempotency reservation: " + err.Error())
			}
		}()
	}
	if isVideoStudioSubmitRequest(c) && request.QuoteExpiresAt <= time.Now().Unix() {
		respondVideoStudioQuoteMismatch(c)
		c.Abort()
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	normalized, err := service.NormalizeVideoStudioSubmission(c.Request.Context(), model.DB, store, c.GetInt("id"), request)
	if err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	token, err = applyVideoStudioTokenContext(c, normalized.TokenID, normalized.Model)
	if err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	if err := service.ApplyVideoStudioEffectiveGroup(normalized, token.Group); err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	if err := replaceVideoStudioTaskBody(c, normalized.TaskPayload); err != nil {
		respondVideoStudioError(c, err)
		c.Abort()
		return
	}
	c.Set(videoStudioNormalizedSubmissionContextKey, normalized)
	c.Next()
}

func QuoteVideoStudioTask(c *gin.Context) {
	normalized, ok := videoStudioNormalizedSubmission(c)
	if !ok {
		respondVideoStudioError(c, service.ErrInvalidVideoStudioSubmission)
		return
	}
	if err := applyVideoStudioEffectiveGroup(c, normalized); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	quote, taskErr := calculateVideoStudioQuote(c)
	if taskErr != nil {
		respondVideoStudioTaskError(c, taskErr)
		return
	}
	common.ApiSuccess(c, service.NewVideoStudioQuote(normalized, quote.Quota, quote.OtherRatios))
}

func SubmitVideoStudioTask(c *gin.Context) {
	normalized, ok := videoStudioNormalizedSubmission(c)
	if !ok || service.ValidateVideoStudioSubmitRequest(service.VideoStudioSubmissionRequest{
		TokenID: normalized.TokenID, MaxQuota: normalized.MaxQuota,
		QuoteHash: normalized.QuoteHash, QuoteExpiresAt: normalized.QuoteExpiresAt,
	}) != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "message": "max_quota and quote_hash are required", "code": "video_quote_required",
		})
		return
	}
	if err := applyVideoStudioEffectiveGroup(c, normalized); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.ValidateVideoStudioQuote(normalized, time.Now()); err != nil {
		respondVideoStudioQuoteMismatch(c)
		return
	}
	quote, taskErr := calculateVideoStudioQuote(c)
	if taskErr != nil {
		respondVideoStudioTaskError(c, taskErr)
		return
	}
	if quote.Quota > *normalized.MaxQuota {
		respondVideoStudioQuoteStale(c, quote.Quota)
		return
	}

	reservation, exists := videoStudioIdempotencyReservation(c)
	if !exists || reservation == nil || !reservation.Created {
		respondVideoStudioError(c, service.ErrInvalidIdempotencyRequest)
		return
	}

	relay.SetTaskMaxQuota(c, *normalized.MaxQuota)
	relay.SetTaskProvisionalPersistHook(c, func(tx *gorm.DB, task *model.Task) error {
		if err := service.BindIdempotencyResource(c.Request.Context(), tx, reservation.Record.ID, model.VideoIdempotencyResourceTask, task.TaskID); err != nil {
			return err
		}
		_, err := service.CreateVideoGeneration(c.Request.Context(), tx, normalized, task.ID)
		return err
	})
	RelayTask(c)
}

func applyVideoStudioTokenContext(c *gin.Context, tokenID int, modelName string) (*model.Token, error) {
	token, err := service.ValidateVideoStudioToken(
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

func prepareVideoStudioIdempotency(c *gin.Context, request service.VideoStudioSubmissionRequest) error {
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		return service.ErrInvalidIdempotencyRequest
	}
	if err := service.ValidateVideoStudioSubmitRequest(request); err != nil {
		return err
	}
	fingerprint, err := service.VideoStudioIdempotencyFingerprint(request)
	if err != nil {
		return service.ErrInvalidIdempotencyRequest
	}
	reservation, err := service.ReserveIdempotencyKey(c.Request.Context(), model.DB, service.IdempotencyReservationRequest{
		UserID: c.GetInt("id"), Operation: model.VideoIdempotencyOperationSubmit,
		Key: idempotencyKey, RequestHash: fingerprint,
	})
	if err != nil {
		return err
	}
	if !reservation.Created {
		respondVideoStudioIdempotentReplay(c, request.Model, reservation.Record.ResourceID)
		c.Abort()
		return nil
	}
	c.Set(videoStudioIdempotencyReservationContextKey, reservation)
	return nil
}

func applyVideoStudioEffectiveGroup(c *gin.Context, normalized *service.NormalizedVideoStudioSubmission) error {
	group := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	if group == "" {
		return service.ErrInvalidVideoStudioSubmission
	}
	if err := service.ApplyVideoStudioEffectiveGroup(normalized, group); err != nil {
		return err
	}
	return replaceVideoStudioTaskBody(c, normalized.TaskPayload)
}

func videoStudioIdempotencyReservation(c *gin.Context) (*service.IdempotencyReservation, bool) {
	value, exists := c.Get(videoStudioIdempotencyReservationContextKey)
	if !exists {
		return nil, false
	}
	reservation, ok := value.(*service.IdempotencyReservation)
	return reservation, ok && reservation != nil
}

func isVideoStudioSubmitRequest(c *gin.Context) bool {
	return c.Request.Method == http.MethodPost && strings.TrimRight(c.Request.URL.Path, "/") == "/pg/videos"
}

func respondVideoStudioSubmitGuardError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrInvalidIdempotencyRequest) && strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "message": "Idempotency-Key is required", "code": "idempotency_key_required",
		})
		return
	}
	if errors.Is(err, service.ErrInvalidVideoStudioSubmission) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "message": "max_quota and quote_hash are required", "code": "video_quote_required",
		})
		return
	}
	respondVideoStudioError(c, err)
}

func calculateVideoStudioQuote(c *gin.Context) (*relay.TaskQuoteResult, *dto.TaskError) {
	if taskErr := PreparePlaygroundTaskContext(c); taskErr != nil {
		return nil, taskErr
	}
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "gen_relay_info_failed", http.StatusInternalServerError)
	}
	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		return nil, taskErr
	}
	return relay.QuoteTaskSubmission(c, relayInfo)
}

func replaceVideoStudioTaskBody(c *gin.Context, body []byte) error {
	if len(body) == 0 {
		return service.ErrInvalidVideoStudioSubmission
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

func videoStudioNormalizedSubmission(c *gin.Context) (*service.NormalizedVideoStudioSubmission, bool) {
	value, exists := c.Get(videoStudioNormalizedSubmissionContextKey)
	if !exists {
		return nil, false
	}
	normalized, ok := value.(*service.NormalizedVideoStudioSubmission)
	return normalized, ok && normalized != nil
}

func respondVideoStudioTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr == nil {
		return
	}
	status := taskErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	c.JSON(status, taskErr)
}

func respondVideoStudioQuoteStale(c *gin.Context, currentQuota int) {
	c.JSON(http.StatusConflict, gin.H{
		"code": "quote_stale", "message": "video quote is stale",
		"data": gin.H{"current_quota": currentQuota},
	})
}

func respondVideoStudioQuoteMismatch(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"code": "quote_stale", "message": service.ErrVideoStudioQuoteMismatch.Error(),
	})
}

func respondVideoStudioIdempotentReplay(c *gin.Context, modelName string, taskID string) {
	if taskID == "" {
		respondVideoStudioError(c, service.ErrIdempotencyInProgress)
		return
	}
	response := dto.NewOpenAIVideo()
	response.ID = taskID
	response.TaskID = taskID
	response.Model = modelName
	if task, exists, err := model.GetByTaskId(c.GetInt("id"), taskID); err == nil && exists {
		if response.Model == "" {
			response.Model = task.Properties.OriginModelName
		}
		response.Status = task.Status.ToVideoStatus()
		response.SetProgressStr(task.Progress)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.SysError("load idempotent video task: " + err.Error())
	}
	c.JSON(http.StatusOK, response)
}
