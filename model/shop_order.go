package model

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ShopCreateOrderInput struct {
	Command         ShopCommand
	PublicID        string
	ProductPublicID string
	ProductVersion  int64
	TermsVersion    int64
	AcceptedTerms   bool
	BuyerInput      ShopSecret
	InputKeys       []string
}
type ShopOrderFilter struct {
	Page          int
	Limit         int
	ProductID     *int64
	UserID        int
	PaymentState  string
	DeliveryState string
	PurchaseState string
	StatusGroup   string
	PublicID      string
	From          int64
	To            int64
}
type ShopOrderDetail struct {
	Order                ShopOrder
	Snapshot             ShopOrderSnapshot
	Deliveries           []ShopDelivery
	CurrentDelivery      *ShopDelivery
	Attempts             []ShopPaymentAttempt
	Receipts             []ShopPaymentReceipt
	Aftersales           []ShopAftersale
	Refunds              []ShopRefundCase
	Events               []ShopEvent
	HasPaymentAdjustment bool
}

func shopInputComplete(terms ShopProductTerms, keys []string) (bool, error) {
	if len(keys) > 3 {
		return false, ErrShopInvalid
	}
	allowed := map[string]bool{}
	for _, f := range terms.BuyerInputSchema {
		allowed[f.Key] = true
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if !allowed[k] || seen[k] {
			return false, ErrShopInvalid
		}
		seen[k] = true
	}
	for _, f := range terms.BuyerInputSchema {
		if f.Required && !seen[f.Key] {
			return false, nil
		}
	}
	return true, nil
}
func shopSnapshot(o *ShopOrder) (ShopOrderSnapshot, error) {
	var s ShopOrderSnapshot
	e := common.UnmarshalJsonStr(o.SnapshotJSON, &s)
	return s, e
}
func shopLockOrder(tx *gorm.DB, publicID string, userID int, active bool, now int64) (*ShopProduct, *ShopOrder, error) {
	var o ShopOrder
	if e := tx.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&o).Error; e != nil {
		return nil, nil, shopNotFound(e)
	}
	if userID > 0 && o.UserID != userID {
		return nil, nil, ErrShopNotFound
	}
	if e := shopLockUser(tx, o.UserID, active, now); e != nil {
		return nil, nil, e
	}
	var p ShopProduct
	if e := lockForUpdate(tx).First(&p, o.ProductID).Error; e != nil {
		return nil, nil, e
	}
	if e := lockForUpdate(tx).First(&o, o.ID).Error; e != nil {
		return nil, nil, e
	}
	return &p, &o, nil
}
func shopUpdateOrder(tx *gorm.DB, o *ShopOrder, now int64, fields map[string]any) error {
	if e := shopCAS(tx, &ShopOrder{}, o.ID, o.Version, now, fields); e != nil {
		return e
	}
	return tx.First(o, o.ID).Error
}
func CreateShopOrder(db *gorm.DB, in ShopCreateOrderInput) (*ShopOrder, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopOrder
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, false); e != nil {
			return e
		}
		if e := shopLockUser(tx, c.ActorID, true, now); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, "create-order", "")
		if e != nil {
			return e
		}
		if k != nil {
			return tx.Where("public_id = ? AND user_id = ?", k.ResourcePublicID, c.ActorID).First(&result).Error
		}
		if !in.AcceptedTerms {
			return ErrShopInvalid
		}
		var settings ShopSettings
		if e = lockForUpdate(tx).First(&settings, 1).Error; e != nil {
			return ErrShopPaused
		}
		if !settings.SalesEnabled {
			return ErrShopPaused
		}
		var p ShopProduct
		if e = lockForUpdate(tx).Where("public_id = ? AND tenant_id = ?", in.ProductPublicID, PlatformTenantID).First(&p).Error; e != nil {
			return shopNotFound(e)
		}
		if p.Status != "published" {
			return ErrShopAction
		}
		if p.Version != in.ProductVersion || p.TermsVersion != in.TermsVersion {
			return ErrShopProductChanged
		}
		if e = ValidateShopProduct(&p, true); e != nil {
			return e
		}
		if p.Currency != settings.Currency || p.CurrencyExponent != settings.CurrencyExponent || p.PriceMinor < settings.MinAmountMinor || p.PriceMinor > settings.MaxAmountMinor {
			return ErrShopInvalid
		}
		var pending int64
		if e = tx.Model(&ShopOrder{}).Where("user_id = ? AND (payment_state = ? OR (purchase_state = ? AND payment_state = ?))", c.ActorID, "verifying", "open", "unpaid").Count(&pending).Error; e != nil {
			return e
		}
		if settings.MaxUnpaidOrders <= 0 || pending >= int64(settings.MaxUnpaidOrders) {
			return ErrShopPendingLimit
		}
		var terms ShopProductTerms
		var retention ShopRetentionPolicy
		if common.UnmarshalJsonStr(p.TermsJSON, &terms) != nil || common.UnmarshalJsonStr(settings.RetentionPolicyJSON, &retention) != nil {
			return ErrShopInvalid
		}
		complete, e := shopInputComplete(terms, in.InputKeys)
		if e != nil {
			return e
		}
		if len(in.InputKeys) > 0 && (in.BuyerInput.Ciphertext == "" || in.BuyerInput.KeyVersion == "" || c.HashKeyVersion == "") {
			return ErrShopInvalid
		}
		expires, e := shopTimeAdd(now, settings.ReservationSeconds)
		if e != nil {
			return e
		}
		snapshot := ShopOrderSnapshot{ProductPublicID: p.PublicID, Name: p.Name, CoverRef: p.CoverRef, Specification: p.Specification, ProductKind: p.ProductKind, DeliveryType: p.DeliveryType, Terms: terms, Retention: retention}
		b, e := common.Marshal(snapshot)
		if e != nil {
			return e
		}
		rec, e := shopRecord("so_", now)
		if e != nil {
			return e
		}
		if in.PublicID != "" {
			if len(in.PublicID) > 64 {
				return ErrShopInvalid
			}
			rec.PublicID = in.PublicID
		}
		result = ShopOrder{ShopRecord: rec, TenantID: PlatformTenantID, UserID: c.ActorID, ProductID: p.ID, ProductVersion: p.Version, SnapshotVersion: 1, SnapshotJSON: string(b), SnapshotHash: shopDigest(snapshot), TermsVersion: p.TermsVersion, PolicyAlgorithmVersion: 1, Quantity: 1, AmountMinor: p.PriceMinor, Currency: p.Currency, CurrencyExponent: p.CurrencyExponent, TermsAcceptedAt: now, PurchaseState: "open", PaymentState: "unpaid", DeliveryState: "not_ready", ReservationExpiresAt: expires, BuyerInputCiphertext: in.BuyerInput.Ciphertext, BuyerInputKeyVersion: in.BuyerInput.KeyVersion, RetentionPolicyVersion: settings.PolicyVersion}
		if complete {
			result.InputCompleteAt = now
			result.MissingInputKeysJSON = "[]"
		} else {
			missing := make([]string, 0)
			for _, field := range terms.BuyerInputSchema {
				if field.Required && !slices.Contains(in.InputKeys, field.Key) {
					missing = append(missing, field.Key)
				}
			}
			b, err := common.Marshal(missing)
			if err != nil {
				return err
			}
			result.MissingInputKeysJSON = string(b)
		}
		if e = tx.Create(&result).Error; e != nil {
			return e
		}
		if p.DeliveryType == "automatic" {
			var stock ShopStockUnit
			if e = lockForUpdate(tx).Where("product_id = ? AND state = ?", p.ID, "available").Order("id asc").First(&stock).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return ErrShopStock
				}
				return e
			}
			q := tx.Model(&ShopStockUnit{}).Where("id = ? AND version = ? AND state = ?", stock.ID, stock.Version, "available").Updates(map[string]any{"state": "reserved", "reserved_order_id": result.ID, "reservation_expires_at": expires, "version": gorm.Expr("version + 1"), "updated_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrShopStock
			}
			result.ReservedStockID = &stock.ID
		} else {
			q := tx.Model(&ShopProduct{}).Where("id = ? AND version = ? AND manual_used < manual_capacity", p.ID, p.Version).Updates(map[string]any{"manual_used": gorm.Expr("manual_used + 1"), "updated_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrShopCapacity
			}
			result.ManualCapacityHeld = true
		}
		if e = tx.Model(&ShopOrder{}).Where("id = ?", result.ID).Updates(map[string]any{"reserved_stock_id": result.ReservedStockID, "manual_capacity_held": result.ManualCapacityHeld}).Error; e != nil {
			return e
		}
		if e = shopEvent(tx, c, &result.ID, nil, "order_created", "public", "", nil); e != nil {
			return e
		}
		return shopRemember(tx, c, "create-order", "", "order", result.PublicID, nil)
	})
	return &result, err
}
func GetShopOrder(db *gorm.DB, scope ShopScope, publicID string) (*ShopOrder, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, err
	}
	var o ShopOrder
	q := db.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID)
	if !scope.Admin {
		q = q.Where("user_id = ?", scope.UserID)
	}
	e := q.First(&o).Error
	return &o, shopNotFound(e)
}
func ListShopOrders(db *gorm.DB, scope ShopScope, f ShopOrderFilter) ([]ShopOrder, int64, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopOrder{}).Where("tenant_id = ?", PlatformTenantID)
	if !scope.Admin {
		q = q.Where("user_id = ?", scope.UserID)
	} else if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.ProductID != nil {
		q = q.Where("product_id = ?", *f.ProductID)
	}
	if f.PublicID != "" {
		q = q.Where("public_id = ?", f.PublicID)
	}
	if f.PaymentState != "" {
		q = q.Where("payment_state = ?", f.PaymentState)
	}
	if f.DeliveryState != "" {
		q = q.Where("delivery_state = ?", f.DeliveryState)
	}
	if f.PurchaseState != "" {
		q = q.Where("purchase_state = ?", f.PurchaseState)
	}
	switch f.StatusGroup {
	case "", "all":
	case "unpaid":
		q = q.Where("payment_state IN ? AND purchase_state = ?", []string{"unpaid", "verifying"}, "open")
	case "delivery":
		q = q.Where("payment_state = ? AND delivery_state <> ? AND active_aftersale_id IS NULL", "paid", "delivered")
	case "delivered":
		q = q.Where("delivery_state = ? AND active_aftersale_id IS NULL", "delivered")
	case "aftersale":
		q = q.Where("active_aftersale_id IS NOT NULL")
	default:
		return nil, 0, ErrShopInvalid
	}
	if f.From > 0 {
		q = q.Where("created_at >= ?", f.From)
	}
	if f.To > 0 {
		q = q.Where("created_at < ?", f.To)
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.Page > 1000000 {
		return nil, 0, ErrShopInvalid
	}
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	var rows []ShopOrder
	e := q.Order("id desc").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&rows).Error
	return rows, n, e
}
func GetShopOrderDetail(db *gorm.DB, scope ShopScope, publicID string) (*ShopOrderDetail, error) {
	o, e := GetShopOrder(db, scope, publicID)
	if e != nil {
		return nil, e
	}
	r := &ShopOrderDetail{Order: *o}
	if r.Snapshot, e = shopSnapshot(o); e != nil {
		return nil, e
	}
	for _, dest := range []any{&r.Deliveries, &r.Attempts, &r.Receipts, &r.Aftersales, &r.Refunds} {
		if e = db.Where("order_id = ?", o.ID).Order("id desc").Limit(100).Find(dest).Error; e != nil {
			return nil, e
		}
	}
	// Current responsibilities remain visible even when older than the bounded history.
	if o.ActiveAttemptID != nil && !slices.ContainsFunc(r.Attempts, func(v ShopPaymentAttempt) bool { return v.ID == *o.ActiveAttemptID }) {
		var row ShopPaymentAttempt
		if e = db.Where("id = ? AND order_id = ?", *o.ActiveAttemptID, o.ID).First(&row).Error; e != nil {
			return nil, e
		}
		r.Attempts = append(r.Attempts, row)
	}
	if o.ActiveAftersaleID != nil && !slices.ContainsFunc(r.Aftersales, func(v ShopAftersale) bool { return v.ID == *o.ActiveAftersaleID }) {
		var row ShopAftersale
		if e = db.Where("id = ? AND order_id = ?", *o.ActiveAftersaleID, o.ID).First(&row).Error; e != nil {
			return nil, e
		}
		r.Aftersales = append(r.Aftersales, row)
	}
	if o.EffectiveReceiptID != nil {
		if !slices.ContainsFunc(r.Receipts, func(v ShopPaymentReceipt) bool { return v.ID == *o.EffectiveReceiptID }) {
			var row ShopPaymentReceipt
			if e = db.Where("id = ? AND order_id = ?", *o.EffectiveReceiptID, o.ID).First(&row).Error; e != nil {
				return nil, e
			}
			r.Receipts = append(r.Receipts, row)
		}
		if !slices.ContainsFunc(r.Refunds, func(v ShopRefundCase) bool { return v.ReceiptID == *o.EffectiveReceiptID }) {
			var rows []ShopRefundCase
			if e = db.Where("receipt_id = ? AND order_id = ?", *o.EffectiveReceiptID, o.ID).Find(&rows).Error; e != nil {
				return nil, e
			}
			r.Refunds = append(r.Refunds, rows...)
		}
	}
	if o.CurrentDeliveryID != nil {
		var d ShopDelivery
		if e = db.Where("id = ? AND order_id = ?", *o.CurrentDeliveryID, o.ID).First(&d).Error; e != nil {
			return nil, e
		}
		r.CurrentDelivery = &d
	}
	if o.EffectiveReceiptID != nil {
		var count int64
		if e = db.Model(&ShopPaymentAdjustment{}).Where("receipt_id = ? AND state IN ?", *o.EffectiveReceiptID, []string{"pending", "succeeded", "disputed"}).Count(&count).Error; e != nil {
			return nil, e
		}
		r.HasPaymentAdjustment = count > 0
	}
	q := db.Where("order_id = ?", o.ID)
	if !scope.Admin {
		q = q.Where("visibility = ?", "public")
	}
	e = q.Order("id desc").Limit(20).Find(&r.Events).Error
	return r, e
}
func shopReleaseReservation(tx *gorm.DB, p *ShopProduct, o *ShopOrder, now int64) error {
	if o.ReservedStockID != nil {
		var s ShopStockUnit
		if e := lockForUpdate(tx).First(&s, *o.ReservedStockID).Error; e != nil {
			return e
		}
		if s.State == "reserved" && s.ReservedOrderID != nil && *s.ReservedOrderID == o.ID {
			if e := shopCAS(tx, &ShopStockUnit{}, s.ID, s.Version, now, map[string]any{"state": "available", "reserved_order_id": nil, "reservation_expires_at": 0}); e != nil {
				return e
			}
		}
		o.ReservedStockID = nil
	}
	if o.ManualCapacityHeld {
		q := tx.Model(&ShopProduct{}).Where("id = ? AND manual_used > 0", p.ID).Updates(map[string]any{"manual_used": gorm.Expr("manual_used - 1"), "updated_at": now})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrShopCapacity
		}
		o.ManualCapacityHeld = false
	}
	return nil
}
func CloseShopOrder(db *gorm.DB, c ShopCommand, id, reason string) (*ShopOrder, error) {
	var result *ShopOrder
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, false); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, "close-order", id)
		if e != nil {
			return e
		}
		if k != nil {
			result, e = GetShopOrder(tx, ShopScope{UserID: c.ActorID, TenantID: c.TenantID}, id)
			return e
		}
		p, o, e := shopLockOrder(tx, id, c.ActorID, true, shopNow(c.Now))
		if e != nil {
			return e
		}
		if e = shopVersion(o.Version, c.ExpectedVersion); e != nil {
			return e
		}
		if o.PaymentState == "paid" {
			return ErrShopAction
		}
		if e = shopReleaseReservation(tx, p, o, shopNow(c.Now)); e != nil {
			return e
		}
		if e = shopUpdateOrder(tx, o, shopNow(c.Now), map[string]any{"purchase_state": "closed", "closed_at": shopNow(c.Now), "closed_reason": reason, "reserved_stock_id": nil, "manual_capacity_held": false}); e != nil {
			return e
		}
		if e = shopEvent(tx, c, &o.ID, nil, "order_closed", "public", reason, nil); e != nil {
			return e
		}
		result = o
		return shopRemember(tx, c, "close-order", id, "order", id, nil)
	})
	return result, err
}
func ExpireShopOrder(db *gorm.DB, id string, now int64) error {
	return shopTransaction(db, func(tx *gorm.DB) error {
		p, o, e := shopLockOrder(tx, id, 0, false, now)
		if e != nil {
			return e
		}
		if o.PurchaseState != "open" || o.ReservationExpiresAt > now || o.PaymentState == "paid" {
			return nil
		}
		if e = shopReleaseReservation(tx, p, o, now); e != nil {
			return e
		}
		if e = shopUpdateOrder(tx, o, now, map[string]any{"purchase_state": "closed", "closed_at": now, "closed_reason": "expired", "reserved_stock_id": nil, "manual_capacity_held": false}); e != nil {
			return e
		}
		return shopEvent(tx, ShopCommand{Now: now}, &o.ID, nil, "order_expired", "public", "", nil)
	})
}
func shopPolicyDates(o *ShopOrder, s ShopOrderSnapshot, now int64) (map[string]any, error) {
	f := map[string]any{}
	if o.PaidAt > 0 && o.InputCompleteAt > 0 && (o.MissingInputKeysJSON == "" || o.MissingInputKeysJSON == "[]") && o.SLAStartedAt == 0 && s.Terms.DeliveryPolicy.SLA != nil {
		start := max(o.PaidAt, o.InputCompleteAt)
		due, e := ShopSLADeadline(start, s.Terms.DeliveryPolicy.SLA)
		if e != nil {
			return nil, e
		}
		f["sla_started_at"] = start
		f["original_delivery_due_at"] = due
		f["delivery_due_at"] = due
	}
	for _, item := range []struct {
		policy  ShopPeriodPolicy
		prefix  string
		started int64
	}{{s.Terms.WarrantyPolicy, "warranty", o.WarrantyStartedAt}, {s.Terms.EntitlementPolicy, "entitlement", o.EntitlementStartsAt}} {
		if item.started != 0 {
			continue
		}
		p := item.policy
		start := int64(0)
		if p.StartEvent == "paid_at" {
			start = o.PaidAt
		} else if p.StartEvent == "first_delivered_at" && o.CurrentDeliveryID != nil {
			start = now
		}
		if p.Mode == "fixed_end" && p.FixedEndAt != nil {
			end, e := time.Parse(time.RFC3339, *p.FixedEndAt)
			if e != nil {
				return nil, ErrShopInvalid
			}
			start = o.PaidAt
			if start > 0 {
				if item.prefix == "warranty" {
					f["warranty_started_at"] = start
					f["warranty_ends_at"] = end.Unix()
				} else {
					f["entitlement_starts_at"] = start
					f["entitlement_ends_at"] = end.Unix()
				}
			}
		} else if p.Mode == "duration" && start > 0 {
			end, e := shopTimeAdd(start, p.DurationSeconds)
			if e != nil {
				return nil, e
			}
			if item.prefix == "warranty" {
				f["warranty_started_at"] = start
				f["warranty_ends_at"] = end
			} else {
				f["entitlement_starts_at"] = start
				f["entitlement_ends_at"] = end
			}
		}
	}
	return f, nil
}

