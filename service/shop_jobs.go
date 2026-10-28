package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// Shop recovery uses the existing leader and durable-outbox lifecycle. Sales
// being disabled never stops recovery of money or delivery already owed.
func RegisterShopBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	cursor := &shopMaintenanceCursor{}
	return registry.Register(BackgroundJob{
		Name: "shop-maintenance", Interval: 30 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if CheckShopRuntimeSchema(ctx, model.DB) != nil {
				return nil // An uninstalled, disabled shop is an intentional state.
			}
			db, err := shopWorkerDatabase(ctx, model.DB, workerID, nil, 0)
			if err != nil {
				return err
			}
			return runShopMaintenance(db.Statement.Context, db, time.Now().Unix(), cursor)
		},
	})
}

func RegisterShopOutbox(processor *KKAIOutboxProcessor, db *gorm.DB, workerID string) error {
	if processor == nil || db == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrKKAIOutboxInvalidConfiguration
	}
	handler := ShopOutboxHandler{DB: db, WorkerID: workerID, LockTimeout: processor.lockTimeout}
	for _, topic := range []string{model.ShopTopicReceipt, model.ShopTopicFulfillment, model.ShopTopicNotification} {
		if err := processor.Register(topic, handler.Handle); err != nil {
			return err
		}
	}
	return nil
}

type ShopOutboxHandler struct {
	DB          *gorm.DB
	WorkerID    string
	LockTimeout time.Duration
	Send        func(subject, recipient, content string) error
}

// The registry's cancellation stops work promptly; the model fence also locks
// ownership in every business transaction so an expired worker cannot commit.
func shopWorkerDatabase(ctx context.Context, db *gorm.DB, workerID string, event *model.KKAIOutboxEvent, lockTimeout time.Duration) (*gorm.DB, error) {
	if !leaderLeaseNamePattern.MatchString(workerID) {
		return nil, model.ErrShopWorkerFenceLost
	}
	lease, err := NewKKAILeaderLeaseStore(db, "newapi-background-writer").Current(ctx)
	if err != nil {
		return nil, err
	}
	if lease.Holder != workerID || lease.Fence <= 0 || lease.LeaseUntil <= time.Now().Unix() {
		return nil, model.ErrShopWorkerFenceLost
	}
	fence := model.ShopWorkerFence{LeaderName: lease.LeaseName, LeaderHolder: lease.Holder, LeaderFence: lease.Fence}
	if event != nil {
		fence.EventID, fence.LockedBy, fence.LockTimeout = event.ID, event.LockedBy, lockTimeout
		if fence.EventID <= 0 || fence.LockedBy == "" || fence.LockTimeout <= 0 {
			return nil, model.ErrShopWorkerFenceLost
		}
	}
	return model.WithShopWorkerFence(db.WithContext(ctx), fence), nil
}

