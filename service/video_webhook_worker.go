package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type videoWebhookEvent struct {
	Version    int              `json:"version"`
	EventID    string           `json:"event_id"`
	EventType  string           `json:"event_type"`
	TaskID     string           `json:"task_id"`
	Reference  string           `json:"client_reference,omitempty"`
	Model      string           `json:"model"`
	Status     model.TaskStatus `json:"status"`
	FinishedAt int64            `json:"finished_at"`
	ResultURL  string           `json:"result_url,omitempty"`
	ErrorCode  string           `json:"error_code,omitempty"`
	FailReason string           `json:"fail_reason,omitempty"`
}

func videoWebhookKey(row int64) string { return "video-webhook-terminal:" + strconv.FormatInt(row, 10) }

func observeVideoWebhook(ctx context.Context, db *gorm.DB, sub model.VideoWebhookTask, now time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A guarded claim and the outbox insert commit together. A lost race does no work.
		claim := tx.Model(&model.VideoWebhookTask{}).Where("task_row_id = ? AND state = ? AND next_check <= ?", sub.TaskRowID, "waiting", now.Unix()).Update("next_check", now.Add(20*time.Second).Unix())
		if claim.Error != nil || claim.RowsAffected != 1 {
			return claim.Error
		}
		var task model.Task
		err := tx.First(&task, "id = ?", sub.TaskRowID).Error
		state := ""
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = "unavailable"
		} else if err != nil {
			return err
		}
		if state == "" {
			var endpoint model.VideoWebhookEndpoint
			if err = tx.First(&endpoint, "id = ?", sub.EndpointID).Error; err != nil {
				return err
			}
			switch {
			case task.TaskID != sub.TaskID || task.TenantID != endpoint.TenantID || task.UserId != endpoint.UserID || task.PrivateData.TokenId != endpoint.TokenID:
				state = "unavailable"
			case endpoint.State == "revoked":
				state = "revoked"
			case task.Status == model.TaskStatusUnknown:
				return tx.Model(&sub).Update("next_check", now.Add(5*time.Minute).Unix()).Error
			case task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure:
				return nil
			case task.PrivateData.UpstreamTaskID == "":
				state = "unaccepted"
			case task.FinishTime <= 0:
				state = "unavailable"
			default:
				event := videoWebhookEvent{Version: 1, EventID: sub.EventID, TaskID: sub.TaskID, Reference: sub.Reference, Model: sub.Model, Status: task.Status, FinishedAt: task.FinishTime}
				if task.Status == model.TaskStatusSuccess {
					hosted, err := TaskUsesStudioAssets(ctx, tx, &task)
					if err != nil {
						return err
					}
					if !task.ResultRetrievable() || hosted {
						state = "unavailable"
						break
					}
					base, err := videoWebhookDownloadOrigin()
					if err != nil {
						return err
					}
					event.EventType = "video.completed"
					event.ResultURL = base.JoinPath("v1", "videos", task.TaskID, "content").String()
				} else {
					event.EventType = "video.failed"
					event.ErrorCode = "generation_failure"
					event.FailReason = "Video generation failed."
					if f := task.FailureForDisplay(); f != nil {
						if f.Code != "" && f.Code != "historical_detail_unavailable" {
							event.ErrorCode = f.Code
						}
						switch event.ErrorCode {
						case "privacy_restriction":
							event.FailReason = "Video generation failed due to a privacy restriction."
						case "copyright_restriction":
							event.FailReason = "Video generation failed due to a copyright restriction."
						case "content_policy_violation":
							event.FailReason = "Video generation failed the content policy check."
						case "temporary_failure":
							event.FailReason = "Video generation failed due to a temporary service issue."
						}
					}
				}
				if err := EnqueueKKAIOutboxEvent(ctx, tx, videoWebhookKey(sub.TaskRowID), VideoWebhookTopic, strconv.FormatInt(sub.TaskRowID, 10), now, event); err != nil {
					return err
				}
				state = "queued"
			}
		}
		return tx.Model(&sub).Update("state", state).Error
	})
}

