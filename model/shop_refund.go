package model

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ShopEvidenceInput struct {
	Command              ShopCommand
	ReceiptPublicID      string
	Evidence             ShopPaymentEvidence
	CorrectionOfPublicID string
}
type ShopRefundApproveInput struct {
	Command         ShopCommand
	ReceiptPublicID string
	Kind            string
	Reason          string
	EvidenceIDs     []string
}
type ShopRefundActionInput struct {
	Command           ShopCommand
	RefundPublicID    string
	AttemptPublicID   string
	Action            string
	Reason            string
	PublicNote        string
	ProviderRefundRef string
	OperatedAt        int64
	Conclusion        string
	EvidenceIDs       []string
}
type ShopAdjustmentInput struct {
	Command               ShopCommand
	ReceiptPublicID       string
	AdjustmentPublicID    string
	Kind                  string
	ProviderAdjustmentRef string
	AmountMinor           int64
	Currency              string
	CurrencyExponent      int
	State                 string
	Reason                string
	EvidenceIDs           []string
}
type ShopReceiptReviewInput struct {
	Command         ShopCommand
	ReceiptPublicID string
	Action          string
	AssigneeID      int
	Reason          string
}

// Immutable references are read first; roots are then locked in domain order.
func shopLockReceipt(tx *gorm.DB, publicID string, now int64) (*ShopOrder, *ShopPaymentReceipt, error) {
	var ref ShopPaymentReceipt
	if err := tx.Where("public_id = ?", publicID).First(&ref).Error; err != nil {
		return nil, nil, shopNotFound(err)
	}
	var order *ShopOrder
	if ref.OrderID != nil {
		var orderRef ShopOrder
		if err := tx.Select("public_id").First(&orderRef, *ref.OrderID).Error; err != nil {
			return nil, nil, err
		}
		_, locked, err := shopLockOrder(tx, orderRef.PublicID, 0, false, now)
		if err != nil {
			return nil, nil, err
		}
		order = locked
	}
	var receipt ShopPaymentReceipt
	if err := lockForUpdate(tx).First(&receipt, ref.ID).Error; err != nil {
		return nil, nil, err
	}
	if (ref.OrderID == nil) != (receipt.OrderID == nil) || ref.OrderID != nil && *ref.OrderID != *receipt.OrderID {
		return nil, nil, ErrShopVersion
	}
	return order, &receipt, nil
}

