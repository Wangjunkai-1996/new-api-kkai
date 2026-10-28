package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ShopPaymentAttemptInput struct {
	Command            ShopCommand
	OrderPublicID      string
	PublicID           string
	TradeNo            string
	Provider           string
	ProviderAccountRef string
	Environment        string
	PaymentMethod      string
	ProviderSnapshot   ShopSecret
}
type ShopPaymentSessionInput struct {
	AttemptID       int64
	ClaimToken      string
	ExpectedVersion int64
	State           string // ready, unknown, or failed (only a proven rejection)
	SessionID       string
	ProviderOrderID string
	Checkout        ShopSecret
	ExpiresAt       int64
	Reason          string
	EvidenceRef     string
	Now             int64
}
type ShopPaymentCheckInput struct {
	AttemptID       int64
	ClaimToken      string
	ExpectedVersion int64
	ReliableClosed  bool
	PaymentState    string // unpaid, unknown; paid must first persist a verified receipt
	EvidenceRef     string
	Reason          string
	Now             int64
}
type ShopVerifiedReceiptInput struct {
	Receipt  ShopPaymentReceipt
	Verified bool
	Now      int64
}

func CreateShopPaymentAttempt(db *gorm.DB, in ShopPaymentAttemptInput) (*ShopPaymentAttempt, bool, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopPaymentAttempt
	created := false
	err := shopTransaction(db, func(tx *gorm.DB) error {
		created = false
		if err := shopAuthorize(tx, c, false); err != nil {
			return err
		}
		var o ShopOrder
		if err := tx.Where("public_id = ? AND user_id = ? AND tenant_id = ?", in.OrderPublicID, c.ActorID, PlatformTenantID).First(&o).Error; err != nil {
			return shopNotFound(err)
		}
		if err := shopLockUser(tx, o.UserID, true, now); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "payment-attempt", in.OrderPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ? AND order_id = ?", k.ResourcePublicID, o.ID).First(&result).Error
		}
		var settings ShopSettings
		if err = lockForUpdate(tx).First(&settings, 1).Error; err != nil {
			return ErrShopPaused
		}
		var product ShopProduct
		if err = lockForUpdate(tx).First(&product, o.ProductID).Error; err != nil {
			return err
		}
		if err = lockForUpdate(tx).First(&o, o.ID).Error; err != nil {
			return err
		}
		if err = shopVersion(o.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if o.ActiveAttemptID != nil {
			if err = lockForUpdate(tx).First(&result, *o.ActiveAttemptID).Error; err != nil {
				return err
			}
			if !slices.Contains([]string{"closed", "failed"}, result.CreateState) {
				if err = shopRemember(tx, c, "payment-attempt", in.OrderPublicID, "payment_attempt", result.PublicID, nil); err != nil {
					return err
				}
				return nil
			}
			if result.CloseState != "closed" || result.CloseEvidenceRef == "" {
				return ErrShopPaymentUnresolved
			}
		}
		if !settings.SalesEnabled {
			return ErrShopPaused
		}
		if o.PurchaseState != "open" || o.PaymentState == "paid" || o.ReservationExpiresAt <= now || o.RiskHold {
			return ErrShopAction
		}
		if in.Provider != settings.Provider || in.ProviderAccountRef != settings.ProviderAccountRef || in.Environment != settings.Environment || o.Currency != settings.Currency || o.CurrencyExponent != settings.CurrencyExponent {
			return ErrShopAction
		}
		if !shopRequired(in.PublicID, 64) || !shopRequired(in.TradeNo, 128) || !strings.HasPrefix(in.TradeNo, "KS") || !shopRequired(in.PaymentMethod, 32) || in.ProviderSnapshot.Ciphertext == "" || in.ProviderSnapshot.KeyVersion == "" {
			return ErrShopInvalid
		}
		if product.DeliveryType == "automatic" && o.ReservedStockID == nil || product.DeliveryType == "manual" && !o.ManualCapacityHeld {
			return ErrShopAction
		}
		var last ShopPaymentAttempt
		err = tx.Where("order_id = ?", o.ID).Order("attempt_no desc").First(&last).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		token, err := NewShopPublicID("")
		if err != nil {
			return err
		}
		result = ShopPaymentAttempt{ShopRecord: ShopRecord{PublicID: in.PublicID, Version: 1, CreatedAt: now, UpdatedAt: now}, OrderID: o.ID, TenantID: PlatformTenantID, UserID: o.UserID, AttemptNo: last.AttemptNo + 1, TradeNo: in.TradeNo, Provider: in.Provider, ProviderAccountRef: in.ProviderAccountRef, Environment: in.Environment, PaymentMethod: in.PaymentMethod, ExpectedAmountMinor: o.AmountMinor, Currency: o.Currency, CurrencyExponent: o.CurrencyExponent, BusinessKind: "shop", RequestHash: c.RequestHash, ProviderSnapshotCiphertext: in.ProviderSnapshot.Ciphertext, CredentialKeyVersion: in.ProviderSnapshot.KeyVersion, CreateState: "creating", ClaimToken: token, ClaimUntil: now + 60, NextCheckAt: now + 60}
		if err = tx.Create(&result).Error; err != nil {
			return err
		}
		if err = shopUpdateOrder(tx, &o, now, map[string]any{"active_attempt_id": result.ID, "payment_state": "verifying"}); err != nil {
			return err
		}
		if err = shopEvent(tx, c, &o.ID, nil, "payment_attempt_created", "public", "", nil); err != nil {
			return err
		}
		if err = shopRemember(tx, c, "payment-attempt", in.OrderPublicID, "payment_attempt", result.PublicID, nil); err != nil {
			return err
		}
		created = true
		return nil
	})
	return &result, created, err
}