type ShopBuyerInputUpdate struct {
	Command       ShopCommand
	OrderPublicID string
	Secret        ShopSecret
	InputKeys     []string
}

func UpdateShopBuyerInput(db *gorm.DB, in ShopBuyerInputUpdate) (*ShopOrder, error) {
	c := in.Command
	var result *ShopOrder
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, false); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, "buyer-input", in.OrderPublicID)
		if e != nil {
			return e
		}
		if k != nil {
			result, e = GetShopOrder(tx, ShopScope{UserID: c.ActorID, TenantID: c.TenantID}, in.OrderPublicID)
			return e
		}
		_, o, e := shopLockOrder(tx, in.OrderPublicID, c.ActorID, true, shopNow(c.Now))
		if e != nil {
			return e
		}
		if e = shopVersion(o.Version, c.ExpectedVersion); e != nil {
			return e
		}
		if o.DeliveryState == "delivered" || o.PurchaseState == "closed" && o.PaymentState != "paid" {
			return ErrShopAction
		}
		s, e := shopSnapshot(o)
		if e != nil {
			return e
		}
		complete, e := shopInputComplete(s.Terms, in.InputKeys)
		if e != nil {
			return e
		}
		if in.Secret.Ciphertext == "" || in.Secret.KeyVersion == "" || c.HashKeyVersion == "" {
			return ErrShopInvalid
		}
		f := map[string]any{"buyer_input_ciphertext": in.Secret.Ciphertext, "buyer_input_key_version": in.Secret.KeyVersion}
		if complete {
			o.MissingInputKeysJSON = "[]"
			if o.InputCompleteAt == 0 {
				o.InputCompleteAt = shopNow(c.Now)
				f["input_complete_at"] = o.InputCompleteAt
			}
			f["missing_input_keys_json"] = "[]"
			if o.SLAPauseStartedAt > 0 && s.Terms.DeliveryPolicy.SLA != nil {
				pause, e := shopSLAPauseSeconds(o.SLAPauseStartedAt, shopNow(c.Now), s.Terms.DeliveryPolicy.SLA)
				if e != nil {
					return e
				}
				if pause > 0 {
					due, e := shopTimeAdd(o.DeliveryDueAt, pause)
					if e != nil {
						return e
					}
					f["delivery_due_at"] = due
				}
				f["sla_paused_seconds"] = o.SLAPausedSeconds + pause
				f["sla_pause_started_at"] = 0
			}
			if o.PaymentState == "paid" && o.DeliveryState == "awaiting_input" {
				f["delivery_state"] = "pending"
			}
		} else {
			missing := make([]string, 0)
			for _, field := range s.Terms.BuyerInputSchema {
				if field.Required && !slices.Contains(in.InputKeys, field.Key) {
					missing = append(missing, field.Key)
				}
			}
			b, err := common.Marshal(missing)
			if err != nil {
				return err
			}
			o.MissingInputKeysJSON = string(b)
			f["missing_input_keys_json"] = string(b)
			if o.PaymentState == "paid" {
				f["delivery_state"] = "awaiting_input"
			}
			if s.Terms.DeliveryPolicy.SLA != nil && s.Terms.DeliveryPolicy.SLA.PauseOnMissingInput && o.SLAStartedAt > 0 && o.SLAPauseStartedAt == 0 {
				f["sla_pause_started_at"] = shopNow(c.Now)
			}
		}
		dates, e := shopPolicyDates(o, s, shopNow(c.Now))
		if e != nil {
			return e
		}
		for k, v := range dates {
			f[k] = v
		}
		if e = shopUpdateOrder(tx, o, shopNow(c.Now), f); e != nil {
			return e
		}
		if e = shopEvent(tx, c, &o.ID, nil, "buyer_input_updated", "public", "", nil); e != nil {
			return e
		}
		result = o
		return shopRemember(tx, c, "buyer-input", in.OrderPublicID, "order", o.PublicID, nil)
	})
	return result, err
}