func shopEvidenceRows(tx *gorm.DB, receiptID *int64, ids []string) ([]ShopPaymentEvidence, error) {
	if len(ids) > 20 || len(ids) > 0 && receiptID == nil {
		return nil, ErrShopEvidence
	}
	seen := make(map[string]bool, len(ids))
	rows := make([]ShopPaymentEvidence, 0, len(ids))
	for _, id := range ids {
		if !shopRequired(id, 64) || seen[id] {
			return nil, ErrShopEvidence
		}
		seen[id] = true
		var row ShopPaymentEvidence
		if err := tx.Where("public_id = ? AND receipt_id = ?", id, *receiptID).First(&row).Error; err != nil {
			return nil, shopEvidenceError(err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func shopEvidenceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrShopEvidence
	}
	return err
}
func shopEvidenceJSON(tx *gorm.DB, receiptID *int64, ids []string) (string, error) {
	if _, err := shopEvidenceRows(tx, receiptID, ids); err != nil {
		return "", err
	}
	b, err := common.Marshal(ids)
	return string(b), err
}

func RegisterShopPaymentEvidence(db *gorm.DB, in ShopEvidenceInput) (*ShopPaymentEvidence, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopPaymentEvidence
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "payment-evidence", in.ReceiptPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&result).Error
		}
		order, receipt, err := shopLockReceipt(tx, in.ReceiptPublicID, now)
		if err != nil {
			return err
		}
		if err = shopVersion(receipt.Version, c.ExpectedVersion); err != nil {
			return err
		}
		v := in.Evidence
		if v.Source != "merchant_console" || v.ProviderTransactionID != receipt.ProviderTransactionID || v.AmountMinor < 0 || v.AmountMinor > receipt.AmountMinor || v.Currency != receipt.Currency || v.CurrencyExponent != receipt.CurrencyExponent || !shopRequired(v.Reference, 500) || !shopRequired(v.Summary, 2000) || !shopRequired(v.ObservedStatus, 32) || !shopBoundedText(v.ProviderAdjustmentRef, 191) || v.ObservedAt <= 0 || v.ObservedAt > now+300 {
			return ErrShopEvidence
		}
		if in.CorrectionOfPublicID != "" {
			var previous ShopPaymentEvidence
			if err = tx.Where("public_id = ? AND receipt_id = ?", in.CorrectionOfPublicID, receipt.ID).First(&previous).Error; err != nil {
				return shopEvidenceError(err)
			}
			v.CorrectionOfID = &previous.ID
		} else {
			v.CorrectionOfID = nil
		}
		v.ShopRecord, err = shopRecord("spe_", now)
		if err != nil {
			return err
		}
		v.ReceiptID = receipt.ID
		v.Provider = receipt.Provider
		v.ProviderAccountRef = receipt.ProviderAccountRef
		v.Environment = receipt.Environment
		v.CreatedBy = c.ActorID
		if err = tx.Create(&v).Error; err != nil {
			return err
		}
		result = v
		if err = shopCAS(tx, &ShopPaymentReceipt{}, receipt.ID, receipt.Version, now, map[string]any{}); err != nil {
			return err
		}
		if err = shopCaseEvent(tx, c, order, &receipt.ID, nil, nil, "payment_evidence_added", "internal", "", []string{v.PublicID}); err != nil {
			return err
		}
		return shopRemember(tx, c, "payment-evidence", in.ReceiptPublicID, "evidence", v.PublicID, nil)
	})
	return &result, err
}

func shopCheckFundsEvidence(tx *gorm.DB, receipt *ShopPaymentReceipt, ids []string, amount int64, ref string, statuses []string) error {
	rows, err := shopEvidenceRows(tx, &receipt.ID, ids)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrShopEvidence
	}
	for _, row := range rows {
		if !slices.Contains([]string{"merchant_console", "provider_query", "provider_webhook"}, row.Source) || row.Provider != receipt.Provider || row.ProviderAccountRef != receipt.ProviderAccountRef || row.Environment != receipt.Environment || row.ProviderTransactionID != receipt.ProviderTransactionID || row.AmountMinor != amount || row.Currency != receipt.Currency || row.CurrencyExponent != receipt.CurrencyExponent || row.ProviderAdjustmentRef != ref || !slices.Contains(statuses, row.ObservedStatus) || row.Reference == "" || row.ObservedAt <= 0 {
			return ErrShopEvidence
		}
		var corrected int64
		if err = tx.Model(&ShopPaymentEvidence{}).Where("correction_of_id = ?", row.ID).Count(&corrected).Error; err != nil {
			return err
		}
		if corrected > 0 {
			return ErrShopEvidence
		}
	}
	return nil
}

// Bounds are checked before every addition. Failed facts do not consume funds.
func shopFundsAdjusted(tx *gorm.DB, receipt *ShopPaymentReceipt, excludeKey string) (int64, bool, error) {
	var rows []ShopPaymentAdjustment
	if err := tx.Where("receipt_id = ? AND state <> ?", receipt.ID, "failed").Order("id asc").Find(&rows).Error; err != nil {
		return 0, false, err
	}
	var total int64
	unresolved := false
	for _, row := range rows {
		if row.AdjustmentKey == excludeKey {
			continue
		}
		if row.AmountMinor <= 0 || row.AmountMinor > receipt.AmountMinor || row.Currency != receipt.Currency || row.CurrencyExponent != receipt.CurrencyExponent || total > receipt.AmountMinor-row.AmountMinor {
			return 0, true, ErrShopRefund
		}
		total += row.AmountMinor
		unresolved = unresolved || row.State != "succeeded"
	}
	return total, unresolved, nil
}