func SaveShopPaymentSession(db *gorm.DB, in ShopPaymentSessionInput) (*ShopPaymentAttempt, error) {
	if !slices.Contains([]string{"ready", "unknown", "failed"}, in.State) || in.ClaimToken == "" {
		return nil, ErrShopInvalid
	}
	if in.State == "ready" && (in.Checkout.Ciphertext == "" || in.Checkout.KeyVersion == "" || in.SessionID == "") {
		return nil, ErrShopInvalid
	}
	if in.State == "failed" && in.EvidenceRef == "" {
		return nil, ErrShopEvidence
	}
	var result ShopPaymentAttempt
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&result, in.AttemptID).Error; err != nil {
			return shopNotFound(err)
		}
		if result.CreateState == "paid" {
			return nil
		}
		if result.ClaimToken != in.ClaimToken || result.Version != in.ExpectedVersion {
			return ErrShopVersion
		}
		fields := map[string]any{"create_state": in.State, "claim_token": "", "claim_until": 0, "next_check_at": shopNow(in.Now) + 60, "last_reason_code": in.Reason}
		if in.State == "ready" {
			fields["provider_session_id"] = in.SessionID
			fields["provider_order_id"] = in.ProviderOrderID
			fields["checkout_ciphertext"] = in.Checkout.Ciphertext
			fields["checkout_key_version"] = in.Checkout.KeyVersion
			fields["provider_expires_at"] = in.ExpiresAt
		}
		if in.State == "failed" {
			fields["close_state"] = "closed"
			fields["close_evidence_ref"] = in.EvidenceRef
			fields["next_check_at"] = 0
		}
		if err := shopCAS(tx, &ShopPaymentAttempt{}, result.ID, result.Version, shopNow(in.Now), fields); err != nil {
			return err
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}
func ClaimShopPaymentCheck(db *gorm.DB, publicID string, now int64) (*ShopPaymentAttempt, error) {
	var result ShopPaymentAttempt
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("public_id = ?", publicID).First(&result).Error; err != nil {
			return shopNotFound(err)
		}
		if result.CreateState == "paid" || result.ClaimUntil > now {
			return ErrShopPaymentUnresolved
		}
		token, err := NewShopPublicID("")
		if err != nil {
			return err
		}
		if err = shopCAS(tx, &ShopPaymentAttempt{}, result.ID, result.Version, now, map[string]any{"claim_token": token, "claim_until": now + 60, "check_attempts": gorm.Expr("check_attempts + 1")}); err != nil {
			return err
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}
func ApplyShopPaymentCheck(db *gorm.DB, in ShopPaymentCheckInput) (*ShopPaymentAttempt, error) {
	if in.ClaimToken == "" || !slices.Contains([]string{"unpaid", "unknown"}, in.PaymentState) || in.ReliableClosed && in.EvidenceRef == "" {
		return nil, ErrShopInvalid
	}
	var result ShopPaymentAttempt
	err := shopTransaction(db, func(tx *gorm.DB) error {
		var initial ShopPaymentAttempt
		if err := tx.First(&initial, in.AttemptID).Error; err != nil {
			return shopNotFound(err)
		}
		var original ShopOrder
		if err := tx.First(&original, initial.OrderID).Error; err != nil {
			return err
		}
		_, order, err := shopLockOrder(tx, original.PublicID, 0, false, shopNow(in.Now))
		if err != nil {
			return err
		}
		if err = lockForUpdate(tx).First(&result, in.AttemptID).Error; err != nil {
			return err
		}
		if result.CreateState == "paid" {
			return nil
		}
		if result.ClaimToken != in.ClaimToken || result.Version != in.ExpectedVersion {
			return ErrShopVersion
		}
		fields := map[string]any{"claim_token": "", "claim_until": 0, "last_checked_at": shopNow(in.Now), "next_check_at": shopNow(in.Now) + 60, "last_reason_code": in.Reason}
		if in.ReliableClosed {
			fields["create_state"] = "closed"
			fields["close_state"] = "closed"
			fields["close_evidence_ref"] = in.EvidenceRef
			fields["next_check_at"] = 0
		} else if in.PaymentState == "unknown" {
			fields["create_state"] = "unknown"
		}
		if err = shopCAS(tx, &ShopPaymentAttempt{}, result.ID, result.Version, shopNow(in.Now), fields); err != nil {
			return err
		}
		if in.ReliableClosed && order.PaymentState != "paid" && order.ActiveAttemptID != nil && *order.ActiveAttemptID == result.ID {
			if err = shopUpdateOrder(tx, order, shopNow(in.Now), map[string]any{"payment_state": "unpaid"}); err != nil {
				return err
			}
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}
func RequestShopPaymentReconcile(db *gorm.DB, c ShopCommand, orderID string) (*ShopOrder, error) {
	var result *ShopOrder
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, false); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "reconcile-payment", orderID)
		if err != nil {
			return err
		}
		if k != nil {
			result, err = GetShopOrder(tx, ShopScope{UserID: c.ActorID, TenantID: c.TenantID}, orderID)
			return err
		}
		_, o, err := shopLockOrder(tx, orderID, c.ActorID, true, shopNow(c.Now))
		if err != nil {
			return err
		}
		if err = shopVersion(o.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if o.ActiveAttemptID == nil {
			return ErrShopAction
		}
		var attempt ShopPaymentAttempt
		if err = lockForUpdate(tx).First(&attempt, *o.ActiveAttemptID).Error; err != nil {
			return err
		}
		if attempt.OrderID != o.ID || attempt.CreateState == "paid" {
			return ErrShopAction
		}
		if err = shopCAS(tx, &ShopPaymentAttempt{}, attempt.ID, attempt.Version, shopNow(c.Now), map[string]any{"next_check_at": shopNow(c.Now)}); err != nil {
			return err
		}
		if err = shopUpdateOrder(tx, o, shopNow(c.Now), map[string]any{}); err != nil {
			return err
		}
		result = o
		return shopRemember(tx, c, "reconcile-payment", orderID, "order", orderID, nil)
	})
	return result, err
}
func SaveShopVerifiedReceipt(db *gorm.DB, in ShopVerifiedReceiptInput) (*ShopPaymentReceipt, error) {
	r := in.Receipt
	if !in.Verified || !shopRequired(r.Provider, 50) || !shopRequired(r.ProviderAccountRef, 128) || !shopRequired(r.Environment, 32) || !shopRequired(r.ProviderTransactionID, 191) || !shopRequired(r.EvidenceRef, 500) || r.AmountMinor <= 0 || len(r.Currency) != 3 || r.Currency != strings.ToUpper(r.Currency) || r.CurrencyExponent < 0 || r.CurrencyExponent > 6 || r.ProviderPaidAt < 0 || !slices.Contains([]string{"provider_query", "provider_webhook"}, r.VerificationSource) {
		return nil, ErrShopInvalid
	}
	now := shopNow(in.Now)
	r.ShopRecord = ShopRecord{}
	rec, err := shopRecord("sr_", now)
	if err != nil {
		return nil, err
	}
	r.ShopRecord = rec
	r.ReceiptKey = shopDigest([]string{r.Provider, r.ProviderAccountRef, r.Environment, r.ProviderTransactionID})
	r.AttemptID = nil
	r.OrderID = nil
	r.ProcessingState = "pending"
	r.ExceptionCode = ""
	r.ReceivedAt = now
	r.NextRetryAt = now
	var stored ShopPaymentReceipt
	conflict := false
	err = shopTransaction(db, func(tx *gorm.DB) error {
		conflict = false
		// The receipt and its recovery event commit without taking business locks.
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "receipt_key"}}, DoNothing: true}).Create(&r).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("receipt_key = ?", r.ReceiptKey).First(&stored).Error; err != nil {
			return err
		}
		if stored.Provider != r.Provider || stored.ProviderAccountRef != r.ProviderAccountRef || stored.Environment != r.Environment || stored.ProviderTransactionID != r.ProviderTransactionID || stored.OriginalTradeRef != r.OriginalTradeRef || stored.AmountMinor != r.AmountMinor || stored.Currency != r.Currency || stored.CurrencyExponent != r.CurrencyExponent || stored.ProviderPaidAt != 0 && r.ProviderPaidAt != 0 && stored.ProviderPaidAt != r.ProviderPaidAt || stored.ProviderSessionID != "" && r.ProviderSessionID != "" && stored.ProviderSessionID != r.ProviderSessionID || stored.ProviderOrderID != "" && r.ProviderOrderID != "" && stored.ProviderOrderID != r.ProviderOrderID {
			conflict = true
			return shopEvent(tx, ShopCommand{Now: now}, stored.OrderID, &stored.ID, "receipt_conflict", "internal", r.EvidenceRef, nil)
		}
		updates := map[string]any{}
		if stored.ProviderSessionID == "" && r.ProviderSessionID != "" {
			updates["provider_session_id"] = r.ProviderSessionID
		}
		if stored.ProviderOrderID == "" && r.ProviderOrderID != "" {
			updates["provider_order_id"] = r.ProviderOrderID
		}
		if r.VerificationSource == "provider_query" && stored.VerificationSource != "provider_query" {
			updates["verification_source"] = r.VerificationSource
			updates["evidence_ref"] = r.EvidenceRef
			updates["evidence_hash"] = r.EvidenceHash
		}
		if r.PricingVerified && !stored.PricingVerified {
			updates["pricing_verified"] = true
		}
		if stored.ProviderPaidAt == 0 && r.ProviderPaidAt > 0 {
			updates["provider_paid_at"] = r.ProviderPaidAt
		}
		if len(updates) > 0 {
			if stored.ProcessingState == "review" || stored.ProcessingState == "pending" {
				updates["processing_state"] = "pending"
				updates["next_retry_at"] = now
			}
			if err := shopCAS(tx, &ShopPaymentReceipt{}, stored.ID, stored.Version, now, updates); err != nil {
				return err
			}
			if err := tx.First(&stored, stored.ID).Error; err != nil {
				return err
			}
		}
		return shopEnqueue(tx, ShopTopicReceipt, shopEventKey("receipt", stored.ID, stored.Version), stored.PublicID, map[string]any{"schema_version": 1, "receipt_id": stored.ID}, now)
	})
	if err == nil && conflict {
		err = ErrShopEvidence
	}
	return &stored, err
}
func MatchShopPaymentReceipt(db *gorm.DB, receiptID, now int64) (*ShopPaymentReceipt, error) {
	var result ShopPaymentReceipt
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := tx.First(&result, receiptID).Error; err != nil {
			return shopNotFound(err)
		}
		var initial ShopPaymentAttempt
		err := tx.Where("trade_no = ? AND business_kind = ?", result.OriginalTradeRef, "shop").First(&initial).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err = shopCAS(tx, &ShopPaymentReceipt{}, result.ID, result.Version, now, map[string]any{"processing_state": "review", "exception_code": "UNMATCHED_PAYMENT", "next_retry_at": now + 300, "last_processed_at": now}); err != nil {
				return err
			}
			return tx.First(&result, receiptID).Error
		}
		if err != nil {
			return err
		}
		var orderRef ShopOrder
		if err = tx.First(&orderRef, initial.OrderID).Error; err != nil {
			return err
		}
		product, order, err := shopLockOrder(tx, orderRef.PublicID, 0, false, now)
		if err != nil {
			return err
		}
		var attempt ShopPaymentAttempt
		if err = lockForUpdate(tx).First(&attempt, initial.ID).Error; err != nil {
			return err
		}
		if err = lockForUpdate(tx).First(&result, receiptID).Error; err != nil {
			return err
		}
		if result.OrderID != nil && (*result.OrderID != order.ID || result.AttemptID == nil || *result.AttemptID != attempt.ID) {
			return ErrShopEvidence
		}
		if result.ProcessingState == "matched" || result.ProcessingState == "extra" {
			return nil
		}
		var user User
		err = tx.Unscoped().Select("id", "status", "deleted_at").First(&user, order.UserID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		retired := errors.Is(err, gorm.ErrRecordNotFound) || user.DeletedAt.Valid || user.Status != common.UserStatusEnabled
		var userState ShopUserState
		if err = tx.First(&userState, order.UserID).Error; err != nil {
			return err
		}
		retired = retired || userState.RetiredAt > 0
		fields := map[string]any{"order_id": order.ID, "attempt_id": attempt.ID, "last_processed_at": now, "next_retry_at": 0}
		identity := result.Provider == attempt.Provider && result.ProviderAccountRef == attempt.ProviderAccountRef && result.Environment == attempt.Environment && result.OriginalTradeRef == attempt.TradeNo && (attempt.ProviderSessionID == "" || attempt.ProviderSessionID == result.ProviderSessionID) && (attempt.ProviderOrderID == "" || attempt.ProviderOrderID == result.ProviderOrderID)
		if !identity {
			fields["order_id"] = nil
			fields["attempt_id"] = nil
			fields["processing_state"] = "review"
			fields["exception_code"] = "PAYMENT_IDENTITY_MISMATCH"
		} else if retired {
			fields["processing_state"] = "review"
			fields["exception_code"] = "BUYER_RETIRED"
		} else if result.AmountMinor != order.AmountMinor || result.Currency != order.Currency || result.CurrencyExponent != order.CurrencyExponent || !result.PricingVerified || result.ProviderPaidAt <= 0 {
			fields["processing_state"] = "review"
			fields["exception_code"] = "PAYMENT_EVIDENCE_MISMATCH"
		} else if order.EffectiveReceiptID != nil && *order.EffectiveReceiptID != result.ID {
			fields["processing_state"] = "extra"
			fields["exception_code"] = "EXTRA_PAYMENT"
		} else {
			fields["processing_state"] = "matched"
			fields["exception_code"] = ""
			order.PaidAt = result.ProviderPaidAt
			snapshot, err := shopSnapshot(order)
			if err != nil {
				return err
			}
			orderFields, err := shopPolicyDates(order, snapshot, now)
			if err != nil {
				return err
			}
			orderFields["payment_state"] = "paid"
			orderFields["paid_at"] = result.ProviderPaidAt
			orderFields["effective_receipt_id"] = result.ID
			orderFields["delivery_state"] = "pending"
			if order.InputCompleteAt == 0 || order.MissingInputKeysJSON != "" && order.MissingInputKeysJSON != "[]" {
				orderFields["delivery_state"] = "awaiting_input"
			}
			if snapshot.DeliveryType == "manual" && !order.ManualCapacityHeld {
				capacity := tx.Model(&ShopProduct{}).Where("id = ? AND manual_used < manual_capacity", product.ID).Updates(map[string]any{"manual_used": gorm.Expr("manual_used + 1"), "updated_at": now})
				if capacity.Error != nil {
					return capacity.Error
				}
				if capacity.RowsAffected == 1 {
					orderFields["manual_capacity_held"] = true
				} else {
					orderFields["delivery_state"] = "exception"
					orderFields["exception_code"] = "CAPACITY_UNAVAILABLE"
				}
			}
			if err = shopUpdateOrder(tx, order, now, orderFields); err != nil {
				return err
			}
			if err = shopEnqueue(tx, ShopTopicFulfillment, shopEventKey("first-delivery", order.ID, 1), order.PublicID, map[string]any{"schema_version": 1, "order_id": order.ID, "order_public_id": order.PublicID}, now); err != nil {
				return err
			}
		}
		if identity && result.AmountMinor == order.AmountMinor && result.Currency == order.Currency && result.CurrencyExponent == order.CurrencyExponent && result.PricingVerified && result.ProviderPaidAt > 0 {
			if err = shopCAS(tx, &ShopPaymentAttempt{}, attempt.ID, attempt.Version, now, map[string]any{"create_state": "paid", "claim_token": "", "claim_until": 0, "next_check_at": 0}); err != nil {
				return err
			}
		}
		if err = shopCAS(tx, &ShopPaymentReceipt{}, result.ID, result.Version, now, fields); err != nil {
			return err
		}
		// Review only records a receipt awaiting identity/payment evidence. A
		// public received-payment fact belongs to a verified match (or a distinct
		// extra payment); those terminal states return early on subsequent retries.
		if fields["processing_state"] == "matched" || fields["processing_state"] == "extra" {
			if err = shopEvent(tx, ShopCommand{Now: now}, &order.ID, &result.ID, "payment_received", "public", "", nil); err != nil {
				return err
			}
		}
		return tx.First(&result, receiptID).Error
	})
	return &result, err
}

