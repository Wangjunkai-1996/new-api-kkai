package model

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MembershipPaymentPending         = "pending"
	MembershipPaymentVerified        = "verified"
	MembershipPaymentComplete        = "complete"
	MembershipPaymentReview          = "review"
	MembershipPaymentMerchantQuery   = "merchant_query"
	MembershipLegacyCredentialReview = "original payment credentials unavailable"
)

var (
	ErrMembershipPaymentSchema     = errors.New("membership payment schema is not ready")
	ErrMembershipPaymentClaim      = errors.New("membership payment check claim changed")
	ErrMembershipPaymentEvidence   = errors.New("membership payment evidence requires review")
	membershipPaymentSchemaChecker struct {
		sync.RWMutex
		check func(*gorm.DB) error
	}
)

// Separate tables keep legacy membership reads usable before explicit expansion.
type MembershipPaymentCheck struct {
	OrderID            int64  `gorm:"primaryKey;autoIncrement:false"`
	TradeNo            string `gorm:"type:varchar(128);not null;uniqueIndex:ux_membership_payment_check_trade"`
	UserID             int    `gorm:"not null;index:idx_membership_payment_check_user"`
	MerchantID         string `gorm:"type:varchar(128);not null"`
	EncryptedSnapshot  string `json:"-" gorm:"type:text;not null"`
	Status             string `gorm:"type:varchar(24);not null;index:idx_membership_payment_check_due,priority:1"`
	NextCheckAt        int64  `gorm:"not null;index:idx_membership_payment_check_due,priority:2"`
	ClaimToken         string `json:"-" gorm:"type:varchar(64);not null"`
	ClaimUntil         int64  `gorm:"not null;index:idx_membership_payment_check_due,priority:3"`
	Attempts           int    `gorm:"not null"`
	LastCheckedAt      int64  `gorm:"not null"`
	LastSource         string `gorm:"type:varchar(32);not null"`
	LastError          string `gorm:"type:varchar(512);not null"`
	CloseReason        string `gorm:"type:varchar(32);not null"`
	CallbackReceivedAt int64  `gorm:"not null"`
	CreatedAt          int64  `gorm:"not null"`
	UpdatedAt          int64  `gorm:"not null"`
}

func (MembershipPaymentCheck) TableName() string { return "kkai_membership_payment_checks" }

// Only authenticated merchant-query results enter this table. Callback hints
// merely wake a check and cannot reserve a receipt key or authorize fulfillment.
type MembershipPaymentEvidence struct {
	ID              int64  `gorm:"primaryKey"`
	OrderID         int64  `gorm:"not null;index:idx_membership_payment_evidence_order"`
	ReceiptKey      string `gorm:"type:varchar(191);not null;uniqueIndex:ux_membership_payment_evidence_receipt"`
	ReceiptID       string `gorm:"type:varchar(128);not null"`
	MerchantID      string `gorm:"type:varchar(128);not null"`
	AmountMinor     int64  `gorm:"not null"`
	Currency        string `gorm:"type:varchar(8);not null"`
	ProviderPaidAt  int64  `gorm:"not null"`
	VerifiedAt      int64  `gorm:"not null"`
	Source          string `gorm:"type:varchar(32);not null"`
	Status          string `gorm:"type:varchar(24);not null;index:idx_membership_payment_evidence_retry,priority:1"`
	RetryAt         int64  `gorm:"not null;index:idx_membership_payment_evidence_retry,priority:2"`
	Attempts        int    `gorm:"not null"`
	LastError       string `gorm:"type:varchar(512);not null"`
	ValidationError string `gorm:"type:varchar(512);not null"`
	CreatedAt       int64  `gorm:"not null"`
	UpdatedAt       int64  `gorm:"not null"`
}

func (MembershipPaymentEvidence) TableName() string { return "kkai_membership_payment_evidence" }

type MembershipVerifiedPaymentInput struct {
	OrderID         int64
	ClaimToken      string
	MerchantID      string
	ReceiptID       string
	AmountMinor     int64
	Currency        string
	ProviderPaidAt  int64
	ValidationError string
}