func ApproveShopRefund(db *gorm.DB, in ShopRefundApproveInput) (*ShopRefundCase, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopRefundCase
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "refund-approve", in.ReceiptPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&result).Error
		}
		order, receipt, err := shopLockReceipt(tx, in.ReceiptPublicID, now)
		if err != nil {
			return err
		}
		if err = shopVersion(receipt.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if !slices.Contains([]string{"goods", "extra", "mismatch"}, in.Kind) || !shopRequired(in.Reason, 2000) || receipt.AmountMinor <= 0 {
			return ErrShopInvalid
		}
		goods := order != nil && order.EffectiveReceiptID != nil && *order.EffectiveReceiptID == receipt.ID
		if in.Kind == "goods" && (!goods || receipt.ProcessingState != "matched") || in.Kind == "extra" && (goods || receipt.ProcessingState != "extra") || in.Kind == "mismatch" && (goods || receipt.ProcessingState != "review") {
			return ErrShopRefund
		}
		if len(in.EvidenceIDs) == 0 {
			return ErrShopEvidence
		}
		if _, err = shopEvidenceJSON(tx, &receipt.ID, in.EvidenceIDs); err != nil {
			return err
		}
		adjusted, _, err := shopFundsAdjusted(tx, receipt, "")
		if err != nil {
			return err
		}
		if adjusted != 0 {
			return ErrShopRefund
		}
		err = lockForUpdate(tx).Where("receipt_id = ?", receipt.ID).First(&result).Error
		if err == nil {
			if result.Kind != in.Kind || result.AmountMinor != receipt.AmountMinor {
				return ErrShopRefund
			}
			if result.State != "cancelled" {
				return shopRemember(tx, c, "refund-approve", in.ReceiptPublicID, "refund", result.PublicID, nil)
			}
			if err = shopCAS(tx, &ShopRefundCase{}, result.ID, result.Version, now, map[string]any{"state": "approved", "reason": in.Reason, "approved_by": c.ActorID, "approved_at": now, "cancelled_at": 0, "cancelled_by": 0, "cancel_reason": ""}); err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			rec, err := shopRecord("sr_", now)
			if err != nil {
				return err
			}
			result = ShopRefundCase{ShopRecord: rec, ReceiptID: receipt.ID, OrderID: receipt.OrderID, Kind: in.Kind, AmountMinor: receipt.AmountMinor, Currency: receipt.Currency, CurrencyExponent: receipt.CurrencyExponent, State: "approved", Reason: in.Reason, ApprovedBy: c.ActorID, ApprovedAt: now, ReviewOwnerID: c.ActorID, NextAction: "register_refund_attempt"}
			if order != nil {
				result.AftersaleID = order.ActiveAftersaleID
			}
			if err = tx.Create(&result).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		if err = shopCAS(tx, &ShopPaymentReceipt{}, receipt.ID, receipt.Version, now, map[string]any{}); err != nil {
			return err
		}
		if goods {
			if err = shopUpdateOrder(tx, order, now, map[string]any{}); err != nil {
				return err
			}
		}
		if err = shopCaseEvent(tx, c, order, &receipt.ID, result.AftersaleID, &result.ID, "refund_approved", "public", in.Reason, in.EvidenceIDs); err != nil {
			return err
		}
		if err = tx.First(&result, result.ID).Error; err != nil {
			return err
		}
		return shopRemember(tx, c, "refund-approve", in.ReceiptPublicID, "refund", result.PublicID, nil)
	})
	return &result, err
}

