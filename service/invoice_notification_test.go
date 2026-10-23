package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceProgressEmailContainsNoInvoiceDataAndRetriesSafely(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceRequest{}, &model.InvoiceEvent{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	t.Setenv("KKAI_INVOICE_KEY_ID", "test")
	keys, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	t.Setenv("KKAI_INVOICE_KEYS", string(keys))
	profile := InvoiceProfileData{Kind: "company", Title: "Private customer title", TaxID: "PRIVATE-TAX-NUMBER", Email: "invoice@example.com"}
	cipher, err := invoiceEncrypt(invoiceAAD(0, 7, "inv-private", "profile"), profile)
	require.NoError(t, err)
	request := model.InvoiceRequest{PublicID: "inv-private", UserID: 7, ProfileCipher: cipher}
	require.NoError(t, db.Create(&request).Error)
	update := model.InvoiceEvent{PublicID: "event-private", RequestID: request.ID, UserID: 7, ActorID: 1, ActorAdmin: true, Kind: "deliver", Status: model.InvoiceDelivered}
	require.NoError(t, db.Create(&update).Error)
	payload, err := common.Marshal(map[string]int64{"event_id": update.ID})
	require.NoError(t, err)
	outbox := model.KKAIOutboxEvent{Payload: string(payload)}
	calls := 0
	handler := InvoiceNotificationHandler{DB: db, Send: func(subject, recipient, content string) error {
		calls++
		assert.Equal(t, profile.Email, recipient)
		assert.Contains(t, content, "inv-private")
		assert.NotContains(t, content, profile.Title)
		assert.NotContains(t, content, profile.TaxID)
		assert.NotContains(t, content, profile.Email)
		assert.NotContains(t, content, "storage")
		return nil
	}}
	require.NoError(t, handler.Handle(context.Background(), outbox))
	assert.Equal(t, 1, calls)
	handler.Send = func(string, string, string) error { return errors.New("SMTP failure for private@example.com") }
	err = handler.Handle(context.Background(), outbox)
	require.Error(t, err)
	assert.EqualError(t, err, "invoice email delivery failed")
	// User-authored activity belongs in the admin queue, not a redundant mail to
	// the customer or a disclosure of their invoice fields to administrators.
	require.NoError(t, db.Model(&update).Update("actor_admin", false).Error)
	require.NoError(t, handler.Handle(context.Background(), outbox))
	assert.Equal(t, 1, calls)
}

func TestInvoiceMailStatusTracksLatestProgressMailAndDistinguishesDeadLetters(t *testing.T) {
	s, user := invoiceFixture(t)
	previousSMTP := common.SMTPServer
	common.SMTPServer = "smtp.example.test"
	t.Cleanup(func() { common.SMTPServer = previousSMTP })
	ctx := context.Background()
	r, err := s.Create(ctx, user, "mail-status-create", invoiceCreateInput())
	require.NoError(t, err)
	admin := InvoiceScope{TenantID: 0, UserID: 2, Admin: true}
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "mail-status-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	assert.Equal(t, "queued", r.NotificationDeliveryState)
	var progress model.InvoiceEvent
	require.NoError(t, s.DB.Where("actor_admin = ?", true).First(&progress).Error)
	query := s.DB.Model(&model.KKAIOutboxEvent{}).Where("event_key = ?", "invoice:event:"+progress.PublicID)
	require.NoError(t, query.Updates(map[string]any{"attempts": 2}).Error)
	var row model.InvoiceRequest
	require.NoError(t, s.DB.Where("public_id = ?", r.PublicID).First(&row).Error)
	// A newer user-authored event never sends progress email or masks its state.
	require.NoError(t, s.event(s.DB, user, row, "message", "", 0))
	r, err = s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	assert.Equal(t, "retrying", r.NotificationDeliveryState)
	require.NoError(t, query.Update("status", model.KKAIOutboxStatusDead).Error)
	r, err = s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	assert.Equal(t, "failed", r.NotificationDeliveryState)
	common.SMTPServer = ""
	r, err = s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	assert.Equal(t, "not_configured", r.NotificationDeliveryState)
}
