package controller

import (
	"errors"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var videoWebhookReference = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

func webhookError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "code": code, "message": code})
}

func PrepareVideoWebhook(c *gin.Context) {
	h := c.Request.Header
	modes, refs := h.Values(service.VideoWebhookHeader), h.Values(service.VideoWebhookReferenceHeader)
	if len(modes) == 0 && len(refs) == 0 {
		return
	}
	h.Del(service.VideoWebhookHeader)
	h.Del(service.VideoWebhookReferenceHeader)
	if c.Request.Method != http.MethodPost || (c.Request.URL.Path != "/v1/videos" && c.Request.URL.Path != "/v1/video/generations") || len(modes) != 1 || modes[0] != "terminal" || len(refs) > 1 || (len(refs) == 1 && !videoWebhookReference.MatchString(refs[0])) {
		webhookError(c, 400, "invalid_webhook_request")
		return
	}
	owner := model.VideoWebhookOwner{TenantID: common.GetContextKeyInt64(c, constant.ContextKeyTenantID), UserID: c.GetInt("id"), TokenID: c.GetInt("token_id")}
	if owner.TenantID != model.PlatformTenantID || !service.VideoWebhookMayRegister(owner.TokenID) {
		webhookError(c, 503, "webhook_unavailable")
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if c.ContentType() != gin.MIMEJSON {
		webhookError(c, 400, "invalid_webhook_request")
		return
	}
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			webhookError(c, http.StatusRequestEntityTooLarge, "request_body_too_large")
			return
		}
		webhookError(c, 400, "invalid_webhook_request")
		return
	}
	switch req.Model {
	case "seedance-2.5", "sd_2.5_special_720p", "sd_2.5_special_1080p", "sd_2.5_special_720p_with_video_ref", "sd_2.5_special_1080p_with_video_ref":
	default:
		webhookError(c, 400, "webhook_model_unsupported")
		return
	}
	var endpoint model.VideoWebhookEndpoint
	err := owner.Scope(model.DB.WithContext(c.Request.Context())).Where("state = ?", "active").First(&endpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		webhookError(c, 409, "webhook_endpoint_not_ready")
		return
	}
	if err != nil {
		webhookError(c, 503, "webhook_unavailable")
		return
	}
	if _, err = service.VideoWebhookSecret(endpoint); err != nil {
		webhookError(c, 503, "webhook_unavailable")
		return
	}
	ref := ""
	if len(refs) == 1 {
		ref = refs[0]
	}
	eventID := service.NewVideoWebhookID("evt_")
	relay.SetTaskProvisionalPersistHook(c, func(tx *gorm.DB, task *model.Task) error {
		subscriptionDB := tx.Session(&gorm.Session{Logger: tx.Logger.LogMode(gormlogger.Silent)})
		if err := owner.Subscribe(subscriptionDB, task, endpoint.ID, eventID, ref, req.Model); err != nil {
			if errors.Is(err, model.ErrVideoWebhookNotReady) {
				return err
			}
			return errors.Join(model.ErrVideoWebhookPersistence, err)
		}
		return nil
	})
	c.Set("video_webhook_requested", true)
}

func VideoWebhookDeliveries(c *gin.Context) { manageVideoWebhook(c, true) }
func ManageVideoWebhook(c *gin.Context)     { manageVideoWebhook(c, false) }