type MembershipPaymentCheckResult struct {
	Status      string
	Reason      string
	NextCheckAt int64
}

func MembershipPaymentModels() []any {
	return []any{&MembershipPaymentCheck{}, &MembershipPaymentEvidence{}}
}

func RegisterMembershipPaymentSchemaChecker(check func(*gorm.DB) error) {
	membershipPaymentSchemaChecker.Lock()
	defer membershipPaymentSchemaChecker.Unlock()
	membershipPaymentSchemaChecker.check = check
}

func MembershipPaymentSchemaAvailable() bool {
	if DB == nil {
		return false
	}
	membershipPaymentSchemaChecker.RLock()
	check := membershipPaymentSchemaChecker.check
	membershipPaymentSchemaChecker.RUnlock()
	return check != nil && check(DB) == nil
}

func EnsureMembershipPaymentCheck(ctx context.Context, orderID int64, merchantID, encryptedSnapshot string) (*MembershipPaymentCheck, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 || merchantID == "" || len(merchantID) > 128 || encryptedSnapshot == "" || len(encryptedSnapshot) > 65535 {
		return nil, ErrMembershipInvalidInput
	}
	var order MembershipOrder
	if err := DB.WithContext(ctx).First(&order, orderID).Error; err != nil {
		return nil, err
	}
	var result *MembershipPaymentCheck
	err := MembershipTransaction(ctx, func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, order.UserID); err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&order, orderID).Error; err != nil {
			return err
		}
		var err error
		result, err = ensureMembershipPaymentCheckTx(tx, &order, merchantID, encryptedSnapshot)
		if err == nil && order.ExceptionReason == MembershipLegacyCredentialReview && order.PaidAt == 0 && (order.Status == MembershipOrderPending || order.Status == MembershipOrderClosed) {
			err = tx.Model(&order).Update("exception_reason", "").Error
		}
		return err
	})
	return result, err
}

func ensureMembershipPaymentCheckTx(tx *gorm.DB, order *MembershipOrder, merchantID, encryptedSnapshot string) (*MembershipPaymentCheck, error) {
	if order.Provider != PaymentProviderEpay || order.Currency != "CNY" || merchantID == "" || len(merchantID) > 128 || encryptedSnapshot == "" || len(encryptedSnapshot) > 65535 {
		return nil, ErrMembershipInvalidInput
	}
	now := common.GetTimestamp()
	check := MembershipPaymentCheck{OrderID: order.ID, TradeNo: order.TradeNo, UserID: order.UserID, MerchantID: merchantID, EncryptedSnapshot: encryptedSnapshot, Status: MembershipPaymentPending, NextCheckAt: now + 30, LastSource: "checkout", CreatedAt: now, UpdatedAt: now}
	if order.Status == MembershipOrderSuccess {
		check.Status, check.NextCheckAt = MembershipPaymentComplete, 0
	}
	if order.Status == MembershipOrderException || order.ResolvedAt > 0 {
		check.Status, check.NextCheckAt = MembershipPaymentReview, 0
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&check).Error; err != nil {
		return nil, err
	}
	if err := lockForUpdate(tx).First(&check, "order_id = ?", order.ID).Error; err != nil {
		return nil, err
	}
	if check.MerchantID != merchantID || check.TradeNo != order.TradeNo || check.UserID != order.UserID {
		return nil, ErrMembershipConflict
	}
	return &check, nil
}

func GetMembershipPaymentCheck(ctx context.Context, orderID int64) (*MembershipPaymentCheck, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	var check MembershipPaymentCheck
	err := DB.WithContext(ctx).First(&check, "order_id = ?", orderID).Error
	return &check, err
}

