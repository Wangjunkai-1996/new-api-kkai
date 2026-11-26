package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrVideoWebhookPersistence = errors.New("webhook_unavailable")
var ErrVideoWebhookNotReady = errors.New("webhook_endpoint_not_ready")

type VideoWebhookOwner struct {
	TenantID int64 `json:"-" gorm:"type:bigint;not null;index:idx_vhook_owner,priority:1"`
	UserID   int   `json:"-" gorm:"type:bigint;not null;index:idx_vhook_owner,priority:2"`
	TokenID  int   `json:"-" gorm:"type:bigint;not null;index:idx_vhook_owner,priority:3"`
}

type VideoWebhookEndpoint struct {
	ID string `json:"id" gorm:"type:varchar(64);primaryKey;not null"`
	VideoWebhookOwner
	URL        string `json:"url" gorm:"type:text;not null"`
	KeyVersion string `json:"-" gorm:"type:varchar(64);not null"`
	State      string `json:"state" gorm:"type:varchar(16);not null"`
	CreatedAt  int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt  int64  `json:"-" gorm:"type:bigint;not null"`
}

func (VideoWebhookEndpoint) TableName() string { return "video_webhook_endpoints" }

type VideoWebhookTask struct {
	TaskRowID  int64  `gorm:"type:bigint;primaryKey;autoIncrement:false;not null" json:"-"`
	TaskID     string `gorm:"type:varchar(191);not null;index" json:"task_id"`
	EndpointID string `gorm:"type:varchar(64);not null;index" json:"endpoint_id"`
	EventID    string `gorm:"type:varchar(64);not null;uniqueIndex" json:"event_id"`
	Reference  string `gorm:"column:client_reference;type:varchar(128)" json:"client_reference,omitempty"`
	Model      string `gorm:"type:varchar(128)" json:"model"`
	State      string `gorm:"type:varchar(16);not null;index:idx_vhook_due,priority:1" json:"state"`
	NextCheck  int64  `gorm:"type:bigint;not null;index:idx_vhook_due,priority:2" json:"-"`
	UpdatedAt  int64  `json:"-" gorm:"type:bigint;not null"`
}

func (VideoWebhookTask) TableName() string { return "video_webhook_tasks" }

// Explicit predicates include the platform tenant, whose ID is zero.
func (o VideoWebhookOwner) Scope(db *gorm.DB) *gorm.DB {
	return db.Where("tenant_id = ? AND user_id = ? AND token_id = ?", o.TenantID, o.UserID, o.TokenID)
}

func (o VideoWebhookOwner) Token(db *gorm.DB, lock bool) (*Token, error) {
	var token Token
	if lock {
		db = lockForUpdate(db)
	}
	err := db.Select("id", "tenant_id", "user_id", "status", "expired_time").Where("id = ? AND tenant_id = ? AND user_id = ?", o.TokenID, o.TenantID, o.UserID).First(&token).Error
	return &token, err
}

func (o VideoWebhookOwner) ChangeEndpoint(ctx context.Context, db *gorm.DB, action, id string, pending *VideoWebhookEndpoint) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := o.Token(tx, true); err != nil {
			return err
		}
		endpoints := func() *gorm.DB { return o.Scope(tx.Model(&VideoWebhookEndpoint{})) }
		switch action {
		case "create":
			if pending == nil || pending.VideoWebhookOwner != o {
				return ErrVideoWebhookNotReady
			}
			if err := endpoints().Where("state = ?", "pending").Update("state", "revoked").Error; err != nil {
				return err
			}
			return tx.Create(pending).Error
		case "verify":
			if err := endpoints().Where("state = ?", "active").Update("state", "retired").Error; err != nil {
				return err
			}
			result := endpoints().Where("id = ? AND state = ?", id, "pending").Update("state", "active")
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVideoWebhookNotReady
			}
			return nil
		case "revoke":
			query := endpoints().Where("state <> ?", "revoked")
			if id != "" {
				query = query.Where("id = ?", id)
			}
			return query.Update("state", "revoked").Error
		default:
			return errors.New("invalid endpoint change")
		}
	})
}

func (o VideoWebhookOwner) Subscribe(tx *gorm.DB, t *Task, endpointID, eventID, ref, publicModel string) error {
	if t.TenantID != o.TenantID || t.UserId != o.UserID || t.PrivateData.TokenId != o.TokenID {
		return ErrVideoWebhookNotReady
	}
	if _, err := o.Token(tx, true); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoWebhookNotReady
		}
		return err
	}
	var endpoint VideoWebhookEndpoint
	// The task creation hook may already have established a MySQL repeatable-read snapshot.
	// Read the endpoint under a lock to observe revocations committed while waiting for the token.
	if err := lockForUpdate(o.Scope(tx)).Select("id").Where("id = ? AND state = ?", endpointID, "active").First(&endpoint).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoWebhookNotReady
		}
		return err
	}
	return tx.Create(&VideoWebhookTask{TaskRowID: t.ID, TaskID: t.TaskID, EndpointID: endpoint.ID, EventID: eventID, Reference: ref, Model: publicModel, State: "waiting", NextCheck: time.Now().Unix()}).Error
}
