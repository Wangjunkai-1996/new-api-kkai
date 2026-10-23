package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WalletPaymentReceipt struct {
	ID                  int64  `gorm:"primaryKey"`
	ReceiptKey          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	Provider            string `gorm:"type:varchar(50);not null"`
	ProviderAccountRef  string `gorm:"type:varchar(128);not null"`
	ProviderEnvironment string `gorm:"type:varchar(32);not null"`
	TransactionID       string `gorm:"type:varchar(255);not null"`
	TopUpID             *int   `gorm:"index"`
	AmountMinor         int64
	Currency            string `gorm:"type:varchar(8);not null"`
	CurrencyExponent    int
	PaidAt              int64
	ReceivedAt          int64
	EvidenceSource      string `gorm:"type:varchar(32)"`
	EvidenceRef         string `gorm:"type:varchar(255)"`
	VerificationStatus  string `gorm:"type:varchar(32)"`
	PricingVerified     bool   `gorm:"type:boolean"`
	SettlementKind      string `gorm:"type:varchar(32)"`
	ProcessingStatus    string `gorm:"type:varchar(32);index"`
	ExceptionReason     string `gorm:"type:varchar(64)"`
	ReviewOwner         string `gorm:"type:varchar(128)"`
	NextRetryAt         int64  `gorm:"index"`
}

func (WalletPaymentReceipt) TableName() string { return "wallet_payment_receipts" }

type WalletPaymentEvent struct {
	ID           int64 `gorm:"primaryKey"`
	ActorID      int
	Source       string `gorm:"type:varchar(32)"`
	EvidenceRef  string `gorm:"type:varchar(255)"`
	ReceiptID    *int64
	EvidenceHash string `gorm:"type:varchar(64)"`
	TopUpID      int    `gorm:"not null;index:idx_wallet_payment_event,priority:1"`
	Kind         string `gorm:"type:varchar(32);not null"`
	Status       string `gorm:"type:varchar(32);not null"`
	ReasonCode   string `gorm:"type:varchar(64);not null"`
	CreatedAt    int64  `gorm:"not null;index:idx_wallet_payment_event,priority:2"`
}

