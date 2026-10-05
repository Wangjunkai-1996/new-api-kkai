package model

import (
	"encoding/hex"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KKAIBalanceAdjustmentReasonCredit   = "invitation_reward"
	KKAIBalanceAdjustmentReasonReversal = "invitation_reward_reversal"
)

var (
	ErrKKAIBalanceAdjustmentInvalidInput        = errors.New("invalid KKAI balance adjustment input")
	ErrKKAIBalanceAdjustmentIdempotencyConflict = errors.New("KKAI balance adjustment idempotency conflict")
	ErrKKAIBalanceAdjustmentReversalConflict    = errors.New("KKAI balance adjustment reversal conflict")
	ErrKKAIBalanceAdjustmentUserNotFound        = errors.New("KKAI balance adjustment user not found")
	ErrKKAIBalanceAdjustmentInsufficientBalance = errors.New("KKAI balance adjustment insufficient balance")
	ErrKKAIBalanceAdjustmentOverflow            = errors.New("KKAI balance adjustment overflow")

	syncKKAIBalanceAdjustmentUserCache = invalidateUserCache
)

type KKAIBalanceAdjustmentInput struct {
	OperationID         string
	UserID              int
	Delta               int64
	Reason              string
	Metadata            string
	PayloadSHA256       string
	OriginalOperationID *string
	CreatedAt           int64
	QuotaEpoch          string
}

type KKAIBalanceAdjustmentResult struct {
	Adjustment *KKAIInternalBalanceAdjustment
	Replayed   bool
}

func ApplyKKAIBalanceAdjustment(input KKAIBalanceAdjustmentInput) (*KKAIBalanceAdjustmentResult, error) {
	if !validKKAIBalanceAdjustmentInput(input) {
		return nil, ErrKKAIBalanceAdjustmentInvalidInput
	}

	result := &KKAIBalanceAdjustmentResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		candidate := &KKAIInternalBalanceAdjustment{
			OperationID:         input.OperationID,
			UserID:              input.UserID,
			Delta:               input.Delta,
			Reason:              input.Reason,
			Metadata:            input.Metadata,
			PayloadSHA256:       input.PayloadSHA256,
			OriginalOperationID: input.OriginalOperationID,
			CreatedAt:           input.CreatedAt,
		}
		create := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(candidate)
		if create.Error != nil {
			return create.Error
		}
		if create.RowsAffected == 0 {
			var stored KKAIInternalBalanceAdjustment
			err := tx.Where("operation_id = ?", input.OperationID).First(&stored).Error
			if errors.Is(err, gorm.ErrRecordNotFound) && input.OriginalOperationID != nil {
				return ErrKKAIBalanceAdjustmentReversalConflict
			}
			if err != nil {
				return err
			}
			if stored.PayloadSHA256 != input.PayloadSHA256 {
				return ErrKKAIBalanceAdjustmentIdempotencyConflict
			}
			storedEpoch := stored.SourceEpoch
			if storedEpoch == "" {
				storedEpoch = common.CreditEpochLegacy
			}
			if input.QuotaEpoch != "" && input.QuotaEpoch != storedEpoch {
				return ErrKKAIBalanceAdjustmentIdempotencyConflict
			}
			result.Adjustment = &stored
			result.Replayed = true
			return nil
		}

		walletDelta, sourceEpoch, err := resolveKKAIBalanceWalletDelta(tx, input)
		if err != nil {
			return err
		}
		balanceAfter, err := updateKKAIBalanceUserQuota(tx, input.UserID, walletDelta)
		if err != nil {
			return err
		}
		candidate.BalanceAfter = balanceAfter
		candidate.BalanceBefore = balanceAfter - walletDelta
		candidate.WalletDelta = &walletDelta
		candidate.SourceEpoch = sourceEpoch
		if err := tx.Model(&KKAIInternalBalanceAdjustment{}).
			Where("id = ?", candidate.ID).
			Updates(map[string]any{
				"balance_before": candidate.BalanceBefore,
				"balance_after":  candidate.BalanceAfter,
				"wallet_delta":   walletDelta,
				"source_epoch":   sourceEpoch,
			}).Error; err != nil {
			return err
		}
		result.Adjustment = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !result.Replayed {
		if err := syncKKAIBalanceAdjustmentUserCache(input.UserID); err != nil {
			common.SysLog("failed to invalidate user cache after KKAI balance adjustment")
		}
	}
	return result, nil
}