// MutateShopOrder handles explicit operations; it never accepts arbitrary state
// strings or a free-form database patch.
type ShopOrderActionInput struct {
	Command          ShopCommand
	OrderPublicID    string
	Action           string
	Reason           string
	PublicReason     string
	EvidenceIDs      []string
	MissingFieldKeys []string
	Visibility       string
}

func MutateShopOrder(db *gorm.DB, in ShopOrderActionInput) (*ShopOrder, error) {
	c := in.Command
	scope := in.Action
	var result *ShopOrder
	if !slices.Contains([]string{"claim-order", "request-input", "risk-hold", "risk-release", "order-note", "retry-delivery"}, scope) {
		return nil, ErrShopInvalid
	}
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if e := shopAuthorize(tx, c, true); e != nil {
			return e
		}
		k, e := shopIdempotency(tx, c, scope, in.OrderPublicID)
		if e != nil {
			return e
		}
		if k != nil {
			result, e = GetShopOrder(tx, ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: true}, in.OrderPublicID)
			return e
		}
		_, o, e := shopLockOrder(tx, in.OrderPublicID, 0, false, shopNow(c.Now))
		if e != nil {
			return e
		}
		if e = shopVersion(o.Version, c.ExpectedVersion); e != nil {
			return e
		}
		if scope != "claim-order" && !shopRequired(in.Reason, 2000) {
			return ErrShopInvalid
		}
		f := map[string]any{}
		visibility := "internal"
		note := in.Reason
		switch scope {
		case "claim-order":
			f["processing_owner_id"] = c.ActorID
		case "request-input":
			s, e := shopSnapshot(o)
			if e != nil {
				return e
			}
			if s.DeliveryType != "manual" || o.PaymentState != "paid" || o.DeliveryState == "delivered" || len(in.MissingFieldKeys) == 0 {
				return ErrShopAction
			}
			allowed := map[string]bool{}
			for _, field := range s.Terms.BuyerInputSchema {
				if field.Required {
					allowed[field.Key] = true
				}
			}
			if !shopRequired(in.PublicReason, 2000) {
				return ErrShopInvalid
			}
			seen := map[string]bool{}
			for _, key := range in.MissingFieldKeys {
				if !allowed[key] || seen[key] {
					return ErrShopInvalid
				}
				seen[key] = true
			}
			b, e := common.Marshal(in.MissingFieldKeys)
			if e != nil {
				return e
			}
			f["missing_input_keys_json"] = string(b)
			f["delivery_state"] = "awaiting_input"
			if s.Terms.DeliveryPolicy.SLA != nil && s.Terms.DeliveryPolicy.SLA.PauseOnMissingInput && o.SLAStartedAt > 0 && o.SLAPauseStartedAt == 0 {
				f["sla_pause_started_at"] = shopNow(c.Now)
			}
			visibility = "public"
			note = in.PublicReason
		case "risk-hold":
			if !shopRequired(in.PublicReason, 2000) {
				return ErrShopInvalid
			}
			f["risk_hold"] = true
			f["risk_reason_code"] = in.Reason
			f["risk_public_note"] = in.PublicReason
			f["risk_actor_id"] = c.ActorID
			f["risk_changed_at"] = shopNow(c.Now)
		case "risk-release":
			f["risk_hold"] = false
			f["risk_actor_id"] = c.ActorID
			f["risk_changed_at"] = shopNow(c.Now)
		case "order-note":
			if !slices.Contains([]string{"public", "internal"}, in.Visibility) {
				return ErrShopInvalid
			}
			visibility = in.Visibility
		case "retry-delivery":
			s, err := shopSnapshot(o)
			if err != nil {
				return err
			}
			if s.DeliveryType != "automatic" || o.DeliveryState != "exception" || o.PaymentState != "paid" || o.CurrentDeliveryID != nil || o.RiskHold {
				return ErrShopAction
			}
			if e = shopDeliveryAllowed(tx, o); e != nil {
				return e
			}
			if e = shopEnqueue(tx, ShopTopicFulfillment, shopEventKey("retry", o.ID, o.Version+1), o.PublicID, map[string]any{"schema_version": 1, "order_id": o.ID, "order_public_id": o.PublicID}, shopNow(c.Now)); e != nil {
				return e
			}
		}
		if e = shopUpdateOrder(tx, o, shopNow(c.Now), f); e != nil {
			return e
		}
		if e = shopEvent(tx, c, &o.ID, o.EffectiveReceiptID, strings.ReplaceAll(scope, "-", "_"), visibility, note, in.EvidenceIDs); e != nil {
			return e
		}
		result = o
		return shopRemember(tx, c, scope, in.OrderPublicID, "order", o.PublicID, nil)
	})
	return result, err
}