func (WalletPaymentEvent) TableName() string { return "wallet_payment_events" }
func recordWalletPaymentEventTx(tx *gorm.DB, order *TopUp, kind, status, reason string, actorID int, source, evidence string) error {
	return tx.Create(&WalletPaymentEvent{TopUpID: order.Id, ActorID: actorID, Source: source, EvidenceRef: evidence, Kind: kind, Status: status, ReasonCode: reason, CreatedAt: common.GetTimestamp()}).Error
}
func WalletPaymentReceiptKey(provider, account, environment, transaction string) string {
	// A JSON tuple avoids identity ambiguity without assuming punctuation
	// restrictions for externally issued transaction identities.
	data, _ := common.Marshal([]string{strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(account), strings.ToLower(strings.TrimSpace(environment)), strings.TrimSpace(transaction)})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
func SaveWalletPaymentReceipt(receipt *WalletPaymentReceipt) (*WalletPaymentReceipt, error) {
	if receipt == nil {
		return nil, ErrResellerFinanceInvalidInput
	}
	clone := *receipt
	clone.Provider = strings.ToLower(strings.TrimSpace(clone.Provider))
	clone.ProviderEnvironment = strings.ToLower(strings.TrimSpace(clone.ProviderEnvironment))
	clone.ProviderAccountRef = strings.TrimSpace(clone.ProviderAccountRef)
	clone.TransactionID = strings.TrimSpace(clone.TransactionID)
	clone.Currency = strings.ToUpper(strings.TrimSpace(clone.Currency))
	if clone.Provider == "" || len(clone.Provider) > 50 || clone.ProviderAccountRef == "" || len(clone.ProviderAccountRef) > 128 || clone.ProviderEnvironment == "" || len(clone.ProviderEnvironment) > 32 || clone.TransactionID == "" || len(clone.TransactionID) > 255 || clone.Currency == "" || len(clone.Currency) > 8 || clone.CurrencyExponent < 0 || clone.CurrencyExponent > 6 || clone.AmountMinor < 0 || clone.PaidAt < 0 || clone.VerificationStatus != "verified" || clone.EvidenceRef == "" || len(clone.EvidenceRef) > 255 {
		return nil, ErrResellerFinanceInvalidInput
	}
	clone.ID = 0
	clone.ReceiptKey = WalletPaymentReceiptKey(clone.Provider, clone.ProviderAccountRef, clone.ProviderEnvironment, clone.TransactionID)
	clone.ReceivedAt = common.GetTimestamp()
	clone.ProcessingStatus = "pending"
	clone.ExceptionReason = ""
	clone.ReviewOwner = ""
	clone.NextRetryAt = clone.ReceivedAt
	if clone.TopUpID == nil {
		clone.ProcessingStatus = "review"
		clone.ExceptionReason = "UNMATCHED_PAYMENT"
		clone.ReviewOwner = "payment-operations"
		clone.NextRetryAt = 0
	}
	// This short transaction commits the paid fact before any accounting work.
	result := DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "receipt_key"}}, DoNothing: true}).Create(&clone)
	if result.Error != nil {
		return nil, result.Error
	}
	var stored WalletPaymentReceipt
	if err := DB.Where("receipt_key = ?", clone.ReceiptKey).First(&stored).Error; err != nil {
		return nil, err
	}
	sameOrder := stored.TopUpID == nil && clone.TopUpID == nil || stored.TopUpID != nil && clone.TopUpID != nil && *stored.TopUpID == *clone.TopUpID
	if stored.Provider != clone.Provider || stored.ProviderAccountRef != clone.ProviderAccountRef || stored.ProviderEnvironment != clone.ProviderEnvironment || stored.TransactionID != clone.TransactionID || stored.AmountMinor != clone.AmountMinor || stored.Currency != clone.Currency || stored.CurrencyExponent != clone.CurrencyExponent || !sameOrder {
		// Keep the accepted paid fact immutable. A conflicting replay is its own
		// audit event and must not poison a legitimate receipt's credit retry.
		encoded, err := common.Marshal(clone)
		if err != nil {
			return &stored, err
		}
		digest := sha256.Sum256(encoded)
		topUpID := 0
		if clone.TopUpID != nil {
			topUpID = *clone.TopUpID
		} else if stored.TopUpID != nil {
			topUpID = *stored.TopUpID
		}
		if err := DB.Create(&WalletPaymentEvent{TopUpID: topUpID, ReceiptID: &stored.ID, EvidenceHash: hex.EncodeToString(digest[:]), Source: clone.EvidenceSource, EvidenceRef: clone.EvidenceRef, Kind: "receipt_conflict", Status: "review_required", ReasonCode: "RECEIPT_IDENTITY_CONFLICT", CreatedAt: common.GetTimestamp()}).Error; err != nil {
			return &stored, err
		}
		return &stored, ErrWalletPaymentEvidence
	}
	// Authentication can add a pricing proof to the same immutable payment fact;
	// it cannot change the accepted amount, currency, merchant, or order binding.
	if clone.PricingVerified && !stored.PricingVerified {
		if err := DB.Model(&stored).Where("pricing_verified = ? OR pricing_verified IS NULL", false).Update("pricing_verified", true).Error; err != nil {
			return &stored, err
		}
		stored.PricingVerified = true
	}
	if stored.PaidAt == 0 && clone.PaidAt > 0 {
		if err := DB.Model(&stored).Where("paid_at = ? OR paid_at IS NULL", 0).Update("paid_at", clone.PaidAt).Error; err != nil {
			return &stored, err
		}
		stored.PaidAt = clone.PaidAt
	}
	return &stored, nil
}

type FinalizeVerifiedWalletTopUpInput struct {
	TradeNo string
	Receipt WalletPaymentReceipt
	// PricingVerified is set only after authenticated provider retrieval confirms
	// the original price/product/quantity and supported adjustments.
	PricingVerified bool
	UserPatch       TopUpUserPatch
}