// Waking only schedules a merchant query; caller-supplied payment facts are not accepted.
func WakeMembershipPaymentCheck(ctx context.Context, orderID int64, source string) error {
	if !MembershipPaymentSchemaAvailable() {
		return ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 || (source != "callback" && source != "self" && source != "admin") {
		return ErrMembershipInvalidInput
	}
	now := common.GetTimestamp()
	updates := map[string]any{"last_source": source, "updated_at": now, "next_check_at": gorm.Expr("CASE WHEN last_checked_at + 30 > ? THEN last_checked_at + 30 ELSE ? END", now, now)}
	if source == "callback" {
		updates["callback_received_at"] = now
	}
	result := DB.WithContext(ctx).Model(&MembershipPaymentCheck{}).Where("order_id = ? AND status = ?", orderID, MembershipPaymentPending).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		check, err := GetMembershipPaymentCheck(ctx, orderID)
		if err != nil {
			return err
		}
		if check.Status == MembershipPaymentPending {
			return nil
		} // MySQL may report no changed rows for an identical wake.
		// A check review can mean missing credentials, without any confirmed
		// payment. Do not return the "payment recorded" business-review error.
		return ErrMembershipConflict
	}
	return nil
}

// A missing historical merchant credential is an operational verification
// review, not a paid receipt or a paid_exception. The marker is cleared only
// when the original checkout later proves a usable merchant snapshot.
func RecordMembershipLegacyReview(ctx context.Context, orderID int64, reason string) error {
	if !MembershipPaymentSchemaAvailable() {
		return ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 || reason != MembershipLegacyCredentialReview {
		return ErrMembershipInvalidInput
	}
	var order MembershipOrder
	if err := DB.WithContext(ctx).First(&order, orderID).Error; err != nil {
		return err
	}
	return MembershipTransaction(ctx, func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, order.UserID); err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&order, orderID).Error; err != nil {
			return err
		}
		if order.Provider != PaymentProviderEpay || order.PaidAt != 0 || order.ResolvedAt != 0 || order.Resolution != "" || (order.Status != MembershipOrderPending && order.Status != MembershipOrderClosed) {
			return nil
		}
		var check MembershipPaymentCheck
		err := tx.First(&check, "order_id = ?", order.ID).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.ExceptionReason != "" && order.ExceptionReason != MembershipLegacyCredentialReview {
			return ErrMembershipConflict
		}
		return tx.Model(&order).Update("exception_reason", reason).Error
	})
}

func ListDueMembershipPaymentChecks(ctx context.Context, now int64, limit int) ([]MembershipPaymentCheck, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || now <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var checks []MembershipPaymentCheck
	err := DB.WithContext(ctx).Where("status = ? AND claim_until <= ? AND (next_check_at <= ? OR claim_token <> ?)", MembershipPaymentPending, now, now, "").Order("CASE WHEN claim_token <> '' THEN 0 ELSE 1 END, next_check_at, order_id").Limit(limit).Find(&checks).Error
	return checks, err
}

func ClaimMembershipPaymentCheck(ctx context.Context, orderID, now int64) (*MembershipPaymentCheck, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 || now <= 0 || now > common.GetTimestamp()+60 {
		return nil, ErrMembershipInvalidInput
	}
	token, err := walletID()
	if err != nil {
		return nil, err
	}
	// The sentinel distinguishes an untouched claim from a wake that arrived
	// during upstream I/O. Expired nonempty claims remain discoverable below.
	claimed := DB.WithContext(ctx).Model(&MembershipPaymentCheck{}).Where("order_id = ? AND status = ? AND claim_until <= ? AND (next_check_at <= ? OR claim_token <> ?)", orderID, MembershipPaymentPending, now, now, "").Updates(map[string]any{"claim_token": token, "claim_until": now + 60, "next_check_at": int64(math.MaxInt64), "attempts": gorm.Expr("attempts + 1"), "updated_at": now})
	if claimed.Error != nil {
		return nil, claimed.Error
	}
	if claimed.RowsAffected != 1 {
		return nil, ErrMembershipPaymentClaim
	}
	var check MembershipPaymentCheck
	err = DB.WithContext(ctx).Where("order_id = ? AND claim_token = ?", orderID, token).First(&check).Error
	return &check, err
}