func MutateShopRefund(db *gorm.DB, in ShopRefundActionInput) (*ShopRefundCase, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopRefundCase
	if !slices.Contains([]string{"attempt", "record", "verify", "cancel"}, in.Action) {
		return nil, ErrShopInvalid
	}
	action := "refund-" + in.Action
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, action, in.RefundPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ?", k.ResourcePublicID).First(&result).Error
		}
		var ref ShopRefundCase
		if err = tx.Where("public_id = ?", in.RefundPublicID).First(&ref).Error; err != nil {
			return shopNotFound(err)
		}
		var receiptRef ShopPaymentReceipt
		if err = tx.Select("public_id").First(&receiptRef, ref.ReceiptID).Error; err != nil {
			return err
		}
		order, receipt, err := shopLockReceipt(tx, receiptRef.PublicID, now)
		if err != nil {
			return err
		}
		if err = lockForUpdate(tx).Where("id = ? AND receipt_id = ?", ref.ID, receipt.ID).First(&result).Error; err != nil {
			return err
		}
		if err = shopVersion(result.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if !shopRequired(in.Reason, 2000) || !shopBoundedText(in.PublicNote, 2000) || !shopBoundedText(in.ProviderRefundRef, 191) {
			return ErrShopInvalid
		}
		if result.State == "succeeded" || result.State == "cancelled" {
			return ErrShopRefund
		}
		var attempts []ShopRefundAttempt
		if err = lockForUpdate(tx).Where("refund_case_id = ?", result.ID).Order("id asc").Find(&attempts).Error; err != nil {
			return err
		}
		caseFields := map[string]any{}
		var attempt *ShopRefundAttempt
		if in.Action == "record" || in.Action == "verify" {
			for i := range attempts {
				if attempts[i].PublicID == in.AttemptPublicID {
					attempt = &attempts[i]
				}
			}
			if attempt == nil {
				return ErrShopNotFound
			}
			if attempt.State == "succeeded" || attempt.State == "failed" {
				return ErrShopRefund
			}
		}
		switch in.Action {
		case "attempt":
			if result.State != "approved" && result.State != "failed" {
				return ErrShopRefund
			}
			for _, old := range attempts {
				if old.State != "failed" {
					return ErrShopRefund
				}
			}
			adjusted, _, err := shopFundsAdjusted(tx, receipt, "")
			if err != nil {
				return err
			}
			if adjusted != 0 {
				return ErrShopRefund
			}
			priorRef := ""
			if len(attempts) > 0 {
				priorRef = attempts[len(attempts)-1].ProviderRefundRef
			}
			if err = shopCheckFundsEvidence(tx, receipt, in.EvidenceIDs, result.AmountMinor, priorRef, []string{"not_refunded", "refund_failed", "not_executed"}); err != nil {
				return err
			}
			rec, err := shopRecord("sra_", now)
			if err != nil {
				return err
			}
			ids, err := shopEvidenceJSON(tx, &receipt.ID, in.EvidenceIDs)
			if err != nil {
				return err
			}
			row := ShopRefundAttempt{ShopRecord: rec, RefundCaseID: result.ID, AttemptNo: int64(len(attempts) + 1), AmountMinor: result.AmountMinor, Currency: result.Currency, CurrencyExponent: result.CurrencyExponent, State: "registered", RegisteredBy: c.ActorID, RegisteredAt: now, EvidenceIDsJSON: ids, NextAction: "operate_original_channel"}
			if err = tx.Create(&row).Error; err != nil {
				return err
			}
			caseFields["state"] = "processing"
			caseFields["next_action"] = "operate_registered_attempt"
		case "record":
			if attempt.State != "registered" || in.OperatedAt <= 0 || in.OperatedAt < attempt.RegisteredAt || in.OperatedAt > now+300 {
				return ErrShopRefund
			}
			ids, err := shopEvidenceJSON(tx, &receipt.ID, in.EvidenceIDs)
			if err != nil {
				return err
			}
			if err = shopCAS(tx, &ShopRefundAttempt{}, attempt.ID, attempt.Version, now, map[string]any{"state": "unknown", "provider_refund_ref": in.ProviderRefundRef, "operated_at": in.OperatedAt, "evidence_ids_json": ids, "next_action": "verify_original_channel"}); err != nil {
				return err
			}
			caseFields["state"] = "unknown"
			caseFields["next_action"] = "verify_original_channel"
		case "verify":
			if !slices.Contains([]string{"succeeded", "failed", "unknown"}, in.Conclusion) {
				return ErrShopInvalid
			}
			ref := attempt.ProviderRefundRef
			if in.ProviderRefundRef != "" {
				if ref != "" && ref != in.ProviderRefundRef {
					return ErrShopEvidence
				}
				ref = in.ProviderRefundRef
			}
			ids, err := shopEvidenceJSON(tx, &receipt.ID, in.EvidenceIDs)
			if err != nil {
				return err
			}
			fields := map[string]any{"state": in.Conclusion, "provider_refund_ref": ref, "verified_by": c.ActorID, "verified_at": now, "evidence_ids_json": ids}
			if in.Conclusion == "succeeded" {
				if attempt.State != "unknown" || attempt.OperatedAt <= 0 {
					return ErrShopRefund
				}
				if !shopRequired(ref, 191) {
					return ErrShopEvidence
				}
				if err = shopCheckFundsEvidence(tx, receipt, in.EvidenceIDs, result.AmountMinor, ref, []string{"succeeded", "refund_succeeded"}); err != nil {
					return err
				}
				adjustment, err := shopAcceptAdjustment(tx, c, receipt, "refund", ref, result.AmountMinor, "succeeded", in.EvidenceIDs, "refund_attempt", now)
				if err != nil {
					return err
				}
				fields["adjustment_id"] = adjustment.ID
				fields["next_action"] = ""
				caseFields["next_action"] = ""
			} else if in.Conclusion == "failed" {
				if err = shopCheckFundsEvidence(tx, receipt, in.EvidenceIDs, result.AmountMinor, ref, []string{"failed", "refund_failed", "not_executed"}); err != nil {
					return err
				}
				fields["failure_code"] = "verified_failure"
				fields["next_action"] = "recheck_before_retry"
				caseFields["next_action"] = "recheck_before_retry"
			} else {
				if len(in.EvidenceIDs) == 0 {
					return ErrShopEvidence
				}
				fields["next_action"] = "verify_original_channel"
				caseFields["next_action"] = "verify_original_channel"
			}
			if err = shopCAS(tx, &ShopRefundAttempt{}, attempt.ID, attempt.Version, now, fields); err != nil {
				return err
			}
			caseFields["state"] = in.Conclusion
		case "cancel":
			adjusted, _, err := shopFundsAdjusted(tx, receipt, "")
			if err != nil {
				return err
			}
			if adjusted != 0 {
				return ErrShopRefund
			}
			for _, old := range attempts {
				if old.State != "failed" && old.State != "registered" {
					return ErrShopRefund
				}
			}
			if err = shopCheckFundsEvidence(tx, receipt, in.EvidenceIDs, result.AmountMinor, "", []string{"not_refunded", "refund_failed", "not_executed"}); err != nil {
				return err
			}
			for _, old := range attempts {
				if old.State == "registered" {
					if old.OperatedAt != 0 || old.ProviderRefundRef != "" {
						return ErrShopRefund
					}
					ids, err := shopEvidenceJSON(tx, &receipt.ID, in.EvidenceIDs)
					if err != nil {
						return err
					}
					if err = shopCAS(tx, &ShopRefundAttempt{}, old.ID, old.Version, now, map[string]any{"state": "failed", "failure_code": "cancelled_not_executed", "verified_by": c.ActorID, "verified_at": now, "evidence_ids_json": ids, "next_action": ""}); err != nil {
						return err
					}
				}
			}
			caseFields["state"] = "cancelled"
			caseFields["cancelled_by"] = c.ActorID
			caseFields["cancelled_at"] = now
			caseFields["cancel_reason"] = in.Reason
			caseFields["next_action"] = ""
		}
		if err = shopCAS(tx, &ShopRefundCase{}, result.ID, result.Version, now, caseFields); err != nil {
			return err
		}
		if err = shopCAS(tx, &ShopPaymentReceipt{}, receipt.ID, receipt.Version, now, map[string]any{}); err != nil {
			return err
		}
		if order != nil && order.EffectiveReceiptID != nil && *order.EffectiveReceiptID == receipt.ID {
			if err = shopUpdateOrder(tx, order, now, map[string]any{}); err != nil {
				return err
			}
		}
		if err = shopCaseEvent(tx, c, order, &receipt.ID, result.AftersaleID, &result.ID, action, "internal", in.Reason, in.EvidenceIDs); err != nil {
			return err
		}
		if in.PublicNote != "" {
			if err = shopCaseEvent(tx, c, order, &receipt.ID, result.AftersaleID, &result.ID, "refund_progress", "public", in.PublicNote, nil); err != nil {
				return err
			}
		}
		if err = tx.First(&result, result.ID).Error; err != nil {
			return err
		}
		return shopRemember(tx, c, action, in.RefundPublicID, "refund", result.PublicID, nil)
	})
	return &result, err
}

