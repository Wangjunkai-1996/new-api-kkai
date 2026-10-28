package model

import (
	"errors"
	"gorm.io/gorm"
)

type ShopFulfillInput struct {
	Command           ShopCommand
	OrderPublicID     string
	DeliveryPublicID  string
	Secret            ShopSecret
	Instructions      string
	Reason            string
	AftersalePublicID string
	OrderVersion      int64
	StockUnitID       int64
	StockVersion      int64
}
type ShopRevealInput struct {
	Command          ShopCommand
	OrderPublicID    string
	DeliveryPublicID string
	Intent           string
	Reason           string
}
type ShopRevealGrant struct {
	Order    ShopOrder
	Delivery ShopDelivery
	Stock    *ShopStockUnit
	Secret   ShopSecret
}

// A candidate is a read-only hint for local decryption, never a reservation.
// FulfillShopOrder checks its exact version and ownership again under locks.
func GetShopFulfillmentCandidate(db *gorm.DB, orderPublicID string) (*ShopStockUnit, error) {
	var order ShopOrder
	if err := db.Where("public_id = ? AND tenant_id = ?", orderPublicID, PlatformTenantID).First(&order).Error; err != nil {
		return nil, shopNotFound(err)
	}
	if order.PaymentState != "paid" || order.RiskHold {
		return nil, ErrShopAction
	}
	var stock ShopStockUnit
	if order.ReservedStockID != nil {
		err := db.Where("id = ? AND state = ? AND reserved_order_id = ?", *order.ReservedStockID, "reserved", order.ID).First(&stock).Error
		if err == nil {
			return &stock, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	err := db.Where("product_id = ? AND state = ?", order.ProductID, "available").Order("id asc").First(&stock).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShopStock
	}
	return &stock, err
}
func shopDeliveryAllowed(tx *gorm.DB, order *ShopOrder) error {
	if order.PaymentState != "paid" || order.EffectiveReceiptID == nil || order.RiskHold {
		return ErrShopRevealBlocked
	}
	var receipt ShopPaymentReceipt
	if err := lockForUpdate(tx).First(&receipt, *order.EffectiveReceiptID).Error; err != nil {
		return err
	}
	if receipt.ProcessingState != "matched" || receipt.OrderID == nil || *receipt.OrderID != order.ID {
		return ErrShopRevealBlocked
	}
	var n int64
	if err := tx.Model(&ShopRefundCase{}).Where("receipt_id = ? AND state <> ?", receipt.ID, "cancelled").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrShopRevealBlocked
	}
	if err := tx.Model(&ShopPaymentAdjustment{}).Where("receipt_id = ? AND state IN ?", receipt.ID, []string{"pending", "succeeded", "disputed"}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrShopRevealBlocked
	}
	return nil
}
func FulfillShopOrder(db *gorm.DB, in ShopFulfillInput) (*ShopDelivery, error) {
	c := in.Command
	now := shopNow(c.Now)
	scope, target := "delivery", in.OrderPublicID
	replacement := in.AftersalePublicID != ""
	if replacement {
		scope, target = "replacement", in.AftersalePublicID
	}
	var result ShopDelivery
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if c.ActorID > 0 {
			if err := shopAuthorize(tx, c, true); err != nil {
				return err
			}
			k, err := shopIdempotency(tx, c, scope, target)
			if err != nil {
				return err
			}
			if k != nil {
				return tx.Where("public_id = ?", k.ResourcePublicID).First(&result).Error
			}
			if !shopRequired(in.Reason, 2000) {
				return ErrShopInvalid
			}
		} else if replacement {
			return ErrShopAction
		}
		product, order, err := shopLockOrder(tx, in.OrderPublicID, 0, false, now)
		if err != nil {
			return err
		}
		if !replacement && order.CurrentDeliveryID != nil {
			if err = tx.First(&result, *order.CurrentDeliveryID).Error; err != nil {
				return err
			}
			if c.ActorID > 0 {
				return shopRemember(tx, c, scope, target, "delivery", result.PublicID, nil)
			}
			return nil
		}
		if c.ActorID > 0 && !replacement {
			if err = shopVersion(order.Version, c.ExpectedVersion); err != nil {
				return err
			}
		}
		if err = shopDeliveryAllowed(tx, order); err != nil {
			return err
		}
		snapshot, err := shopSnapshot(order)
		if err != nil {
			return err
		}
		if order.InputCompleteAt == 0 || order.DeliveryState == "awaiting_input" {
			return ErrShopAction
		}
		var aftersale *ShopAftersale
		if replacement {
			if err = shopVersion(order.Version, in.OrderVersion); err != nil {
				return err
			}
			var row ShopAftersale
			if err = lockForUpdate(tx).Where("public_id = ? AND order_id = ?", in.AftersalePublicID, order.ID).First(&row).Error; err != nil {
				return shopNotFound(err)
			}
			if err = shopVersion(row.Version, c.ExpectedVersion); err != nil {
				return err
			}
			if order.ActiveAftersaleID == nil || *order.ActiveAftersaleID != row.ID || row.State != "processing" || order.CurrentDeliveryID == nil {
				return ErrShopAction
			}
			aftersale = &row
		}
		if !shopRequired(in.DeliveryPublicID, 64) || !shopBoundedText(in.Instructions, 8000) {
			return ErrShopInvalid
		}
		expires, err := shopTimeAdd(now, snapshot.Terms.ViewPolicy.DurationSeconds)
		if err != nil {
			return err
		}
		retention, err := shopTimeAdd(now, snapshot.Retention.SecretAfterDeliverySeconds)
		if err != nil {
			return err
		}
		result = ShopDelivery{ShopRecord: ShopRecord{PublicID: in.DeliveryPublicID, Version: 1, CreatedAt: now, UpdatedAt: now}, OrderID: order.ID, VersionNo: 1, Instructions: in.Instructions, DeliveredAt: now, ViewExpiresAt: expires, CreatorID: c.ActorID, SecretRetentionUntil: max(expires, retention)}
		if aftersale != nil {
			result.AftersaleID = &aftersale.ID
		}
		if snapshot.DeliveryType == "automatic" {
			if in.StockUnitID <= 0 || in.StockVersion <= 0 {
				return ErrShopSecret
			}
			var stock ShopStockUnit
			if err = lockForUpdate(tx).Where("id = ? AND product_id = ?", in.StockUnitID, product.ID).First(&stock).Error; err != nil {
				return shopNotFound(err)
			}
			if stock.Version != in.StockVersion {
				return ErrShopVersion
			}
			reserved := stock.State == "reserved" && stock.ReservedOrderID != nil && *stock.ReservedOrderID == order.ID
			if !reserved && stock.State != "available" || stock.SoldOrderID != nil {
				return ErrShopStock
			}
			if stock.SecretCiphertext == "" || stock.SecretKeyVersion == "" || stock.SecretPurgedAt > 0 {
				return ErrShopSecret
			}
			if order.ReservedStockID != nil && *order.ReservedStockID != stock.ID {
				return ErrShopStock
			}
			if err = shopCAS(tx, &ShopStockUnit{}, stock.ID, stock.Version, now, map[string]any{"state": "sold", "sold_order_id": order.ID, "reserved_order_id": nil, "reservation_expires_at": 0, "secret_retention_until": result.SecretRetentionUntil}); err != nil {
				return err
			}
			result.StockUnitID = &stock.ID
		} else {
			if c.ActorID <= 0 || in.Secret.Ciphertext == "" || in.Secret.KeyVersion == "" || c.HashKeyVersion == "" {
				return ErrShopSecret
			}
			result.ManualCiphertext = in.Secret.Ciphertext
			result.SecretKeyVersion = in.Secret.KeyVersion
		}
		var previous ShopDelivery
		if order.CurrentDeliveryID != nil {
			if err = lockForUpdate(tx).Where("id = ? AND order_id = ?", *order.CurrentDeliveryID, order.ID).First(&previous).Error; err != nil {
				return err
			}
			result.VersionNo = previous.VersionNo + 1
		}
		if err = tx.Create(&result).Error; err != nil {
			return err
		}
		if previous.ID > 0 {
			if err = shopCAS(tx, &ShopDelivery{}, previous.ID, previous.Version, now, map[string]any{"reveal_revoked_at": now, "reveal_revoked_reason": "replaced"}); err != nil {
				return err
			}
		}
		if order.ManualCapacityHeld {
			q := tx.Model(&ShopProduct{}).Where("id = ? AND manual_used > 0", product.ID).Updates(map[string]any{"manual_used": gorm.Expr("manual_used - 1"), "updated_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrShopCapacity
			}
		}
		order.CurrentDeliveryID = &result.ID
		fields, err := shopPolicyDates(order, snapshot, now)
		if err != nil {
			return err
		}
		fields["current_delivery_id"] = result.ID
		fields["delivery_state"] = "delivered"
		fields["reserved_stock_id"] = nil
		fields["manual_capacity_held"] = false
		fields["exception_code"] = ""
		fields["next_action"] = ""
		if aftersale != nil {
			if err = shopCAS(tx, &ShopAftersale{}, aftersale.ID, aftersale.Version, now, map[string]any{"state": "replaced", "closed_at": now, "resolution_code": "replaced"}); err != nil {
				return err
			}
			fields["active_aftersale_id"] = nil
		}
		if err = shopUpdateOrder(tx, order, now, fields); err != nil {
			return err
		}
		if err = shopEvent(tx, c, &order.ID, nil, "delivered", "public", "", nil); err != nil {
			return err
		}
		if err = shopEnqueue(tx, ShopTopicNotification, shopEventKey("delivery-notice", result.ID, 1), order.PublicID, map[string]any{"schema_version": 1, "order_id": order.ID, "delivery_id": result.ID}, now); err != nil {
			return err
		}
		if c.ActorID > 0 {
			if err = shopEvent(tx, c, &order.ID, nil, scope+"_authorized", "internal", in.Reason, nil); err != nil {
				return err
			}
			return shopRemember(tx, c, scope, target, "delivery", result.PublicID, nil)
		}
		return nil
	})
	return &result, err
}
func MarkShopDeliveryException(db *gorm.DB, publicID, code string, now int64) error {
	if !shopRequired(code, 64) {
		return ErrShopInvalid
	}
	return shopTransaction(db, func(tx *gorm.DB) error {
		_, order, err := shopLockOrder(tx, publicID, 0, false, now)
		if err != nil {
			return err
		}
		if order.CurrentDeliveryID != nil || order.PaymentState != "paid" {
			return nil
		}
		if order.DeliveryState == "exception" && order.ExceptionCode == code {
			return nil
		}
		if err = shopUpdateOrder(tx, order, now, map[string]any{"delivery_state": "exception", "exception_code": code, "next_action": "operator_review"}); err != nil {
			return err
		}
		return shopEvent(tx, ShopCommand{Now: now}, &order.ID, nil, "delivery_exception", "public", code, nil)
	})
}
func RevealShopDelivery(db *gorm.DB, in ShopRevealInput) (*ShopRevealGrant, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopRevealGrant
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, c.Admin); err != nil {
			return err
		}
		owner := c.ActorID
		if c.Admin {
			owner = 0
			if !shopRequired(in.Reason, 2000) {
				return ErrShopInvalid
			}
		}
		_, order, err := shopLockOrder(tx, in.OrderPublicID, owner, !c.Admin, now)
		if err != nil {
			return err
		}
		if c.Admin {
			if err = shopVersion(order.Version, c.ExpectedVersion); err != nil {
				return err
			}
		}
		if in.Intent != "view" && in.Intent != "copy" {
			return ErrShopInvalid
		}
		if err = shopDeliveryAllowed(tx, order); err != nil {
			return err
		}
		var delivery ShopDelivery
		if err = tx.Where("public_id = ? AND order_id = ?", in.DeliveryPublicID, order.ID).First(&delivery).Error; err != nil {
			return shopNotFound(err)
		}
		if order.CurrentDeliveryID == nil || *order.CurrentDeliveryID != delivery.ID || delivery.RevealRevokedAt > 0 {
			return ErrShopRevealBlocked
		}
		if now >= delivery.ViewExpiresAt {
			return ErrShopRevealExpired
		}
		if delivery.SecretPurgedAt > 0 {
			return ErrShopSecret
		}
		if delivery.StockUnitID != nil {
			var stock ShopStockUnit
			if err = lockForUpdate(tx).First(&stock, *delivery.StockUnitID).Error; err != nil {
				return err
			}
			if stock.State != "sold" || stock.SoldOrderID == nil || *stock.SoldOrderID != order.ID {
				return ErrShopRevealBlocked
			}
			if stock.SecretCiphertext == "" || stock.SecretPurgedAt > 0 {
				return ErrShopSecret
			}
			result.Stock = &stock
			result.Secret = ShopSecret{Ciphertext: stock.SecretCiphertext, KeyVersion: stock.SecretKeyVersion}
		} else {
			result.Secret = ShopSecret{Ciphertext: delivery.ManualCiphertext, KeyVersion: delivery.SecretKeyVersion}
		}
		if result.Secret.Ciphertext == "" || result.Secret.KeyVersion == "" {
			return ErrShopSecret
		}
		if err = lockForUpdate(tx).First(&delivery, delivery.ID).Error; err != nil {
			return err
		}
		if delivery.FirstRevealedAt == 0 {
			if err = shopCAS(tx, &ShopDelivery{}, delivery.ID, delivery.Version, now, map[string]any{"first_revealed_at": now}); err != nil {
				return err
			}
			delivery.FirstRevealedAt = now
			delivery.Version++
		}
		if err = shopEvent(tx, c, &order.ID, nil, "delivery_"+in.Intent, "internal", in.Reason, nil); err != nil {
			return err
		}
		result.Order = *order
		result.Delivery = delivery
		return nil
	})
	return &result, err
}
func QuarantineShopInventory(db *gorm.DB, c ShopCommand, publicID, reason string) (*ShopStockUnit, error) {
	var result ShopStockUnit
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "inventory-quarantine", publicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ?", publicID).First(&result).Error
		}
		if !shopRequired(reason, 2000) {
			return ErrShopInvalid
		}
		if err = tx.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&result).Error; err != nil {
			return shopNotFound(err)
		}
		orderID := result.ReservedOrderID
		if orderID == nil {
			orderID = result.SoldOrderID
		}
		var order *ShopOrder
		if orderID != nil {
			var ref ShopOrder
			if err = tx.First(&ref, *orderID).Error; err != nil {
				return err
			}
			_, order, err = shopLockOrder(tx, ref.PublicID, 0, false, shopNow(c.Now))
			if err != nil {
				return err
			}
		} else {
			var p ShopProduct
			if err = lockForUpdate(tx).First(&p, result.ProductID).Error; err != nil {
				return err
			}
		}
		if err = lockForUpdate(tx).First(&result, result.ID).Error; err != nil {
			return err
		}
		if err = shopVersion(result.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if err = shopCAS(tx, &ShopStockUnit{}, result.ID, result.Version, shopNow(c.Now), map[string]any{"state": "quarantined", "quarantine_reason": reason, "reserved_order_id": nil, "reservation_expires_at": 0}); err != nil {
			return err
		}
		if order != nil {
			fields := map[string]any{"exception_code": "INVENTORY_QUARANTINED", "next_action": "operator_review"}
			if order.ReservedStockID != nil && *order.ReservedStockID == result.ID {
				fields["reserved_stock_id"] = nil
			}
			if order.CurrentDeliveryID == nil {
				fields["delivery_state"] = "exception"
			}
			if err = shopUpdateOrder(tx, order, shopNow(c.Now), fields); err != nil {
				return err
			}
		}
		if err = shopEvent(tx, c, orderID, nil, "inventory_quarantined", "internal", reason, nil); err != nil {
			return err
		}
		if err = shopRemember(tx, c, "inventory-quarantine", publicID, "stock", publicID, nil); err != nil {
			return err
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}
func RevealShopInventory(db *gorm.DB, c ShopCommand, publicID, reason string) (*ShopStockUnit, error) {
	var result ShopStockUnit
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		if !shopRequired(reason, 2000) {
			return ErrShopInvalid
		}
		if err := tx.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&result).Error; err != nil {
			return shopNotFound(err)
		}
		if result.ReservedOrderID != nil {
			return ErrShopRevealBlocked
		}
		var orderID *int64
		if result.SoldOrderID != nil {
			var ref ShopOrder
			if err := tx.First(&ref, *result.SoldOrderID).Error; err != nil {
				return err
			}
			_, order, err := shopLockOrder(tx, ref.PublicID, 0, false, shopNow(c.Now))
			if err != nil {
				return err
			}
			if err = shopDeliveryAllowed(tx, order); err != nil {
				return err
			}
			if order.CurrentDeliveryID == nil {
				return ErrShopRevealBlocked
			}
			var delivery ShopDelivery
			if err = tx.Where("id = ? AND stock_unit_id = ?", *order.CurrentDeliveryID, result.ID).First(&delivery).Error; err != nil {
				return ErrShopRevealBlocked
			}
			if delivery.RevealRevokedAt > 0 || shopNow(c.Now) >= delivery.ViewExpiresAt {
				return ErrShopRevealExpired
			}
			orderID = &order.ID
		} else {
			var product ShopProduct
			if err := lockForUpdate(tx).First(&product, result.ProductID).Error; err != nil {
				return err
			}
		}
		if err := lockForUpdate(tx).First(&result, result.ID).Error; err != nil {
			return err
		}
		if err := shopVersion(result.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if result.State == "reserved" || result.SoldOrderID != nil && result.State != "sold" {
			return ErrShopRevealBlocked
		}
		if result.SecretCiphertext == "" || result.SecretPurgedAt > 0 {
			return ErrShopSecret
		}
		fields := map[string]any{"revealed_at": shopNow(c.Now)}
		if result.State == "available" {
			fields["state"] = "quarantined"
			fields["quarantine_reason"] = "administrator_revealed"
		}
		if err := shopCAS(tx, &ShopStockUnit{}, result.ID, result.Version, shopNow(c.Now), fields); err != nil {
			return err
		}
		if err := shopEvent(tx, c, orderID, nil, "inventory_revealed", "internal", reason, nil); err != nil {
			return err
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}
func ReadShopBuyerInput(db *gorm.DB, c ShopCommand, publicID string) (*ShopOrder, error) {
	var result *ShopOrder
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, c.Admin); err != nil {
			return err
		}
		owner := c.ActorID
		if c.Admin {
			owner = 0
		}
		_, order, err := shopLockOrder(tx, publicID, owner, !c.Admin, shopNow(c.Now))
		if err != nil {
			return err
		}
		if order.BuyerInputCiphertext == "" || order.BuyerInputPurgedAt > 0 {
			return ErrShopSecret
		}
		if err = shopEvent(tx, c, &order.ID, nil, "buyer_input_read", "internal", "", nil); err != nil {
			return err
		}
		result = order
		return nil
	})
	return result, err
}
