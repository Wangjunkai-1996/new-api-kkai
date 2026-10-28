package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PurgeShopOrderSecrets never deletes financial identity, fingerprints, audit or
// replay records. Every deadline and responsibility is rechecked under locks.
func PurgeShopOrderSecrets(db *gorm.DB, orderPublicID string, now int64) error {
	now = shopNow(now)
	return shopTransaction(db, func(tx *gorm.DB) error {
		_, order, err := shopLockOrder(tx, orderPublicID, 0, false, now)
		if err != nil {
			return err
		}
		pending, err := shopOrderHasResponsibilities(tx, order, true)
		if err != nil {
			return err
		}
		if pending || order.RetentionHold {
			return ErrShopResponsibilities
		}
		var deliveries []ShopDelivery
		if err = tx.Where("order_id = ?", order.ID).Order("id asc").Find(&deliveries).Error; err != nil {
			return err
		}
		for _, delivery := range deliveries {
			if delivery.RetentionHold {
				return ErrShopResponsibilities
			}
		}
		var stocks []ShopStockUnit
		if err = lockForUpdate(tx).Where("sold_order_id = ?", order.ID).Order("id asc").Find(&stocks).Error; err != nil {
			return err
		}
		for _, stock := range stocks {
			if stock.RetentionHold {
				return ErrShopResponsibilities
			}
		}
		if err = lockForUpdate(tx).Where("order_id = ?", order.ID).Order("id asc").Find(&deliveries).Error; err != nil {
			return err
		}
		purged := false
		for _, delivery := range deliveries {
			if delivery.SecretPurgedAt != 0 || delivery.SecretRetentionUntil <= 0 || now < max(delivery.SecretRetentionUntil, delivery.ViewExpiresAt) || delivery.RetentionHold {
				continue
			}
			if err = shopCAS(tx, &ShopDelivery{}, delivery.ID, delivery.Version, now, map[string]any{"manual_ciphertext": "", "secret_key_version": "", "secret_purged_at": now}); err != nil {
				return err
			}
			purged = true
		}
		for _, stock := range stocks {
			if stock.State != "sold" || stock.SecretPurgedAt != 0 || stock.SecretRetentionUntil <= 0 || now < stock.SecretRetentionUntil {
				continue
			}
			canPurge := true
			for _, delivery := range deliveries {
				if delivery.StockUnitID != nil && *delivery.StockUnitID == stock.ID && (delivery.RetentionHold || now < delivery.ViewExpiresAt || now < delivery.SecretRetentionUntil) {
					canPurge = false
				}
			}
			if !canPurge {
				continue
			}
			if err = shopCAS(tx, &ShopStockUnit{}, stock.ID, stock.Version, now, map[string]any{"secret_ciphertext": "", "secret_key_version": "", "secret_purged_at": now}); err != nil {
				return err
			}
			purged = true
		}
		fields := map[string]any{}
		deadline := order.BuyerInputRetentionUntil
		if deadline == 0 && order.BuyerInputPurgedAt == 0 && order.BuyerInputCiphertext != "" {
			snapshot, err := shopSnapshot(order)
			if err != nil {
				return err
			}
			settledAt := order.ClosedAt
			for _, delivery := range deliveries {
				settledAt = max(settledAt, delivery.DeliveredAt)
			}
			var refunds []ShopRefundCase
			if err = tx.Where("order_id = ?", order.ID).Find(&refunds).Error; err != nil {
				return err
			}
			for _, refund := range refunds {
				settledAt = max(settledAt, refund.UpdatedAt)
			}
			var adjustments []ShopPaymentAdjustment
			if err = tx.Where("receipt_id IN (?) AND state = ?", tx.Model(&ShopPaymentReceipt{}).Select("id").Where("order_id = ?", order.ID), "succeeded").Find(&adjustments).Error; err != nil {
				return err
			}
			for _, adjustment := range adjustments {
				settledAt = max(settledAt, adjustment.VerifiedAt, adjustment.UpdatedAt)
			}
			if settledAt > 0 {
				deadline, err = shopTimeAdd(settledAt, snapshot.Retention.BuyerInputAfterClosureSeconds)
				if err != nil {
					return err
				}
				fields["buyer_input_retention_until"] = deadline
			}
		}
		if order.BuyerInputPurgedAt == 0 && deadline > 0 && now >= deadline {
			fields["buyer_input_ciphertext"] = ""
			fields["buyer_input_key_version"] = ""
			fields["buyer_input_purged_at"] = now
			purged = true
		}
		if len(fields) > 0 {
			if err = shopUpdateOrder(tx, order, now, fields); err != nil {
				return err
			}
		}
		if purged {
			return shopEvent(tx, ShopCommand{Now: now}, &order.ID, nil, "secrets_purged", "internal", "Retention policy satisfied", nil)
		}
		return nil
	})
}