func FinalizeVerifiedWalletTopUp(input FinalizeVerifiedWalletTopUpInput) (*FinalizeTopUpResult, error) {
	input.Receipt.PricingVerified = input.PricingVerified
	receipt, err := SaveWalletPaymentReceipt(&input.Receipt)
	if err != nil {
		return nil, err
	}
	var result *FinalizeTopUpResult
	err = walletTransaction(func(tx *gorm.DB) error {
		order, err := lockTopUpByTradeNo(tx, input.TradeNo)
		if err != nil {
			return err
		}
		stored := &WalletPaymentReceipt{}
		if err := lockForUpdate(tx).First(stored, receipt.ID).Error; err != nil {
			return err
		}
		if stored.TopUpID == nil || *stored.TopUpID != order.Id || stored.Provider != order.PaymentProvider || stored.ProviderAccountRef != order.ProviderAccountRef || stored.ProviderEnvironment != order.ProviderEnvironment || stored.Currency != order.Currency || stored.CurrencyExponent != order.CurrencyExponent || stored.VerificationStatus != "verified" || (stored.ExceptionReason != "" && stored.ExceptionReason != "CREDIT_REVIEW" && stored.ExceptionReason != "PAYMENT_PROOF_INCOMPLETE") {
			return ErrWalletPaymentEvidence
		}
		if !stored.PricingVerified || stored.PaidAt == 0 {
			return ErrWalletPaymentProofIncomplete
		}
		if order.PricingMode == "channel_adjusted" {
			if !stored.PricingVerified || stored.AmountMinor == 0 && stored.SettlementKind != "no_payment_required" {
				return ErrWalletPaymentEvidence
			}
		} else if stored.AmountMinor <= 0 || stored.AmountMinor != order.PaidMinor {
			return ErrWalletPaymentEvidence
		}
		if order.TenantID > 0 && stored.AmountMinor != order.PaidMinor {
			return ErrWalletPaymentEvidence
		}
		if stored.PaidAt < order.CreateTime || stored.PaidAt > common.GetTimestamp() {
			return ErrWalletPaymentEvidence
		}
		if stored.ProcessingStatus == "credited" {
			if order.Status != common.TopUpStatusSuccess {
				return ErrWalletPaymentEvidence
			}
			result = &FinalizeTopUpResult{TopUp: *order, AlreadyCompleted: true}
			return nil
		}
		var other int64
		if err := tx.Model(&WalletPaymentReceipt{}).Where("top_up_id = ? AND id <> ? AND processing_status = ?", order.Id, stored.ID, "credited").Count(&other).Error; err != nil {
			return err
		}
		if other > 0 {
			return ErrWalletPaymentEvidence
		}
		if order.CreditedQuota <= 0 || order.WalletLifecycleVersion != 1 {
			return ErrWalletPaymentEvidence
		}
		// Only verified receipts can reopen local terminal states. Unverified manual
		// completion continues to require pending in the existing finalizer.
		if order.Status != common.TopUpStatusPending && order.Status != common.TopUpStatusSuccess && order.Status != "cancelled" && order.Status != common.TopUpStatusExpired && order.Status != common.TopUpStatusFailed && order.Status != "review_required" && order.Status != "credit_review" {
			return ErrTopUpStatusInvalid
		}
		order.ProviderPaymentState = "paid"
		amount := stored.AmountMinor
		order.PayableMinor = &amount
		order.CheckClaimToken = ""
		order.CheckClaimUntil = 0
		order.NextCheckAt = 0
		order.ReasonCode = ""
		order.Version++
		if order.PaymentProvider == PaymentProviderEpay && stored.Currency == ResellerCurrencyCNY && stored.AmountMinor > 0 {
			if err := recordInvoiceTopUpReceiptTx(tx, *order, TopUpPaymentReceipt{ProviderReceiptID: stored.TransactionID, AmountMinor: stored.AmountMinor, Currency: stored.Currency}, stored.PaidAt); err != nil {
				return err
			}
		}
		if order.Status == common.TopUpStatusSuccess {
			result = &FinalizeTopUpResult{TopUp: *order, AlreadyCompleted: true}
			if err := tx.Save(order).Error; err != nil {
				return err
			}
		} else {
			result, err = completeLockedTopUpTx(tx, order, FinalizeTopUpInput{TradeNo: input.TradeNo, Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
				return TopUpCompletion{QuotaDelta: topUp.CreditedQuota, UserPatch: input.UserPatch}, nil
			}}, stored.PaidAt)
			if err != nil {
				return err
			}
		}
		if err := tx.Model(stored).Updates(map[string]any{"processing_status": "credited", "exception_reason": "", "review_owner": "", "next_retry_at": 0}).Error; err != nil {
			return err
		}
		return recordWalletPaymentEventTx(tx, order, "payment_received", "success", "", 0, stored.EvidenceSource, stored.EvidenceRef)
	})
	if err != nil {
		reason := "CREDIT_REVIEW"
		if errors.Is(err, ErrWalletPaymentEvidence) {
			reason = "PAYMENT_EVIDENCE_MISMATCH"
		}
		if errors.Is(err, ErrWalletPaymentProofIncomplete) {
			reason = "PAYMENT_PROOF_INCOMPLETE"
		}
		// A failed credit cannot roll back the committed paid fact. Never overwrite
		// another worker's completed settlement while recording this exception.
		saved := DB.Model(&WalletPaymentReceipt{}).Where("id = ? AND processing_status <> ?", receipt.ID, "credited").Updates(map[string]any{"processing_status": "review", "exception_reason": reason, "review_owner": "payment-operations", "next_retry_at": common.GetTimestamp() + 300})
		if saved.Error != nil {
			return nil, errors.Join(err, saved.Error)
		}
		if receipt.TopUpID != nil {
			marked := DB.Model(&TopUp{}).Where("id = ? AND status <> ?", *receipt.TopUpID, common.TopUpStatusSuccess).Updates(map[string]any{"status": "credit_review", "review_owner": "payment-operations", "reason_code": "CREDIT_REVIEW", "review_started_at": common.GetTimestamp(), "next_check_at": common.GetTimestamp() + 300, "version": gorm.Expr("COALESCE(version,0) + 1")})
			if marked.Error != nil {
				return nil, errors.Join(err, marked.Error)
			}
		}
		return nil, err
	}
	invalidateTopUpUserCache(result)
	return result, nil
}