func shopAcceptAdjustment(tx *gorm.DB, c ShopCommand, receipt *ShopPaymentReceipt, kind, ref string, amount int64, state string, evidence []string, source string, now int64) (*ShopPaymentAdjustment, error) {
	if !slices.Contains([]string{"refund", "chargeback"}, kind) || !slices.Contains([]string{"pending", "succeeded", "failed", "disputed"}, state) || !shopRequired(ref, 191) || amount <= 0 || amount > receipt.AmountMinor {
		return nil, ErrShopRefund
	}
	key := shopDigest([]string{receipt.Provider, receipt.ProviderAccountRef, receipt.Environment, kind, ref})
	var row ShopPaymentAdjustment
	err := lockForUpdate(tx).Where("adjustment_key = ?", key).First(&row).Error
	if err == nil {
		if row.ReceiptID != receipt.ID || row.Kind != kind || row.AmountMinor != amount || row.Currency != receipt.Currency || row.CurrencyExponent != receipt.CurrencyExponent || row.ProviderAdjustmentRef != ref {
			return nil, ErrShopEvidence
		}
		if row.State == "succeeded" && state != "succeeded" || row.State == "failed" && state != "failed" {
			return nil, ErrShopEvidence
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if state != "failed" {
		adjusted, _, err := shopFundsAdjusted(tx, receipt, key)
		if err != nil {
			return nil, err
		}
		if adjusted > receipt.AmountMinor-amount {
			return nil, ErrShopRefund
		}
	}
	ids, err := shopEvidenceJSON(tx, &receipt.ID, evidence)
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		rec, err := shopRecord("spa_", now)
		if err != nil {
			return nil, err
		}
		row = ShopPaymentAdjustment{ShopRecord: rec, ReceiptID: receipt.ID, AdjustmentKey: key, Kind: kind, ProviderAdjustmentRef: ref, AmountMinor: amount, Currency: receipt.Currency, CurrencyExponent: receipt.CurrencyExponent, State: state, Source: source, EvidenceIDsJSON: ids, VerifiedBy: c.ActorID, VerifiedAt: now}
		if err = tx.Create(&row).Error; err != nil {
			return nil, err
		}
	} else {
		if err = shopCAS(tx, &ShopPaymentAdjustment{}, row.ID, row.Version, now, map[string]any{"state": state, "evidence_ids_json": ids, "verified_by": c.ActorID, "verified_at": now}); err != nil {
			return nil, err
		}
		if err = tx.First(&row, row.ID).Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}

func RecordShopPaymentAdjustment(db *gorm.DB, in ShopAdjustmentInput) (*ShopPaymentAdjustment, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result *ShopPaymentAdjustment
	action := "payment-adjustment"
	if in.AdjustmentPublicID != "" {
		action = "payment-adjustment-review"
	}
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, action, in.ReceiptPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			var row ShopPaymentAdjustment
			err = tx.Where("public_id = ?", k.ResourcePublicID).First(&row).Error
			result = &row
			return err
		}
		order, receipt, err := shopLockReceipt(tx, in.ReceiptPublicID, now)
		if err != nil {
			return err
		}
		if err = shopVersion(receipt.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if !shopRequired(in.Reason, 2000) || in.Currency != receipt.Currency || in.CurrencyExponent != receipt.CurrencyExponent {
			return ErrShopInvalid
		}
		kind := in.Kind
		if kind == "external_refund" {
			kind = "refund"
		}
		if in.AdjustmentPublicID != "" {
			var old ShopPaymentAdjustment
			if err = tx.Where("public_id = ? AND receipt_id = ?", in.AdjustmentPublicID, receipt.ID).First(&old).Error; err != nil {
				return shopNotFound(err)
			}
			if kind != old.Kind || in.ProviderAdjustmentRef != old.ProviderAdjustmentRef || in.AmountMinor != old.AmountMinor {
				return ErrShopEvidence
			}
		}
		statuses := []string{in.State}
		if kind == "refund" {
			statuses = append(statuses, "refund_"+in.State)
		}
		if kind == "chargeback" {
			statuses = append(statuses, "chargeback_"+in.State)
		}
		if err = shopCheckFundsEvidence(tx, receipt, in.EvidenceIDs, in.AmountMinor, in.ProviderAdjustmentRef, statuses); err != nil {
			return err
		}
		result, err = shopAcceptAdjustment(tx, c, receipt, kind, in.ProviderAdjustmentRef, in.AmountMinor, in.State, in.EvidenceIDs, "merchant_console", now)
		if err != nil {
			return err
		}
		if err = shopCAS(tx, &ShopPaymentReceipt{}, receipt.ID, receipt.Version, now, map[string]any{}); err != nil {
			return err
		}
		if order != nil && order.EffectiveReceiptID != nil && *order.EffectiveReceiptID == receipt.ID {
			if err = shopUpdateOrder(tx, order, now, map[string]any{}); err != nil {
				return err
			}
		}
		if err = shopCaseEvent(tx, c, order, &receipt.ID, nil, nil, action, "internal", in.Reason, in.EvidenceIDs); err != nil {
			return err
		}
		return shopRemember(tx, c, action, in.ReceiptPublicID, "adjustment", result.PublicID, nil)
	})
	return result, err
}