// A trusted provider signal freezes fulfillment without claiming money was
// returned. Only independently verified adjustment facts can record a refund.
func HoldShopPaymentForReview(db *gorm.DB, attemptPublicID, reason, evidenceRef string, now int64) error {
	if !shopRequired(reason, 64) || !shopRequired(evidenceRef, 500) {
		return ErrShopInvalid
	}
	now = shopNow(now)
	return shopTransaction(db, func(tx *gorm.DB) error {
		var attempt ShopPaymentAttempt
		if err := tx.Where("public_id = ?", attemptPublicID).First(&attempt).Error; err != nil {
			return shopNotFound(err)
		}
		var ref ShopOrder
		if err := tx.First(&ref, attempt.OrderID).Error; err != nil {
			return err
		}
		_, order, err := shopLockOrder(tx, ref.PublicID, 0, false, now)
		if err != nil {
			return err
		}
		if err = lockForUpdate(tx).First(&attempt, attempt.ID).Error; err != nil {
			return err
		}
		if err = shopUpdateOrder(tx, order, now, map[string]any{"risk_hold": true, "risk_reason_code": reason, "risk_public_note": "Payment requires review", "risk_evidence_ref": evidenceRef, "risk_actor_id": 0, "risk_changed_at": now}); err != nil {
			return err
		}
		return shopEvent(tx, ShopCommand{Now: now}, &order.ID, order.EffectiveReceiptID, "payment_provider_review", "internal", evidenceRef, nil)
	})
}
