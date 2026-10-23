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

func TestInvoiceNotificationsKeepReaderIsolationAndNewProgressUnread(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{UserID: 2, Admin: true}
	otherAdmin := InvoiceScope{UserID: 3, Admin: true}
	require.NoError(t, s.DB.Exec("INSERT INTO users (id,tenant_id,username,email) VALUES (3,0,'other-admin','other-admin@example.com')").Error)
	r, err := s.Create(ctx, user, "notify-reader-create", invoiceCreateInput())
	require.NoError(t, err)
	require.NoError(t, s.ReadNotifications(ctx, admin, ""))
	require.NoError(t, s.ReadNotifications(ctx, admin, ""), "read-all remains safe when there are no unread events")
	other, err := s.Notifications(ctx, otherAdmin, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, other.UnreadCount, "another administrator keeps their own unread state")
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "notify-reader-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	progress, err := s.Notifications(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	require.Len(t, progress.Items, 1)
	assert.Equal(t, r.PublicID, progress.Items[0].RequestPublicID)
	assert.Empty(t, progress.Items[0].Description)
	assert.EqualValues(t, 1, progress.UnreadCount)
	otherCustomer, err := s.Notifications(ctx, InvoiceScope{UserID: 3}, InvoiceFilter{})
	require.NoError(t, err)
	assert.Empty(t, otherCustomer.Items)
	require.Error(t, s.ReadNotifications(ctx, InvoiceScope{UserID: 3}, progress.Items[0].ID), "another customer cannot mark this event read")
	require.NoError(t, s.ReadNotifications(ctx, user, progress.Items[0].ID))
	read, err := s.Notifications(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	require.Len(t, read.Items, 1)
	assert.NotEmpty(t, read.Items[0].ReadAt)
	require.NoError(t, s.ReadNotifications(ctx, user, progress.Items[0].ID), "reading an already-read event is idempotent")
	repeated, err := s.Notifications(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.Equal(t, read.Items[0].ReadAt, repeated.Items[0].ReadAt)
	r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "notify-reader-hold", InvoiceActionInput{Version: r.Version, Reason: "Check payment documentation"})
	require.NoError(t, err)
	progress, err = s.Notifications(ctx, user, InvoiceFilter{PageSize: 1})
	require.NoError(t, err)
	require.Len(t, progress.Items, 1)
	assert.Equal(t, "hold", progress.Items[0].Kind)
	assert.Empty(t, progress.Items[0].Description, "notifications must not reveal private financial explanations")
	assert.Empty(t, progress.Items[0].ReadAt)
	assert.EqualValues(t, 2, progress.Total)
	assert.EqualValues(t, 1, progress.UnreadCount, "new progress remains unread after an earlier read operation")
	require.NoError(t, s.ReadNotifications(ctx, user, ""))
	progress, err = s.Notifications(ctx, user, InvoiceFilter{UnreadOnly: true})
	require.NoError(t, err)
	assert.Empty(t, progress.Items)
	assert.Zero(t, progress.UnreadCount)
	other, err = s.Notifications(ctx, otherAdmin, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, other.UnreadCount)
}