func ReviewShopPaymentReceipt(db *gorm.DB, in ShopReceiptReviewInput) (*ShopPaymentReceipt, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result *ShopPaymentReceipt
	if in.Action != "assign" && in.Action != "reconcile" {
		return nil, ErrShopInvalid
	}
	action := "receipt-" + in.Action
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, action, in.ReceiptPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			var row ShopPaymentReceipt
			err = tx.Where("public_id = ?", k.ResourcePublicID).First(&row).Error
			result = &row
			return err
		}
		order, row, err := shopLockReceipt(tx, in.ReceiptPublicID, now)
		if err != nil {
			return err
		}
		if err = shopVersion(row.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if !shopRequired(in.Reason, 2000) {
			return ErrShopInvalid
		}
		fields := map[string]any{}
		if in.Action == "assign" {
			if err = shopAuthorize(tx, ShopCommand{ActorID: in.AssigneeID, TenantID: PlatformTenantID, Admin: true}, true); err != nil {
				return err
			}
			fields["review_owner_id"] = in.AssigneeID
		} else {
			fields["next_retry_at"] = now
			if err = shopEnqueue(tx, ShopTopicReceipt, shopEventKey("receipt-reconcile", row.ID, row.Version+1), row.PublicID, map[string]any{"schema_version": 1, "receipt_id": row.ID}, now); err != nil {
				return err
			}
		}
		if err = shopCAS(tx, &ShopPaymentReceipt{}, row.ID, row.Version, now, fields); err != nil {
			return err
		}
		if err = shopCaseEvent(tx, c, order, &row.ID, nil, nil, action, "internal", in.Reason, nil); err != nil {
			return err
		}
		if err = tx.First(row, row.ID).Error; err != nil {
			return err
		}
		result = row
		return shopRemember(tx, c, action, in.ReceiptPublicID, "receipt", row.PublicID, nil)
	})
	return result, err
}