func SetShopRetentionHold(db *gorm.DB, c ShopCommand, orderPublicID string, hold bool, reason string) (*ShopOrder, error) {
	var result *ShopOrder
	if !shopRequired(reason, 200) {
		return nil, ErrShopInvalid
	}
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "retention-hold", orderPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			result, err = GetShopOrder(tx, ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: true}, orderPublicID)
			return err
		}
		_, order, err := shopLockOrder(tx, orderPublicID, 0, false, shopNow(c.Now))
		if err != nil {
			return err
		}
		if err = shopVersion(order.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if err = shopUpdateOrder(tx, order, shopNow(c.Now), map[string]any{"retention_hold": hold, "retention_hold_reason": reason}); err != nil {
			return err
		}
		kind := "retention_hold_set"
		if !hold {
			kind = "retention_hold_released"
		}
		if err = shopEvent(tx, c, &order.ID, nil, kind, "internal", reason, nil); err != nil {
			return err
		}
		result = order
		return shopRemember(tx, c, "retention-hold", orderPublicID, "order", order.PublicID, nil)
	})
	return result, err
}

// RecordShopReminder deduplicates a responsibility episode and only emits the
// notification outbox event after rechecking that the threshold is still met.
func RecordShopReminder(db *gorm.DB, orderPublicID, kind string, now int64) (bool, error) {
	if !slices.Contains([]string{"payment_unknown", "delivery_exception", "refund_verification"}, kind) {
		return false, ErrShopInvalid
	}
	now = shopNow(now)
	recorded := false
	err := shopTransaction(db, func(tx *gorm.DB) error {
		recorded = false
		_, order, err := shopLockOrder(tx, orderPublicID, 0, false, now)
		if err != nil {
			return err
		}
		settings, err := GetShopSettings(tx)
		if err != nil {
			return err
		}
		var policy ShopReminderPolicy
		if common.UnmarshalJsonStr(settings.ReminderPolicyJSON, &policy) != nil {
			return ErrShopInvalid
		}
		var seconds, id, changed int64
		switch kind {
		case "payment_unknown":
			seconds = policy.PaymentUnknownSeconds
			var attempt ShopPaymentAttempt
			err = tx.Where("order_id = ? AND create_state = ?", order.ID, "unknown").Order("id asc").First(&attempt).Error
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			if err != nil {
				return err
			}
			id = attempt.ID
			// Reconciliation claims update UpdatedAt on every poll. The frozen
			// attempt creation time bounds how long its uncertainty can persist.
			changed = attempt.CreatedAt
		case "delivery_exception":
			seconds = policy.DeliveryExceptionSeconds
			if order.PaymentState != "paid" || order.DeliveryState != "exception" {
				return nil
			}
			id = order.ID
			changed = order.UpdatedAt
		case "refund_verification":
			seconds = policy.RefundVerificationSeconds
			var refund ShopRefundCase
			err = tx.Where("order_id = ? AND state IN ?", order.ID, []string{"processing", "unknown"}).Order("id asc").First(&refund).Error
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			if err != nil {
				return err
			}
			id = refund.ID
			changed = refund.UpdatedAt
		}
		if seconds <= 0 || changed <= 0 || now < changed || now-changed < seconds {
			return nil
		}
		key := shopDigest([]any{"reminder", kind, order.ID, id, changed})
		rec, err := shopRecord("se_", now)
		if err != nil {
			return err
		}
		event := ShopEvent{ShopRecord: rec, TenantID: PlatformTenantID, OrderID: &order.ID, Kind: "reminder_" + kind, ActorKind: "system", Visibility: "internal", OperationKey: &key}
		saved := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if saved.Error != nil {
			return saved.Error
		}
		if saved.RowsAffected == 0 {
			return nil
		}
		if err = shopEnqueue(tx, ShopTopicNotification, "shop:reminder:"+key, order.PublicID, map[string]any{"schema_version": 1, "order_id": order.ID, "reminder_kind": kind}, now); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return recorded, err
}
