package service

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

const (
	InternalBalanceReasonInvitationReward         = model.InternalBalanceAdjustmentReasonCredit
	InternalBalanceReasonInvitationRewardReversal = model.InternalBalanceAdjustmentReasonReversal
)

var (
	ErrInvalidInternalBalanceAdjustment   = model.ErrBalanceAdjustmentInvalidInput
	ErrInternalBalanceIdempotencyConflict = model.ErrBalanceAdjustmentIdempotencyConflict
	ErrInternalBalanceReversalConflict    = model.ErrBalanceAdjustmentReversalConflict
	ErrInternalBalanceUserNotFound        = model.ErrBalanceAdjustmentUserNotFound
	ErrInternalBalanceInsufficientBalance = model.ErrBalanceAdjustmentInsufficientBalance
	ErrInternalBalanceOverflow            = model.ErrBalanceAdjustmentOverflow

	internalBalanceOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type InternalBalanceAdjustmentResponse struct {
	AdjustmentID  int64  `json:"adjustment_id"`
	OperationID   string `json:"operation_id"`
	UserID        int    `json:"user_id"`
	Delta         int64  `json:"delta"`
	Reason        string `json:"reason"`
	BalanceBefore int64  `json:"balance_before"`
	BalanceAfter  int64  `json:"balance_after"`
	CreatedAt     int64  `json:"created_at"`
	Replayed      bool   `json:"replayed"`
}

func CreateInternalBalanceAdjustment(
	request dto.InternalBalanceAdjustmentRequest,
) (*InternalBalanceAdjustmentResponse, error) {
	metadata := request.Metadata
	if metadata == nil {
		metadata = &dto.InternalBalanceAdjustmentMetadata{}
	}
	if err := validateInternalBalanceAdjustment(request, metadata); err != nil {
		return nil, err
	}

	metadataJSON, err := common.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	canonicalPayload, err := common.Marshal(struct {
		OperationID string                                `json:"operation_id"`
		UserID      int                                   `json:"user_id"`
		Delta       int64                                 `json:"delta"`
		Reason      string                                `json:"reason"`
		Metadata    dto.InternalBalanceAdjustmentMetadata `json:"metadata"`
	}{
		OperationID: request.OperationID,
		UserID:      request.UserID,
		Delta:       request.Delta,
		Reason:      request.Reason,
		Metadata:    *metadata,
	})
	if err != nil {
		return nil, err
	}
	payloadHash := sha256.Sum256(canonicalPayload)

	var originalOperationID *string
	if metadata.OriginalOperationID != "" {
		originalOperationID = &metadata.OriginalOperationID
	}
	result, err := model.ApplyInternalBalanceAdjustment(model.InternalBalanceAdjustmentInput{
		OperationID:         request.OperationID,
		UserID:              request.UserID,
		Delta:               request.Delta,
		Reason:              request.Reason,
		Metadata:            string(metadataJSON),
		PayloadSHA256:       hex.EncodeToString(payloadHash[:]),
		OriginalOperationID: originalOperationID,
		CreatedAt:           common.GetTimestamp(),
	})
	if err != nil {
		return nil, err
	}
	adjustment := result.Adjustment
	return &InternalBalanceAdjustmentResponse{
		AdjustmentID:  adjustment.ID,
		OperationID:   adjustment.OperationID,
		UserID:        adjustment.UserID,
		Delta:         adjustment.Delta,
		Reason:        adjustment.Reason,
		BalanceBefore: adjustment.BalanceBefore,
		BalanceAfter:  adjustment.BalanceAfter,
		CreatedAt:     adjustment.CreatedAt,
		Replayed:      result.Replayed,
	}, nil
}

func validateInternalBalanceAdjustment(
	request dto.InternalBalanceAdjustmentRequest,
	metadata *dto.InternalBalanceAdjustmentMetadata,
) error {
	if !internalBalanceOperationIDPattern.MatchString(request.OperationID) || request.UserID <= 0 {
		return ErrInvalidInternalBalanceAdjustment
	}
	if request.Delta == 0 || request.Delta > math.MaxInt32 || request.Delta < -math.MaxInt32 {
		return ErrInvalidInternalBalanceAdjustment
	}
	if metadata.RebateRecordID != nil && *metadata.RebateRecordID <= 0 {
		return ErrInvalidInternalBalanceAdjustment
	}
	if metadata.PayoutID != nil && *metadata.PayoutID <= 0 {
		return ErrInvalidInternalBalanceAdjustment
	}
	if metadata.OriginalOperationID != "" &&
		!internalBalanceOperationIDPattern.MatchString(metadata.OriginalOperationID) {
		return ErrInvalidInternalBalanceAdjustment
	}

	switch strings.TrimSpace(request.Reason) {
	case InternalBalanceReasonInvitationReward:
		if request.Reason != InternalBalanceReasonInvitationReward ||
			request.Delta < 0 || metadata.OriginalOperationID != "" {
			return ErrInvalidInternalBalanceAdjustment
		}
	case InternalBalanceReasonInvitationRewardReversal:
		if request.Reason != InternalBalanceReasonInvitationRewardReversal ||
			request.Delta > 0 || metadata.OriginalOperationID == "" ||
			metadata.OriginalOperationID == request.OperationID {
			return ErrInvalidInternalBalanceAdjustment
		}
	default:
		return ErrInvalidInternalBalanceAdjustment
	}
	return nil
}