// Responsibility reads run under the order/user write fence without acquiring
// later business locks, preserving the global lock order.
func shopOrderHasResponsibilities(tx *gorm.DB, order *ShopOrder, includeAftersale bool) (bool, error) {
	if order.RiskHold || order.PaymentState == "verifying" || includeAftersale && order.ActiveAftersaleID != nil {
		return true, nil
	}
	var n int64
	for _, q := range []*gorm.DB{
		tx.Model(&ShopPaymentAttempt{}).Where("order_id = ? AND create_state IN ? AND close_state <> ?", order.ID, []string{"prepared", "creating", "ready", "unknown"}, "closed"),
		tx.Model(&ShopRefundCase{}).Where("order_id = ? AND state IN ?", order.ID, []string{"approved", "processing", "unknown", "failed"}),
	} {
		if err := q.Count(&n).Error; err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	var receipts []ShopPaymentReceipt
	if err := tx.Where("order_id = ?", order.ID).Order("id asc").Find(&receipts).Error; err != nil {
		return false, err
	}
	effectiveReturned := false
	for i := range receipts {
		r := &receipts[i]
		adjusted, pending, err := shopFundsAdjusted(tx, r, "")
		if err != nil {
			return false, err
		}
		if pending {
			return true, nil
		}
		returned := r.AmountMinor > 0 && adjusted == r.AmountMinor
		if !returned && slices.Contains([]string{"pending", "review", "extra"}, r.ProcessingState) {
			return true, nil
		}
		if order.EffectiveReceiptID != nil && *order.EffectiveReceiptID == r.ID {
			effectiveReturned = returned
		}
	}
	if order.PaymentState == "paid" && order.DeliveryState != "delivered" && !effectiveReturned {
		return true, nil
	}
	return false, nil
}

func CheckShopUserDeletionTx(tx *gorm.DB, userID int) error {
	installed, err := shopDeletionSchemaReady(tx)
	if err != nil || !installed {
		return err
	}
	now := time.Now().Unix()
	if err = shopLockUser(tx, userID, false, now); err != nil {
		return err
	}
	var orders []ShopOrder
	if err = tx.Where("tenant_id = ? AND user_id = ?", PlatformTenantID, userID).Order("id asc").Find(&orders).Error; err != nil {
		return err
	}
	for i := range orders {
		if orders[i].PurchaseState == "open" && orders[i].PaymentState != "paid" {
			return ErrShopResponsibilities
		}
		pending, err := shopOrderHasResponsibilities(tx, &orders[i], true)
		if err != nil {
			return err
		}
		if pending {
			return ErrShopResponsibilities
		}
	}
	return tx.Model(&ShopUserState{}).Where("user_id = ?", userID).Updates(map[string]any{"retired_at": now, "updated_at": now}).Error
}
