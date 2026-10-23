package service

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

// InvoiceNotificationHandler retries progress email through the existing durable
// outbox. The durable in-site event remains authoritative, even without SMTP.
type InvoiceNotificationHandler struct {
	DB   *gorm.DB
	Send func(subject, recipient, content string) error
}

func (h InvoiceNotificationHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	var payload struct {
		EventID int64 `json:"event_id"`
	}
	if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.EventID <= 0 {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid invoice notification reference"))
	}
	db := h.DB
	if db == nil {
		db = model.DB
	}
	var update model.InvoiceEvent
	if err := db.WithContext(ctx).Where("id = ?", payload.EventID).First(&update).Error; err != nil {
		return err
	}
	// User actions are already visible to their author. Administrators receive
	// the queue's own in-site notifications, never a copy of customer tax data.
	if !update.ActorAdmin || common.SMTPServer == "" && h.Send == nil {
		return nil
	}
	var request model.InvoiceRequest
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND user_id = ?", update.RequestID, update.TenantID, update.UserID).First(&request).Error; err != nil {
		return err
	}
	var profile InvoiceProfileData
	if err := invoiceDecrypt(invoiceAAD(request.TenantID, request.UserID, request.PublicID, "profile"), request.ProfileCipher, &profile); err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(profile.Email)
	if err != nil || recipient.Address != profile.Email || strings.ContainsAny(profile.Email, "\r\n,;") {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid invoice notification recipient"))
	}
	// Never mail the attachment, tax ID, personal description or invoice title
	// to an address entered in a form. The recipient must sign in to see them.
	content := "<p>Your invoice request has an update.</p><p>" + html.EscapeString(request.PublicID) + "</p><p>Sign in to the invoice center to review the current status and documents.</p>"
	send := h.Send
	if send == nil {
		send = common.SendEmail
	}
	if err := send("Invoice request update", recipient.Address, content); err != nil {
		// SMTP errors can include recipient/server details. Keep retry errors safe.
		return fmt.Errorf("invoice email delivery failed")
	}
	return nil
}

func RegisterInvoiceBackgroundJobs(registry *BackgroundJobRegistry) error {
	return registry.Register(BackgroundJob{
		Name: "invoice-staged-cleanup", Interval: time.Hour, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if kkaimigrate.CheckInvoiceSchema(ctx, model.DB) != nil {
				return nil
			}
			assets, err := NewInvoiceAssetServiceFromEnvironment(ctx, model.DB)
			if err != nil {
				return nil // Storage configuration may intentionally leave invoices disabled.
			}
			_, cleanupErr := assets.CleanupExpired(ctx, 100)
			return cleanupErr
		},
	})
}