func FinishMembershipPaymentCheck(ctx context.Context, orderID int64, token string, result MembershipPaymentCheckResult) error {
	if !MembershipPaymentSchemaAvailable() {
		return ErrMembershipPaymentSchema
	}
	if ctx == nil || orderID <= 0 || token == "" || len(result.Reason) > 512 || (result.Status != MembershipPaymentPending && result.Status != MembershipPaymentReview && result.Status != MembershipPaymentComplete) {
		return ErrMembershipInvalidInput
	}
	now := common.GetTimestamp()
	if result.Status == MembershipPaymentPending && result.NextCheckAt <= now {
		return ErrMembershipInvalidInput
	}
	if result.Status != MembershipPaymentPending {
		result.NextCheckAt = 0
	}
	updates := map[string]any{"status": result.Status, "next_check_at": result.NextCheckAt, "claim_token": "", "claim_until": 0, "last_checked_at": now, "last_error": result.Reason, "updated_at": now}
	if result.Status == MembershipPaymentPending {
		result.NextCheckAt = max(result.NextCheckAt, now+30)
		updates["next_check_at"] = gorm.Expr("CASE WHEN next_check_at < ? THEN ? WHEN next_check_at < ? THEN next_check_at ELSE ? END", now+30, now+30, result.NextCheckAt, result.NextCheckAt)
	}
	updated := DB.WithContext(ctx).Model(&MembershipPaymentCheck{}).Where("order_id = ? AND status = ? AND claim_token = ? AND claim_until > ?", orderID, MembershipPaymentPending, token, now).Updates(updates)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrMembershipPaymentClaim
	}
	return nil
}

func ListDueMembershipPaymentEvidence(ctx context.Context, now int64, limit int) ([]MembershipPaymentEvidence, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || now <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var evidence []MembershipPaymentEvidence
	err := DB.WithContext(ctx).Where("status = ? AND retry_at <= ?", MembershipPaymentPending, now).Order("retry_at, id").Limit(limit).Find(&evidence).Error
	return evidence, err
}

// PersistVerifiedMembershipPayment accepts server-side merchant-query facts,
// not webhook parameters. Identity must match the frozen order merchant even
// when amount or business eligibility requires review after durable storage.
func PersistVerifiedMembershipPayment(ctx context.Context, input MembershipVerifiedPaymentInput) (*MembershipPaymentEvidence, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || input.OrderID <= 0 || input.ClaimToken == "" || input.MerchantID == "" || len(input.MerchantID) > 128 || input.ReceiptID == "" || len(input.ReceiptID) > 128 || input.AmountMinor < 0 || len(input.Currency) > 8 || len(input.ValidationError) > 512 {
		return nil, ErrMembershipInvalidInput
	}
	var order MembershipOrder
	if err := DB.WithContext(ctx).First(&order, input.OrderID).Error; err != nil {
		return nil, err
	}
	var result *MembershipPaymentEvidence
	var conflict error
	err := MembershipTransaction(ctx, func(tx *gorm.DB) error {
		result, conflict = nil, nil
		if _, _, err := lockMembershipUserTx(tx, order.UserID); err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&order, input.OrderID).Error; err != nil {
			return err
		}
		var check MembershipPaymentCheck
		if err := lockForUpdate(tx).First(&check, "order_id = ?", order.ID).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		if check.Status != MembershipPaymentPending || check.ClaimToken != input.ClaimToken || check.ClaimUntil <= now {
			return ErrMembershipPaymentClaim
		}
		if order.Provider != PaymentProviderEpay || check.MerchantID != input.MerchantID || check.TradeNo != order.TradeNo || check.UserID != order.UserID {
			return ErrMembershipPaymentEvidence
		}
		validation := strings.TrimSpace(input.ValidationError)
		if validation == "" && (input.ProviderPaidAt <= 0 || input.ProviderPaidAt < order.CreatedAt || input.ProviderPaidAt > now) {
			validation = "provider payment time is missing or invalid"
		}
		evidence := MembershipPaymentEvidence{OrderID: order.ID, ReceiptKey: PaymentProviderEpay + ":" + input.ReceiptID, ReceiptID: input.ReceiptID, MerchantID: input.MerchantID, AmountMinor: input.AmountMinor, Currency: strings.ToUpper(input.Currency), ProviderPaidAt: input.ProviderPaidAt, VerifiedAt: now, Source: MembershipPaymentMerchantQuery, Status: MembershipPaymentPending, RetryAt: now, ValidationError: validation, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&evidence).Error; err != nil {
			return err
		}
		var saved MembershipPaymentEvidence
		if err := lockForUpdate(tx).Where("receipt_key = ?", evidence.ReceiptKey).First(&saved).Error; err != nil {
			return err
		}
		check.Status, check.NextCheckAt, check.ClaimToken, check.ClaimUntil, check.LastCheckedAt, check.UpdatedAt = MembershipPaymentVerified, 0, "", 0, now, now
		if saved.OrderID != order.ID {
			check.Status, check.LastError, conflict = MembershipPaymentReview, "provider transaction already belongs to another order", ErrMembershipPaymentEvidence
		} else if saved.ReceiptID != input.ReceiptID || saved.MerchantID != input.MerchantID || saved.AmountMinor != input.AmountMinor || saved.Currency != evidence.Currency || saved.ProviderPaidAt != input.ProviderPaidAt || saved.Source != MembershipPaymentMerchantQuery || saved.ValidationError != validation {
			check.Status, check.LastError, conflict = MembershipPaymentReview, "verified payment evidence changed", ErrMembershipPaymentEvidence
			if err := tx.Model(&saved).Updates(map[string]any{"status": MembershipPaymentReview, "last_error": check.LastError, "updated_at": now}).Error; err != nil {
				return err
			}
		} else {
			result = &saved
			if saved.Status == MembershipPaymentComplete || saved.Status == MembershipPaymentReview {
				check.Status = saved.Status
			}
		}
		return tx.Save(&check).Error
	})
	if err != nil {
		return nil, err
	}
	return result, conflict
}