func (h ShopOutboxHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	if h.DB == nil || len(event.Payload) > 4096 {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid shop outbox reference"))
	}
	if CheckShopRuntimeSchema(ctx, h.DB) != nil {
		return DeferKKAIOutboxUntil(time.Now().Add(time.Minute), fmt.Errorf("shop schema unavailable"))
	}
	db, err := shopWorkerDatabase(ctx, h.DB, h.WorkerID, &event, h.LockTimeout)
	if err != nil {
		return err
	}
	ctx = db.Statement.Context
	switch event.Topic {
	case model.ShopTopicReceipt:
		var payload struct {
			SchemaVersion int   `json:"schema_version"`
			ReceiptID     int64 `json:"receipt_id"`
		}
		if common.UnmarshalStrict([]byte(event.Payload), &payload) != nil || payload.SchemaVersion != 1 || payload.ReceiptID <= 0 {
			return PermanentKKAIOutboxError(fmt.Errorf("invalid shop receipt reference"))
		}
		var receipt model.ShopPaymentReceipt
		if err := db.Where("id = ? AND public_id = ?", payload.ReceiptID, event.AggregateID).First(&receipt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return PermanentKKAIOutboxError(fmt.Errorf("shop receipt reference mismatch"))
			}
			return err
		}
		_, err := model.MatchShopPaymentReceipt(db, receipt.ID, time.Now().Unix())
		return err
	case model.ShopTopicFulfillment:
		var payload struct {
			SchemaVersion int    `json:"schema_version"`
			OrderID       int64  `json:"order_id"`
			OrderPublicID string `json:"order_public_id"`
		}
		if common.UnmarshalStrict([]byte(event.Payload), &payload) != nil || payload.SchemaVersion != 1 || payload.OrderID <= 0 || payload.OrderPublicID != event.AggregateID {
			return PermanentKKAIOutboxError(fmt.Errorf("invalid shop fulfillment reference"))
		}
		var order model.ShopOrder
		if err := db.Where("id = ? AND public_id = ? AND tenant_id = ?", payload.OrderID, payload.OrderPublicID, model.PlatformTenantID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return PermanentKKAIOutboxError(fmt.Errorf("shop order reference mismatch"))
			}
			return err
		}
		err := FulfillShopOrderAutomatically(ctx, db, order.PublicID)
		if errors.Is(err, model.ErrShopStock) || errors.Is(err, model.ErrShopSecret) || errors.Is(err, ErrShopSecretUnavailable) || errors.Is(err, model.ErrShopAction) || errors.Is(err, model.ErrShopRefund) {
			return nil // Stable exceptions remain in the order queue until an explicit retry.
		}
		return err
	case model.ShopTopicNotification:
		var payload struct {
			SchemaVersion int    `json:"schema_version"`
			OrderID       int64  `json:"order_id"`
			DeliveryID    int64  `json:"delivery_id,omitempty"`
			ReminderKind  string `json:"reminder_kind,omitempty"`
		}
		if common.UnmarshalStrict([]byte(event.Payload), &payload) != nil || payload.SchemaVersion != 1 || payload.OrderID <= 0 ||
			(payload.DeliveryID <= 0 && !slices.Contains([]string{"payment_unknown", "delivery_exception", "refund_verification"}, payload.ReminderKind)) ||
			(payload.DeliveryID != 0 && payload.ReminderKind != "") {
			return PermanentKKAIOutboxError(fmt.Errorf("invalid shop notification reference"))
		}
		var order model.ShopOrder
		if err := db.Where("id = ? AND public_id = ? AND tenant_id = ?", payload.OrderID, event.AggregateID, model.PlatformTenantID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return PermanentKKAIOutboxError(fmt.Errorf("shop notification order mismatch"))
			}
			return err
		}
		if payload.ReminderKind != "" {
			// Operational reminders are durable internal events. They are not
			// delivery messages and must not be emailed to the customer.
			return nil
		}
		var delivery model.ShopDelivery
		if err := db.Where("id = ? AND order_id = ?", payload.DeliveryID, order.ID).First(&delivery).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return PermanentKKAIOutboxError(fmt.Errorf("shop notification delivery mismatch"))
			}
			return err
		}
		if common.SMTPServer == "" && h.Send == nil {
			return nil
		}
		var user model.User
		if err := db.Select("id", "email").Where("id = ? AND tenant_id = ?", order.UserID, model.PlatformTenantID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // A retired user still keeps the durable in-site event.
			}
			return err
		}
		if user.Email == "" {
			return nil
		}
		recipient, err := mail.ParseAddress(user.Email)
		if err != nil || recipient.Address != user.Email || strings.ContainsAny(user.Email, "\r\n,;") {
			return PermanentKKAIOutboxError(fmt.Errorf("invalid shop notification recipient"))
		}
		send := h.Send
		if send == nil {
			send = common.SendEmail
		}
		content := "<p>Your KK shop order has an update.</p><p>" + html.EscapeString(order.PublicID) + "</p><p>Sign in to the shop order center to review the current status.</p>"
		if err := ctx.Err(); err != nil {
			return err
		}
		if send("KK shop order update", recipient.Address, content) != nil {
			return fmt.Errorf("shop email delivery failed")
		}
		return nil
	default:
		return PermanentKKAIOutboxError(fmt.Errorf("invalid shop outbox topic"))
	}
}

type shopMaintenanceCursor struct {
	deliveryID  int64
	retentionID int64
	reminderIDs map[string]int64
}