func manageVideoWebhook(c *gin.Context, listDeliveries bool) {
	id, err := strconv.Atoi(c.Param("id"))
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok || c.GetBool("use_access_token") {
		webhookError(c, 403, "dashboard_session_required")
		return
	}
	if err != nil || id <= 0 {
		webhookError(c, 400, "invalid_token_id")
		return
	}
	owner := model.VideoWebhookOwner{TenantID: common.GetContextKeyInt64(c, constant.ContextKeyTenantID), UserID: identity.UserID, TokenID: id}
	ctx := c.Request.Context()
	db := model.DB.WithContext(ctx).Session(&gorm.Session{Logger: model.DB.Logger.LogMode(gormlogger.Silent)})
	if !service.VideoWebhookEnabled() || owner.TenantID != model.PlatformTenantID {
		webhookError(c, 503, "webhook_unavailable")
		return
	}
	if _, err := owner.Token(db, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			webhookError(c, 404, "token_not_found")
		} else {
			webhookError(c, 503, "webhook_unavailable")
		}
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var data any
	action := ""
	switch c.Request.Method {
	case http.MethodGet:
		if listDeliveries {
			var rows []model.VideoWebhookTask
			q := db.Where("endpoint_id IN (?)", owner.Scope(db.Model(&model.VideoWebhookEndpoint{})).Select("id"))
			if key := c.Query("task_id"); key != "" {
				q = q.Where("task_id = ?", key)
			}
			if key := c.Query("event_id"); key != "" {
				q = q.Where("event_id = ?", key)
			}
			if err = q.Order("task_row_id DESC").Limit(30).Find(&rows).Error; err == nil {
				data, err = service.VideoWebhookDeliveries(db, rows)
			}
		} else {
			endpoints := make([]model.VideoWebhookEndpoint, 0)
			err = owner.Scope(db).Where("state <> ?", "revoked").Order("created_at DESC, id").Limit(100).Find(&endpoints).Error
			data = endpoints
		}
	case http.MethodPut:
		if !service.VideoWebhookMayRegister(id) {
			webhookError(c, 503, "webhook_unavailable")
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		if err = common.DecodeJsonSingle(c.Request.Body, &req); err != nil || service.ValidateVideoWebhookURL(ctx, req.URL) != nil {
			webhookError(c, 400, "invalid_webhook_url")
			return
		}
		endpoint := model.VideoWebhookEndpoint{ID: service.NewVideoWebhookID("wh_"), VideoWebhookOwner: owner, URL: req.URL, KeyVersion: os.Getenv("KKAI_VIDEO_WEBHOOK_KEY_ID"), State: "pending"}
		var secret string
		secret, err = service.VideoWebhookSecret(endpoint)
		if err == nil {
			err = owner.ChangeEndpoint(ctx, db, "create", "", &endpoint)
		}
		data = gin.H{"endpoint": endpoint, "secret": secret}
		action = "create"
	case http.MethodPost:
		if !service.VideoWebhookMayRegister(id) {
			webhookError(c, 503, "webhook_unavailable")
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if common.DecodeJsonSingle(c.Request.Body, &req) != nil || !videoWebhookReference.MatchString(req.ID) || len(req.ID) > 64 {
			webhookError(c, 400, "endpoint_id_required")
			return
		}
		var ep model.VideoWebhookEndpoint
		err = owner.Scope(db).Where("id = ? AND state = ?", req.ID, "pending").First(&ep).Error
		if err == nil {
			if err = service.VerifyVideoWebhook(ctx, ep); err != nil {
				webhookError(c, 422, "webhook_verification_failed")
				return
			}
			err = owner.ChangeEndpoint(ctx, db, "verify", ep.ID, nil)
		}
		action = "verify"
	case http.MethodDelete:
		// A version-specific revoke cannot accidentally become revoke-all.
		err = owner.ChangeEndpoint(ctx, db, "revoke", c.Param("version"), nil)
		action = "revoke"
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrVideoWebhookNotReady) {
			webhookError(c, 409, "webhook_endpoint_not_ready")
		} else {
			webhookError(c, 503, "webhook_unavailable")
		}
		return
	}
	if action != "" {
		model.RecordAuditLog(c, model.AuditLog{UserId: identity.UserID, ActorRole: c.GetInt("role"), Category: model.AuditCategorySecurity, Action: "video_webhook." + action, TokenRef: strconv.Itoa(id), Success: true})
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}

func RedriveVideoWebhook(c *gin.Context) {
	if !service.VideoWebhookEnabled() {
		webhookError(c, 503, "webhook_unavailable")
		return
	}
	var req struct {
		Key    string `json:"redrive_key"`
		Reason string `json:"reason"`
	}
	eventID := c.Param("event")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if len(eventID) > 64 || !videoWebhookReference.MatchString(eventID) || common.DecodeJsonSingle(c.Request.Body, &req) != nil || len(req.Reason) == 0 || len(req.Reason) > 128 || !videoWebhookReference.MatchString(req.Key) || len(req.Key) > 64 {
		webhookError(c, 400, "invalid_redrive_request")
		return
	}
	actor := "user-" + strconv.Itoa(c.GetInt("id"))
	applied, err := service.RedriveVideoWebhook(c.Request.Context(), model.DB, eventID, req.Key, actor, time.Now())
	if err != nil {
		webhookError(c, 409, "webhook_redrive_unavailable")
		return
	}
	model.RecordAuditLog(c, model.AuditLog{UserId: c.GetInt("id"), ActorRole: c.GetInt("role"), Category: model.AuditCategoryOperation, Action: "video_webhook.redrive", Content: eventID + " " + model.SafeTaskFailureMessage(req.Reason), Success: true})
	c.JSON(200, gin.H{"success": true, "data": gin.H{"applied": applied}})
}