func videoWebhookRecipient(ctx context.Context, db *gorm.DB, event model.KKAIOutboxEvent) (model.VideoWebhookEndpoint, error) {
	var endpoint model.VideoWebhookEndpoint
	var sub model.VideoWebhookTask
	row, err := strconv.ParseInt(event.AggregateID, 10, 64)
	if err != nil || row <= 0 || event.EventKey != videoWebhookKey(row) || event.Topic != VideoWebhookTopic {
		return endpoint, PermanentKKAIOutboxError(errors.New("invalid_webhook_event"))
	}
	db = db.WithContext(ctx)
	if err = db.First(&sub, "task_row_id = ?", row).Error; err != nil {
		return endpoint, err
	}
	var body videoWebhookEvent
	if common.UnmarshalJsonStr(event.Payload, &body) != nil || body.EventID != sub.EventID || body.TaskID != sub.TaskID || sub.State != "queued" {
		return endpoint, PermanentKKAIOutboxError(errors.New("invalid_webhook_event"))
	}
	if err = db.First(&endpoint, "id = ?", sub.EndpointID).Error; err != nil {
		return endpoint, err
	}
	var task model.Task
	err = db.Select("task_id", "tenant_id", "user_id", "private_data").First(&task, row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (task.TaskID != sub.TaskID || task.TenantID != endpoint.TenantID || task.UserId != endpoint.UserID || task.PrivateData.TokenId != endpoint.TokenID)) {
		return endpoint, PermanentKKAIOutboxError(errors.New("webhook_task_unavailable"))
	}
	if err != nil {
		return endpoint, err
	}
	if endpoint.State != "active" && endpoint.State != "retired" {
		return endpoint, PermanentKKAIOutboxError(errors.New("webhook_revoked"))
	}
	token, err := endpoint.Token(db, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return endpoint, PermanentKKAIOutboxError(errors.New("webhook_token_revoked"))
	}
	if err != nil {
		return endpoint, err
	}
	if (token.Status != common.TokenStatusEnabled && token.Status != common.TokenStatusExhausted) || (token.ExpiredTime > 0 && token.ExpiredTime <= time.Now().Unix()) {
		return endpoint, PermanentKKAIOutboxError(errors.New("webhook_token_revoked"))
	}
	var owners int64
	err = db.Model(&model.User{}).Where("id = ? AND tenant_id = ? AND status = ?", endpoint.UserID, endpoint.TenantID, common.UserStatusEnabled).Count(&owners).Error
	if err != nil {
		return endpoint, err
	}
	if owners != 1 {
		return endpoint, PermanentKKAIOutboxError(errors.New("webhook_owner_disabled"))
	}
	return endpoint, nil
}

