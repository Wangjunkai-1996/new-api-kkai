package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func RequestResellerWithdrawalTx(tx *gorm.DB, input ResellerWithdrawalInput) (*ResellerWithdrawal, error) {
	if tx == nil || input.TenantID <= 0 || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 || input.AmountMinor < ResellerMinWithdrawalMinor || input.FeeMinor < 0 || input.FeeMinor >= input.AmountMinor || strings.TrimSpace(input.AlipayAccount) == "" || len(input.AlipayAccount) > 254 || strings.TrimSpace(input.AlipayName) == "" || len(input.AlipayName) > 128 || len(input.FeePolicySnapshot) > 512 {
		return nil, ErrResellerFinanceInvalidInput
	}
	now := normalizeFinanceNow(input.Now)
	wallet, err := resellerWalletForUpdate(tx, input.TenantID, now)
	if err != nil {
		return nil, err
	}
	payload := input
	payload.Now = 0
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	var existing ResellerWithdrawal
	if err := lockForUpdate(tx).Where("tenant_id = ? AND idempotency_key = ?", input.TenantID, input.IdempotencyKey).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if wallet.DebtMinor > 0 {
		return nil, ErrResellerDebtOutstanding
	}
	carry, err := parseResellerDecimal(wallet.ExactProfitCarry)
	if err != nil {
		return nil, err
	}
	reserve := int64(0)
	if carry.IsNegative() {
		reserve, err = decimalCeilInt64(carry.Neg())
		if err != nil {
			return nil, err
		}
	}
	if reserve > wallet.AvailableMinor || wallet.AvailableMinor-reserve < input.AmountMinor {
		return nil, ErrResellerInsufficientFunds
	}
	timeline, err := common.Marshal([]ResellerWithdrawalEvent{{Status: ResellerWithdrawalRequested, At: now}})
	if err != nil {
		return nil, err
	}
	withdrawal := &ResellerWithdrawal{TenantID: input.TenantID, WalletID: wallet.ID, IdempotencyKey: input.IdempotencyKey, AmountMinor: input.AmountMinor, FeeMinor: input.FeeMinor, NetMinor: input.AmountMinor - input.FeeMinor, Status: ResellerWithdrawalRequested, AlipayAccount: input.AlipayAccount, AlipayName: input.AlipayName, FeePolicySnapshot: input.FeePolicySnapshot, Timeline: string(timeline), PayloadHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(withdrawal).Error; err != nil {
		return nil, err
	}
	if err := applyResellerWalletDelta(wallet, 0, -input.AmountMinor, input.AmountMinor, 0); err != nil {
		return nil, err
	}
	wallet.UpdatedAt = now
	if err := tx.Save(wallet).Error; err != nil {
		return nil, err
	}
	entry := &ResellerLedgerEntry{TenantID: input.TenantID, WalletID: wallet.ID, OperationID: fmt.Sprintf("withdrawal:%d:%s", input.TenantID, input.IdempotencyKey), Kind: ResellerLedgerKindWithdrawal, AmountMinor: -input.AmountMinor, AvailableDelta: -input.AmountMinor, FrozenDelta: input.AmountMinor, SourceReference: fmt.Sprintf("withdrawal:%d", withdrawal.ID), CreatedAt: now}
	// The immutable ledger deliberately contains no Alipay recipient identity.
	if _, err := createResellerLedgerEntry(tx, entry, struct{ WithdrawalID, Amount, Fee int64 }{withdrawal.ID, input.AmountMinor, input.FeeMinor}); err != nil {
		return nil, err
	}
	return withdrawal, nil
}

type ResellerWithdrawalEvent struct {
	Status     string `json:"status"`
	ActorID    int    `json:"actor_id"`
	PaymentRef string `json:"payment_ref,omitempty"`
	Note       string `json:"note,omitempty"`
	At         int64  `json:"at"`
}

type ResellerWithdrawalReviewInput struct {
	WithdrawalID int64
	ReviewerID   int
	Status       string
	PaymentRef   string
	Note         string
	Now          int64
}

// ReviewResellerWithdrawalTx records a human review or confirmed manual payout.
// An uncertain payment stays approved; only a confirmed failed/rejected outcome
// releases frozen funds. No function performs or retries an external payout.
func ReviewResellerWithdrawalTx(tx *gorm.DB, input ResellerWithdrawalReviewInput) (*ResellerWithdrawal, error) {
	if tx == nil || input.WithdrawalID <= 0 || input.ReviewerID <= 0 || len(input.PaymentRef) > 192 || len(input.Note) > 4096 {
		return nil, ErrResellerFinanceInvalidInput
	}
	switch input.Status {
	case ResellerWithdrawalApproved:
	case ResellerWithdrawalPaid:
		if strings.TrimSpace(input.PaymentRef) == "" {
			return nil, ErrResellerFinanceInvalidInput
		}
	case ResellerWithdrawalRejected, ResellerWithdrawalFailed:
		if strings.TrimSpace(input.Note) == "" {
			return nil, ErrResellerFinanceInvalidInput
		}
	default:
		return nil, ErrResellerFinanceInvalidInput
	}
	now := normalizeFinanceNow(input.Now)
	// Read only the tenant before acquiring the common wallet-first lock order.
	var identity ResellerWithdrawal
	if err := tx.Select("id", "tenant_id").First(&identity, input.WithdrawalID).Error; err != nil {
		return nil, err
	}
	wallet, err := resellerWalletForUpdate(tx, identity.TenantID, now)
	if err != nil {
		return nil, err
	}
	var withdrawal ResellerWithdrawal
	if err := lockForUpdate(tx).First(&withdrawal, input.WithdrawalID).Error; err != nil {
		return nil, err
	}
	if withdrawal.Status == input.Status {
		if withdrawal.ReviewedBy != input.ReviewerID || withdrawal.PaymentRef != input.PaymentRef || withdrawal.ReviewNote != input.Note {
			return nil, ErrResellerLedgerConflict
		}
		return &withdrawal, nil
	}
	if (input.Status == ResellerWithdrawalApproved && withdrawal.Status != ResellerWithdrawalRequested) || (input.Status == ResellerWithdrawalPaid && withdrawal.Status != ResellerWithdrawalApproved) || ((input.Status == ResellerWithdrawalRejected || input.Status == ResellerWithdrawalFailed) && withdrawal.Status != ResellerWithdrawalRequested && withdrawal.Status != ResellerWithdrawalApproved) {
		return nil, ErrResellerWithdrawalConflict
	}
	if (input.Status == ResellerWithdrawalApproved || input.Status == ResellerWithdrawalPaid) && wallet.DebtMinor > 0 {
		return nil, ErrResellerDebtOutstanding
	}
	if input.Status == ResellerWithdrawalPaid {
		carry, err := parseResellerDecimal(wallet.ExactProfitCarry)
		if err != nil {
			return nil, err
		}
		if carry.IsNegative() && decimal.NewFromInt(wallet.AvailableMinor).LessThan(carry.Neg().Ceil()) {
			return nil, ErrResellerInsufficientFunds
		}
	}
	availableDelta, frozenDelta := int64(0), int64(0)
	kind := ResellerLedgerKindWithdrawalPaid
	if input.Status == ResellerWithdrawalPaid {
		frozenDelta = -withdrawal.AmountMinor
	}
	if input.Status == ResellerWithdrawalRejected || input.Status == ResellerWithdrawalFailed {
		availableDelta = withdrawal.AmountMinor
		frozenDelta = -withdrawal.AmountMinor
		kind = ResellerLedgerKindWithdrawalReversal
	}
	if frozenDelta != 0 {
		if err := applyResellerWalletDelta(wallet, 0, availableDelta, frozenDelta, 0); err != nil {
			return nil, err
		}
		wallet.UpdatedAt = now
		if err := tx.Save(wallet).Error; err != nil {
			return nil, err
		}
		entry := &ResellerLedgerEntry{TenantID: withdrawal.TenantID, WalletID: wallet.ID, OperationID: fmt.Sprintf("withdrawal-%s:%d", input.Status, withdrawal.ID), Kind: kind, AmountMinor: availableDelta, AvailableDelta: availableDelta, FrozenDelta: frozenDelta, SourceReference: fmt.Sprintf("withdrawal:%d", withdrawal.ID), ReversalOf: fmt.Sprintf("withdrawal:%d:%s", withdrawal.TenantID, withdrawal.IdempotencyKey), CreatedAt: now}
		if _, err := createResellerLedgerEntry(tx, entry, input); err != nil {
			return nil, err
		}
	}
	var timeline []ResellerWithdrawalEvent
	if err := common.UnmarshalJsonStr(withdrawal.Timeline, &timeline); err != nil {
		return nil, err
	}
	timeline = append(timeline, ResellerWithdrawalEvent{Status: input.Status, ActorID: input.ReviewerID, PaymentRef: input.PaymentRef, Note: input.Note, At: now})
	encoded, err := common.Marshal(timeline)
	if err != nil {
		return nil, err
	}
	withdrawal.Status = input.Status
	withdrawal.PaymentRef = input.PaymentRef
	withdrawal.ReviewNote = input.Note
	withdrawal.ReviewedBy = input.ReviewerID
	withdrawal.ReviewedAt = now
	withdrawal.UpdatedAt = now
	withdrawal.Timeline = string(encoded)
	if input.Status == ResellerWithdrawalPaid {
		withdrawal.PaidAt = now
	}
	if err := tx.Save(&withdrawal).Error; err != nil {
		return nil, err
	}
	return &withdrawal, nil
}