// A review is a business terminal state. Only pending verified evidence may
// retry, always carrying its original validation failures into the finalizer.
func RetryVerifiedMembershipPayment(ctx context.Context, evidenceID int64) (*MembershipOrder, error) {
	if !MembershipPaymentSchemaAvailable() {
		return nil, ErrMembershipPaymentSchema
	}
	if ctx == nil || evidenceID <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	var evidence MembershipPaymentEvidence
	if err := DB.WithContext(ctx).First(&evidence, evidenceID).Error; err != nil {
		return nil, err
	}
	var order MembershipOrder
	if err := DB.WithContext(ctx).First(&order, evidence.OrderID).Error; err != nil {
		return nil, err
	}
	if evidence.Status == MembershipPaymentComplete || evidence.Status == MembershipPaymentReview {
		return &order, nil
	}
	if evidence.Status != MembershipPaymentPending || evidence.Source != MembershipPaymentMerchantQuery {
		return nil, ErrMembershipPaymentEvidence
	}
	input := MembershipPaymentInput{TradeNo: order.TradeNo, Provider: PaymentProviderEpay, ReceiptID: evidence.ReceiptID, AmountMinor: evidence.AmountMinor, Currency: evidence.Currency, OneTime: true, Paid: true, ValidationError: evidence.ValidationError}
	result, err := completeMembershipPayment(ctx, input, evidence.ID)
	if err != nil {
		// A cancelled request leaves the already durable pending evidence due;
		// the next writer task resumes it without needing the webhook again.
		now := common.GetTimestamp()
		saved := DB.WithContext(ctx).Model(&MembershipPaymentEvidence{}).Where("id = ? AND status = ?", evidence.ID, MembershipPaymentPending).Updates(map[string]any{"retry_at": now + 30, "attempts": gorm.Expr("attempts + 1"), "last_error": "membership fulfillment failed", "updated_at": now})
		if saved.Error != nil {
			return nil, errors.Join(err, saved.Error)
		}
	}
	return result, err
}