// ClaimWalletTopUpCheck uses a lease plus version CAS; process-local locks are
// never the authority. Replays/restarts can recover an expired lease.
func ClaimWalletTopUpCheck(trade string, now int64) (*TopUp, error) {
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		var err error
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.CheckClaimUntil > now {
			return ErrWalletOrderChanged
		}
		if order.Status == common.TopUpStatusSuccess {
			return ErrTopUpStatusInvalid
		}
		token, err := walletID()
		if err != nil {
			return err
		}
		updated := tx.Model(order).Where("COALESCE(version,0) = ? AND COALESCE(check_claim_until,0) <= ?", order.Version, now).Updates(map[string]any{"check_claim_token": token, "check_claim_until": now + 60, "check_attempts": order.CheckAttempts + 1, "version": order.Version + 1})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWalletOrderChanged
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}

type WalletTopUpCheckResult struct {
	ActorID              int
	EvidenceSource       string
	ClaimToken           string
	ExpectedVersion      int64
	ProviderPaymentState string
	ReasonCode           string
	NextCheckAt          int64
	// ReliableClosed requires authenticated evidence that payment is unpaid AND
	// the original session is closed; a timeout or a client clock is insufficient.
	ReliableClosed   bool
	CloseEvidenceRef string
	TerminalStatus   string
	PayableMinor     *int64
}

