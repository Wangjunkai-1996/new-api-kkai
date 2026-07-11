package model

import (
	"encoding/hex"
	"errors"
	"math"

	"gorm.io/gorm"
)

func validInternalBalanceAdjustmentInput(input InternalBalanceAdjustmentInput) bool {
	if len(input.OperationID) == 0 || len(input.OperationID) > 128 || input.UserID <= 0 {
		return false
	}
	if input.Delta == 0 || input.Delta > math.MaxInt32 || input.Delta < -math.MaxInt32 {
		return false
	}
	if len(input.Metadata) > 2048 || len(input.PayloadSHA256) != 64 || input.CreatedAt <= 0 {
		return false
	}
	if _, err := hex.DecodeString(input.PayloadSHA256); err != nil {
		return false
	}
	switch input.Reason {
	case InternalBalanceAdjustmentReasonCredit:
		return input.Delta > 0 && input.OriginalOperationID == nil
	case InternalBalanceAdjustmentReasonReversal:
		return input.Delta < 0 &&
			input.OriginalOperationID != nil &&
			*input.OriginalOperationID != "" &&
			*input.OriginalOperationID != input.OperationID &&
			len(*input.OriginalOperationID) <= 128
	default:
		return false
	}
}

func syncInternalBalanceAdjustmentCache(userID int, delta int64) error {
	if err := cacheIncrUserQuota(userID, delta); err == nil {
		return nil
	}
	return invalidateUserCache(userID)
}

func validateInternalBalanceReversal(tx *gorm.DB, input InternalBalanceAdjustmentInput) error {
	if input.OriginalOperationID == nil {
		return nil
	}
	var original InternalBalanceAdjustment
	if err := tx.Where("operation_id = ?", *input.OriginalOperationID).First(&original).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBalanceAdjustmentReversalConflict
		}
		return err
	}
	if original.UserID != input.UserID ||
		original.Reason != InternalBalanceAdjustmentReasonCredit ||
		original.Delta <= 0 ||
		original.Delta != -input.Delta {
		return ErrBalanceAdjustmentReversalConflict
	}
	return nil
}

func updateInternalBalanceUserQuota(tx *gorm.DB, userID int, delta int64) (int64, error) {
	query := tx.Model(&User{}).Where("id = ?", userID)
	if delta < 0 {
		query = query.Where("COALESCE(quota, 0) >= ?", -delta)
	} else {
		query = query.Where("COALESCE(quota, 0) <= ?", int64(math.MaxInt32)-delta)
	}
	update := query.UpdateColumn("quota", gorm.Expr("COALESCE(quota, 0) + ?", delta))
	if update.Error != nil {
		return 0, update.Error
	}
	if update.RowsAffected == 0 {
		var user User
		err := tx.Select("id", "quota").Where("id = ?", userID).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrBalanceAdjustmentUserNotFound
		}
		if err != nil {
			return 0, err
		}
		if delta < 0 {
			return 0, ErrBalanceAdjustmentInsufficientBalance
		}
		return 0, ErrBalanceAdjustmentOverflow
	}

	var balanceAfter int64
	if err := tx.Model(&User{}).
		Where("id = ?", userID).
		Select("quota").
		Scan(&balanceAfter).Error; err != nil {
		return 0, err
	}
	return balanceAfter, nil
}