func registerVideoWebhookJobs(registry *BackgroundJobRegistry, workerID string) error {
	if os.Getenv("KKAI_VIDEO_WEBHOOK_ENABLED") != "true" {
		return nil
	}
	db := model.DB
	var schemaErr error
	if common.CanRunRuntimeAutoMigrate() {
		schemaErr = kkaimigrate.ApplyVideoWebhookExpansion(context.Background(), db, kkaimigrate.Options{Offline: true})
	} else {
		schemaErr = kkaimigrate.CheckVideoWebhookSchema(db)
	}
	videoWebhookSchemaUnavailable.Store(schemaErr != nil)
	if schemaErr != nil {
		common.SysError(fmt.Sprintf("video webhook disabled until schema is ready and process restarts: %v", schemaErr))
		return nil
	}
	db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	err := registry.Register(BackgroundJob{Name: "video-webhook-observe", Interval: 5 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true, Run: func(ctx context.Context) error {
		var rows []model.VideoWebhookTask
		now := time.Now()
		if err := db.WithContext(ctx).Where("state = ? AND next_check <= ?", "waiting", now.Unix()).Order("next_check, task_row_id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		var firstErr error
		for _, row := range rows {
			if err := observeVideoWebhook(ctx, db, row, now); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("webhook observation %d: %w", row.TaskRowID, err)
				}
				if ctx.Err() != nil {
					break
				}
				// Give other subscriptions a turn even when one row repeatedly fails.
				deferred := db.WithContext(ctx).Model(&row).Where("state = ? AND next_check <= ?", "waiting", now.Unix()).Update("next_check", now.Add(time.Minute).Unix())
				if deferred.Error != nil {
					return errors.Join(firstErr, deferred.Error)
				}
			}
		}
		return firstErr
	}})
	if err != nil {
		return err
	}
	worker := NewKKAIOutboxProcessor(db, workerID+"-video-webhook")
	if err := worker.Register(VideoWebhookTopic, func(ctx context.Context, event model.KKAIOutboxEvent) error {
		ep, err := videoWebhookRecipient(ctx, db, event)
		if err != nil {
			return err
		}
		attemptAt := time.Now()
		err = sendVideoWebhook(ctx, videoWebhookClient, ep, []byte(event.Payload), "")
		if event.Attempts == 0 && !errors.Is(err, ErrVideoWebhookUnavailable) {
			var body videoWebhookEvent
			if common.UnmarshalJsonStr(event.Payload, &body) == nil {
				common.SysLog(fmt.Sprintf("video webhook first attempt event_id=%s terminal_to_attempt_seconds=%d", body.EventID, attemptAt.Unix()-body.FinishedAt))
			}
		}
		if errors.Is(err, ErrVideoWebhookUnavailable) {
			common.SysError("video webhook signing key version unavailable")
			return DeferKKAIOutboxUntil(time.Now().Add(time.Minute), err)
		}
		return err
	}); err != nil {
		return err
	}
	if err := registry.Register(BackgroundJob{Name: "video-webhook-cleanup", Interval: time.Hour, WritesData: true, RequiresLeaderLease: true, Run: func(ctx context.Context) error {
		return cleanupVideoWebhooks(ctx, db, time.Now().Add(-30*24*time.Hour).Unix())
	}}); err != nil {
		return err
	}
	return registry.Register(BackgroundJob{Name: "video-webhook-deliver", Interval: 2 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true, Run: func(ctx context.Context) error {
		if os.Getenv("KKAI_VIDEO_WEBHOOK_PAUSE_DELIVERY") == "true" {
			return nil
		}
		_, err := worker.ProcessBatch(ctx, 10)
		return err
	}})
}

func cleanupVideoWebhooks(ctx context.Context, db *gorm.DB, cutoff int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var events []model.KKAIOutboxEvent
		if err := tx.Select("id", "aggregate_id").Where("topic = ? AND ((status = ? AND delivered_at < ?) OR (status = ? AND available_at < ?))", VideoWebhookTopic, "delivered", cutoff, "dead", cutoff).Order("id").Limit(250).Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			del := tx.Where("id = ? AND ((status = ? AND delivered_at < ?) OR (status = ? AND available_at < ?))", event.ID, "delivered", cutoff, "dead", cutoff).Delete(&model.KKAIOutboxEvent{})
			if del.Error != nil {
				return del.Error
			}
			if del.RowsAffected == 1 {
				if err := tx.Where("task_row_id = ? AND state = ?", event.AggregateID, "queued").Delete(&model.VideoWebhookTask{}).Error; err != nil {
					return err
				}
			}
		}
		var rows []int64
		expired := tx.Model(&model.VideoWebhookTask{}).Where("state IN ? AND updated_at < ?", []string{"unaccepted", "unavailable", "revoked"}, cutoff)
		if err := expired.Limit(250).Pluck("task_row_id", &rows).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.Where("task_row_id IN ?", rows).Delete(&model.VideoWebhookTask{}).Error; err != nil {
				return err
			}
		}
		const unused = "state IN ? AND updated_at < ? AND NOT EXISTS (SELECT 1 FROM video_webhook_tasks WHERE endpoint_id = video_webhook_endpoints.id)"
		states := []string{"retired", "revoked", "pending"}
		var versions []string
		if err := tx.Model(&model.VideoWebhookEndpoint{}).Where(unused, states, cutoff).Limit(250).Pluck("id", &versions).Error; err != nil {
			return err
		}
		if len(versions) == 0 {
			return nil
		}
		return tx.Where("id IN ?", versions).Where(unused, states, cutoff).Delete(&model.VideoWebhookEndpoint{}).Error
	})
}