func ApplyWalletTopUpCheck(trade string, input WalletTopUpCheckResult) (*TopUp, error) {
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		var err error
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if input.ClaimToken == "" || order.CheckClaimToken != input.ClaimToken || order.Version != input.ExpectedVersion {
			return ErrWalletOrderChanged
		}
		if input.ProviderPaymentState == "paid" {
			return ErrWalletPaymentEvidence
		}
		now := common.GetTimestamp()
		updates := map[string]any{"last_checked_at": now, "next_check_at": input.NextCheckAt, "check_claim_token": "", "check_claim_until": 0, "version": order.Version + 1}
		kind, eventStatus, eventReason := "provider_checked", input.TerminalStatus, input.ReasonCode
		if input.PayableMinor != nil && *input.PayableMinor < 0 {
			return ErrResellerFinanceInvalidInput
		}
		holdReceipt := order.ProviderPaymentState == "paid" || order.Status == "credit_review"
		if !holdReceipt {
			var receipts int64
			if err := tx.Model(&WalletPaymentReceipt{}).Where("top_up_id = ? AND verification_status = ?", order.Id, "verified").Count(&receipts).Error; err != nil {
				return err
			}
			holdReceipt = receipts > 0
		}
		if holdReceipt {
			// A later negative query cannot erase a paid fact or resolve a receipt
			// exception. Keep the original flag: a mismatch hold is not proof of paid.
			updates["status"] = "credit_review"
			if input.NextCheckAt <= 0 {
				updates["next_check_at"] = now + 300
			}
			if order.ReasonCode == "" {
				updates["reason_code"] = "CREDIT_REVIEW"
			}
			if order.ReviewOwner == "" {
				updates["review_owner"] = "payment-operations"
			}
			if order.ReviewStartedAt == 0 {
				updates["review_started_at"] = now
			}
			kind, eventStatus, eventReason = "provider_check_held", "credit_review", "CREDIT_REVIEW_RETAINED"
		} else {
			updates["provider_payment_state"] = input.ProviderPaymentState
			updates["reason_code"] = input.ReasonCode
			if input.PayableMinor != nil {
				updates["payable_minor"] = *input.PayableMinor
			}
			if input.ReliableClosed {
				if input.CloseEvidenceRef == "" || input.ProviderPaymentState != "unpaid" && input.ProviderPaymentState != "failed" || input.TerminalStatus != "cancelled" && input.TerminalStatus != common.TopUpStatusExpired && input.TerminalStatus != common.TopUpStatusFailed {
					return ErrWalletPaymentEvidence
				}
				updates["closed_at"] = now
				updates["close_evidence_ref"] = input.CloseEvidenceRef
				updates["status"] = input.TerminalStatus
				updates["next_check_at"] = 0
			} else if input.ReasonCode == "REVIEW_REQUIRED" || input.ReasonCode == "CREDIT_REVIEW" {
				updates["review_started_at"] = now
				updates["review_owner"] = "payment-operations"
				if input.ReasonCode == "CREDIT_REVIEW" {
					updates["status"] = "credit_review"
				} else {
					updates["status"] = "review_required"
				}
			}
		}
		if err := tx.Model(order).Updates(updates).Error; err != nil {
			return err
		}
		if err := recordWalletPaymentEventTx(tx, order, kind, eventStatus, eventReason, input.ActorID, input.EvidenceSource, input.CloseEvidenceRef); err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}

func GetWalletPaymentEvents(topUpID int) ([]WalletPaymentEvent, error) {
	var events []WalletPaymentEvent
	err := DB.Where("top_up_id = ?", topUpID).Order("id DESC").Limit(100).Find(&events).Error
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, err
}

// RetryVerifiedWalletTopUpReceipt resumes the accounting step from durable
// authenticated evidence after a crash; it never fabricates a fresh query.
func RetryVerifiedWalletTopUpReceipt(receiptID int64) (*FinalizeTopUpResult, error) {
	var receipt WalletPaymentReceipt
	if err := DB.First(&receipt, receiptID).Error; err != nil {
		return nil, err
	}
	if receipt.VerificationStatus != "verified" || receipt.TopUpID == nil {
		return nil, ErrWalletPaymentEvidence
	}
	var order TopUp
	if err := DB.First(&order, *receipt.TopUpID).Error; err != nil {
		return nil, err
	}
	return FinalizeVerifiedWalletTopUp(FinalizeVerifiedWalletTopUpInput{TradeNo: order.TradeNo, Receipt: receipt, PricingVerified: receipt.PricingVerified})
}

func GetWalletPaymentReceipts(topUpID int) ([]WalletPaymentReceipt, error) {
	var receipts []WalletPaymentReceipt
	err := DB.Where("top_up_id = ?", topUpID).Order("id DESC").Limit(100).Find(&receipts).Error
	return receipts, err
}
