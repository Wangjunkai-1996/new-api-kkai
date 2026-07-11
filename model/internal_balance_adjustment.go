package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	InternalBalanceAdjustmentReasonCredit   = "invitation_reward"
	InternalBalanceAdjustmentReasonReversal = "invitation_reward_reversal"
)

var (
	ErrBalanceAdjustmentInvalidInput        = errors.New("invalid balance adjustment input")
	ErrBalanceAdjustmentIdempotencyConflict = errors.New("balance adjustment idempotency conflict")
	ErrBalanceAdjustmentReversalConflict    = errors.New("balance adjustment reversal conflict")
	ErrBalanceAdjustmentUserNotFound        = errors.New("balance adjustment user not found")
	ErrBalanceAdjustmentInsufficientBalance = errors.New("balance adjustment insufficient balance")
	ErrBalanceAdjustmentOverflow            = errors.New("balance adjustment overflow")

	syncInternalBalanceAdjustmentUserCache = syncInternalBalanceAdjustmentCache
)

type InternalBalanceAdjustment struct {
	ID                  int64   `json:"id" gorm:"primaryKey"`
	OperationID         string  `json:"operation_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	UserID              int     `json:"user_id" gorm:"not null;index"`
	Delta               int64   `json:"delta" gorm:"type:bigint;not null"`
	Reason              string  `json:"reason" gorm:"type:varchar(64);not null"`
	Metadata            string  `json:"metadata" gorm:"type:text;not null"`
	PayloadSHA256       string  `json:"-" gorm:"type:char(64);not null"`
	OriginalOperationID *string `json:"original_operation_id,omitempty" gorm:"type:varchar(128);uniqueIndex"`
	BalanceBefore       int64   `json:"balance_before" gorm:"type:bigint;not null"`
	BalanceAfter        int64   `json:"balance_after" gorm:"type:bigint;not null"`
	ClaimToken          string  `json:"-" gorm:"type:char(36);not null"`
	CreatedAt           int64   `json:"created_at" gorm:"type:bigint;not null"`
}

type InternalBalanceAdjustmentInput struct {
	OperationID         string
	UserID              int
	Delta               int64
	Reason              string
	Metadata            string
	PayloadSHA256       string
	OriginalOperationID *string
	CreatedAt           int64
}

type InternalBalanceAdjustmentResult struct {
	Adjustment *InternalBalanceAdjustment
	Replayed   bool
}

func ApplyInternalBalanceAdjustment(
	input InternalBalanceAdjustmentInput,
) (*InternalBalanceAdjustmentResult, error) {
	if !validInternalBalanceAdjustmentInput(input) {
		return nil, ErrBalanceAdjustmentInvalidInput
	}
	claimToken := uuid.NewString()
	result := &InternalBalanceAdjustmentResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		candidate := &InternalBalanceAdjustment{
			OperationID:         input.OperationID,
			UserID:              input.UserID,
			Delta:               input.Delta,
			Reason:              input.Reason,
			Metadata:            input.Metadata,
			PayloadSHA256:       input.PayloadSHA256,
			OriginalOperationID: input.OriginalOperationID,
			ClaimToken:          claimToken,
			CreatedAt:           input.CreatedAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(candidate).Error; err != nil {
			return err
		}

		var stored InternalBalanceAdjustment
		err := tx.Where("operation_id = ?", input.OperationID).First(&stored).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && input.OriginalOperationID != nil {
			return ErrBalanceAdjustmentReversalConflict
		}
		if err != nil {
			return err
		}
		if stored.ClaimToken != claimToken {
			if stored.PayloadSHA256 != input.PayloadSHA256 {
				return ErrBalanceAdjustmentIdempotencyConflict
			}
			result.Adjustment = &stored
			result.Replayed = true
			return nil
		}

		if err := validateInternalBalanceReversal(tx, input); err != nil {
			return err
		}
		balanceAfter, err := updateInternalBalanceUserQuota(tx, input.UserID, input.Delta)
		if err != nil {
			return err
		}
		stored.BalanceAfter = balanceAfter
		stored.BalanceBefore = balanceAfter - input.Delta
		stored.ClaimToken = ""
		if err := tx.Model(&InternalBalanceAdjustment{}).
			Where("id = ?", stored.ID).
			Updates(map[string]any{
				"balance_before": stored.BalanceBefore,
				"balance_after":  stored.BalanceAfter,
				"claim_token":    stored.ClaimToken,
			}).Error; err != nil {
			return err
		}
		result.Adjustment = &stored
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !result.Replayed {
		if err := syncInternalBalanceAdjustmentUserCache(input.UserID, input.Delta); err != nil {
			common.SysLog("failed to synchronize user cache after internal balance adjustment")
		}
	}
	return result, nil
}