type VideoWebhookDelivery struct {
	model.VideoWebhookTask
	DeliveryStatus string `json:"delivery_status,omitempty"`
	FailedAttempts int    `json:"failed_attempts"`
	LastError      string `json:"last_error,omitempty"`
	NextAttempt    int64  `json:"next_attempt,omitempty"`
	DeliveredAt    int64  `json:"delivered_at,omitempty"`
}

func VideoWebhookDeliveries(db *gorm.DB, subs []model.VideoWebhookTask) ([]VideoWebhookDelivery, error) {
	keys := make([]string, 0, len(subs))
	for _, s := range subs {
		keys = append(keys, videoWebhookKey(s.TaskRowID))
	}
	var events []model.KKAIOutboxEvent
	if len(keys) > 0 {
		if err := db.Select("event_key", "status", "attempts", "last_error", "available_at", "delivered_at").Where("topic = ? AND event_key IN ?", VideoWebhookTopic, keys).Find(&events).Error; err != nil {
			return nil, err
		}
	}
	byKey := make(map[string]model.KKAIOutboxEvent, len(events))
	for _, e := range events {
		byKey[e.EventKey] = e
	}
	result := make([]VideoWebhookDelivery, 0, len(subs))
	for _, s := range subs {
		e := byKey[videoWebhookKey(s.TaskRowID)]
		d := VideoWebhookDelivery{VideoWebhookTask: s, DeliveryStatus: e.Status, FailedAttempts: e.Attempts, DeliveredAt: e.DeliveredAt}
		if e.Status == "pending" {
			d.NextAttempt = e.AvailableAt
		}
		if e.LastError != "" && e.Status != model.KKAIOutboxStatusDelivered {
			// Operator identities and redrive audit markers remain private.
			d.LastError = "delivery_failed"
			if strings.HasPrefix(e.LastError, "webhook_") && len(e.LastError) < 48 {
				d.LastError = e.LastError
			}
		}
		result = append(result, d)
	}
	return result, nil
}

var errVideoWebhookRedriveReplay = errors.New("webhook redrive key already used")

func RedriveVideoWebhook(ctx context.Context, db *gorm.DB, eventID, key, actor string, now time.Time) (bool, error) {
	db = db.WithContext(ctx)
	var sub model.VideoWebhookTask
	if err := db.Select("task_row_id").Where("event_id = ?", eventID).First(&sub).Error; err != nil {
		return false, err
	}
	var event model.KKAIOutboxEvent
	if err := db.Select("id").Where("event_key = ? AND topic = ?", videoWebhookKey(sub.TaskRowID), VideoWebhookTopic).First(&event).Error; err != nil {
		return false, err
	}
	_, applied, err := redriveKKAIOutboxDeadEvent(ctx, db, event.ID, key, actor, now, func(ctx context.Context, tx *gorm.DB, e model.KKAIOutboxEvent, _ time.Time) error {
		if e.Topic != VideoWebhookTopic {
			return errors.New("invalid_webhook_event")
		}
		endpoint, err := videoWebhookRecipient(ctx, tx, e)
		if err != nil {
			return err
		}
		// Keep older ticket keys idempotent even after multiple subsequent redrives.
		reservation, err := ReserveIdempotencyKey(ctx, tx, IdempotencyReservationRequest{
			UserID: endpoint.UserID, Operation: "video_webhook_redrive", Key: eventID + ":" + key,
			RequestHash: fmt.Sprintf("%x", sha256.Sum256([]byte(eventID))), TTL: 30 * 24 * time.Hour,
		})
		if err != nil {
			return err
		}
		if !reservation.Created {
			return errVideoWebhookRedriveReplay
		}
		return BindIdempotencyResource(ctx, tx, reservation.Record.ID, "video_webhook_event", eventID)
	})
	if errors.Is(err, errVideoWebhookRedriveReplay) {
		return false, nil
	}
	return applied, err
}