func validKKAIBalanceAdjustmentInput(input KKAIBalanceAdjustmentInput) bool {
	if input.OperationID == "" || len(input.OperationID) > 128 || input.UserID <= 0 {
		return false
	}
	if input.Delta == 0 || input.Delta == math.MinInt64 {
		return false
	}
	if input.QuotaEpoch != "" && input.QuotaEpoch != common.CreditEpochLegacy && input.QuotaEpoch != common.CreditEpochUSD {
		return false
	}
	if len(input.Metadata) > 2048 || len(input.PayloadSHA256) != 64 || input.CreatedAt <= 0 {
		return false
	}
	if _, err := hex.DecodeString(input.PayloadSHA256); err != nil {
		return false
	}
	switch input.Reason {
	case KKAIBalanceAdjustmentReasonCredit:
		return input.Delta > 0 && input.OriginalOperationID == nil
	case KKAIBalanceAdjustmentReasonReversal:
		return input.Delta < 0 && input.OriginalOperationID != nil &&
			*input.OriginalOperationID != "" && *input.OriginalOperationID != input.OperationID &&
			len(*input.OriginalOperationID) <= 128
	default:
		return false
	}
}

func resolveKKAIBalanceWalletDelta(tx *gorm.DB, input KKAIBalanceAdjustmentInput) (int64, string, error) {
	epoch, err := common.CurrentCreditEpoch()
	if err != nil {
		return 0, "", err
	}
	if epoch != nil {
		if _, ok := epoch.LegacyMaxIDs["kkai_internal_balance_adjustments"]; !ok {
			return 0, "", common.ErrCreditEpochInvalid
		}
	}
	if input.OriginalOperationID == nil {
		source := input.QuotaEpoch
		if epoch == nil {
			if source == common.CreditEpochUSD {
				return 0, "", common.ErrCreditEpochInvalid
			}
			return input.Delta, common.CreditEpochLegacy, nil
		}
		// A delayed payout cannot be classified from its arrival timestamp.
		// Existing idempotent operations were handled above; new operations must
		// declare the unit agreed with the external invitation ledger.
		switch source {
		case common.CreditEpochLegacy:
			return common.LegacyCreditToUSD(input.Delta), source, nil
		case common.CreditEpochUSD:
			return input.Delta, source, nil
		default:
			return 0, "", common.ErrCreditEpochInvalid
		}
	}
	var original KKAIInternalBalanceAdjustment
	if err := lockForUpdate(tx).Where("operation_id = ?", *input.OriginalOperationID).First(&original).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, "", ErrKKAIBalanceAdjustmentReversalConflict
		}
		return 0, "", err
	}
	if original.UserID != input.UserID || original.Reason != KKAIBalanceAdjustmentReasonCredit ||
		original.Delta <= 0 || original.Delta != -input.Delta {
		return 0, "", ErrKKAIBalanceAdjustmentReversalConflict
	}
	source := original.SourceEpoch
	if source == "" {
		source = common.CreditEpochLegacy
	}
	if input.QuotaEpoch != "" && input.QuotaEpoch != source {
		return 0, "", ErrKKAIBalanceAdjustmentReversalConflict
	}
	if epoch == nil {
		return input.Delta, source, nil
	}
	if epoch.IsLegacy("kkai_internal_balance_adjustments", original.ID) {
		return -common.LegacyCreditToUSD(original.Delta), common.CreditEpochLegacy, nil
	}
	if original.WalletDelta == nil || *original.WalletDelta < 0 {
		return 0, "", common.ErrCreditEpochInvalid
	}
	return -*original.WalletDelta, source, nil
}

func updateKKAIBalanceUserQuota(tx *gorm.DB, userID int, delta int64) (int64, error) {
	if delta == 0 {
		var user User
		if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", userID).First(&user).Error; err != nil {
			return 0, err
		}
		return user.Quota, nil
	}
	query := tx.Model(&User{}).Where("id = ?", userID)
	if delta < 0 {
		query = query.Where("COALESCE(quota, 0) >= ?", -delta)
	} else {
		query = query.Where("COALESCE(quota, 0) <= ?", int64(math.MaxInt64)-delta)
	}
	update := query.UpdateColumn("quota", gorm.Expr("COALESCE(quota, 0) + ?", delta))
	if update.Error != nil {
		return 0, update.Error
	}
	if update.RowsAffected == 0 {
		var user User
		err := tx.Select("id", "quota").Where("id = ?", userID).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrKKAIBalanceAdjustmentUserNotFound
		}
		if err != nil {
			return 0, err
		}
		if delta < 0 {
			return 0, ErrKKAIBalanceAdjustmentInsufficientBalance
		}
		return 0, ErrKKAIBalanceAdjustmentOverflow
	}

	var balanceAfter int64
	if err := tx.Model(&User{}).Where("id = ?", userID).Select("quota").Scan(&balanceAfter).Error; err != nil {
		return 0, err
	}
	return balanceAfter, nil
}