func runShopMaintenance(ctx context.Context, db *gorm.DB, now int64, cursor *shopMaintenanceCursor) error {
	db = db.WithContext(ctx)
	var failures []error
	var attempts []model.ShopPaymentAttempt
	if err := db.Select("public_id").Where("business_kind = ? AND create_state IN ? AND next_check_at > 0 AND next_check_at <= ? AND claim_until <= ?", "shop", []string{"prepared", "creating", "ready", "unknown"}, now, now).Order("next_check_at ASC, id ASC").Limit(10).Find(&attempts).Error; err != nil {
		return err
	}
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := ReconcileShopPayment(ctx, db, attempt.PublicID); err != nil && !errors.Is(err, model.ErrShopPaymentUnresolved) && !errors.Is(err, model.ErrShopVersion) {
			failures = append(failures, err)
		}
	}
	var orders []model.ShopOrder
	if err := db.Select("public_id").Where("tenant_id = ? AND purchase_state = ? AND payment_state <> ? AND reservation_expires_at > 0 AND reservation_expires_at <= ?", model.PlatformTenantID, "open", "paid", now).Order("reservation_expires_at ASC, id ASC").Limit(20).Find(&orders).Error; err != nil {
		return err
	}
	for _, order := range orders {
		if err := model.ExpireShopOrder(db, order.PublicID, now); err != nil && !errors.Is(err, model.ErrShopVersion) {
			failures = append(failures, err)
		}
	}
	var receipts []model.ShopPaymentReceipt
	if err := db.Select("id").Where("processing_state IN ? AND next_retry_at > 0 AND next_retry_at <= ?", []string{"pending", "review"}, now).Order("next_retry_at ASC, id ASC").Limit(20).Find(&receipts).Error; err != nil {
		return err
	}
	for _, receipt := range receipts {
		if _, err := model.MatchShopPaymentReceipt(db, receipt.ID, now); err != nil && !errors.Is(err, model.ErrShopVersion) {
			failures = append(failures, err)
		}
	}
	orders = nil
	if err := db.Select("id", "public_id").Where("tenant_id = ? AND payment_state = ? AND delivery_state = ? AND input_complete_at > 0 AND current_delivery_id IS NULL AND risk_hold = ? AND id > ?", model.PlatformTenantID, "paid", "pending", false, cursor.deliveryID).Order("id ASC").Limit(20).Find(&orders).Error; err != nil {
		return err
	}
	for _, order := range orders {
		cursor.deliveryID = order.ID
		err := FulfillShopOrderAutomatically(ctx, db, order.PublicID)
		if err != nil && !errors.Is(err, model.ErrShopStock) && !errors.Is(err, model.ErrShopSecret) && !errors.Is(err, ErrShopSecretUnavailable) && !errors.Is(err, model.ErrShopAction) && !errors.Is(err, model.ErrShopRefund) {
			failures = append(failures, err)
		}
	}
	if len(orders) < 20 {
		cursor.deliveryID = 0
	}
	orders = nil
	dueDeliveries := db.Model(&model.ShopDelivery{}).Select("order_id").Where("secret_retention_until > 0 AND secret_retention_until <= ? AND secret_purged_at = 0", now)
	if err := db.Select("id", "public_id").Where("tenant_id = ? AND id > ? AND ((buyer_input_retention_until > 0 AND buyer_input_retention_until <= ? AND buyer_input_purged_at = 0) OR (buyer_input_retention_until = 0 AND buyer_input_purged_at = 0 AND buyer_input_ciphertext <> ?) OR id IN (?))", model.PlatformTenantID, cursor.retentionID, now, "", dueDeliveries).Order("id ASC").Limit(20).Find(&orders).Error; err != nil {
		return err
	}
	for _, order := range orders {
		cursor.retentionID = order.ID
		if err := model.PurgeShopOrderSecrets(db, order.PublicID, now); err != nil && !errors.Is(err, model.ErrShopResponsibilities) && !errors.Is(err, model.ErrShopAction) {
			failures = append(failures, err)
		}
	}
	if len(orders) < 20 {
		cursor.retentionID = 0
	}
	if err := remindShopResponsibilities(ctx, db, now, cursor); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func remindShopResponsibilities(ctx context.Context, db *gorm.DB, now int64, cursor *shopMaintenanceCursor) error {
	settings, err := model.GetShopSettings(db)
	if err != nil {
		return err
	}
	var policy model.ShopReminderPolicy
	if common.UnmarshalJsonStr(settings.ReminderPolicyJSON, &policy) != nil {
		return nil
	}
	if cursor.reminderIDs == nil {
		cursor.reminderIDs = make(map[string]int64)
	}
	for _, rule := range []struct {
		kind    string
		seconds int64
	}{{"payment_unknown", policy.PaymentUnknownSeconds}, {"delivery_exception", policy.DeliveryExceptionSeconds}, {"refund_verification", policy.RefundVerificationSeconds}} {
		if rule.seconds <= 0 || now <= rule.seconds {
			continue
		}
		query := db.WithContext(ctx).Model(&model.ShopOrder{}).Select("id", "public_id").Where("tenant_id = ? AND id > ?", model.PlatformTenantID, cursor.reminderIDs[rule.kind])
		switch rule.kind {
		case "payment_unknown":
			query = query.Where("id IN (?)", db.Model(&model.ShopPaymentAttempt{}).Select("order_id").Where("create_state = ? AND created_at <= ?", "unknown", now-rule.seconds))
		case "delivery_exception":
			query = query.Where("payment_state = ? AND delivery_state = ? AND updated_at <= ?", "paid", "exception", now-rule.seconds)
		case "refund_verification":
			query = query.Where("id IN (?)", db.Model(&model.ShopRefundCase{}).Select("order_id").Where("state IN ? AND updated_at <= ?", []string{"processing", "unknown"}, now-rule.seconds))
		}
		var orders []model.ShopOrder
		if err := query.Order("id ASC").Limit(20).Find(&orders).Error; err != nil {
			return err
		}
		for _, order := range orders {
			cursor.reminderIDs[rule.kind] = order.ID
			if _, err := model.RecordShopReminder(db, order.PublicID, rule.kind, now); err != nil {
				return err
			}
		}
		if len(orders) < 20 {
			cursor.reminderIDs[rule.kind] = 0
		}
	}
	return nil
}
