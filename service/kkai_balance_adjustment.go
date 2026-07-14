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

var (
	ErrKKAIBalanceAdjustmentInvalidInput        = model.ErrKKAIBalanceAdjustmentInvalidInput
	ErrKKAIBalanceAdjustmentIdempotencyConflict = model.ErrKKAIBalanceAdjustmentIdempotencyConflict
	ErrKKAIBalanceAdjustmentReversalConflict    = model.ErrKKAIBalanceAdjustmentReversalConflict
	ErrKKAIBalanceAdjustmentUserNotFound        = model.ErrKKAIBalanceAdjustmentUserNotFound
	ErrKKAIBalanceAdjustmentInsufficientBalance = model.ErrKKAIBalanceAdjustmentInsufficientBalance
	ErrKKAIBalanceAdjustmentOverflow            = model.ErrKKAIBalanceAdjustmentOverflow

	kkaiBalanceOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type KKAIBalanceAdjustmentResponse struct {
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

func ApplyKKAIBalanceAdjustment(request dto.KKAIBalanceAdjustmentRequest) (*KKAIBalanceAdjustmentResponse, error) {
	metadata := request.Metadata
	if metadata == nil {
		metadata = &dto.KKAIBalanceAdjustmentMetadata{}
	}
	if err := validateKKAIBalanceAdjustment(request, metadata); err != nil {
		return nil, err
	}

	metadataJSON, err := common.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	canonicalPayload, err := common.Marshal(struct {
		OperationID string                            `json:"operation_id"`
		UserID      int                               `json:"user_id"`
		Delta       int64                             `json:"delta"`
		Reason      string                            `json:"reason"`
		Metadata    dto.KKAIBalanceAdjustmentMetadata `json:"metadata"`
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
	result, err := model.ApplyKKAIBalanceAdjustment(model.KKAIBalanceAdjustmentInput{
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
	return &KKAIBalanceAdjustmentResponse{
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

func validateKKAIBalanceAdjustment(
	request dto.KKAIBalanceAdjustmentRequest,
	metadata *dto.KKAIBalanceAdjustmentMetadata,
) error {
	if !kkaiBalanceOperationIDPattern.MatchString(request.OperationID) || request.UserID <= 0 {
		return ErrKKAIBalanceAdjustmentInvalidInput
	}
	if request.Delta == 0 || request.Delta > math.MaxInt32 || request.Delta < -math.MaxInt32 {
		return ErrKKAIBalanceAdjustmentInvalidInput
	}
	if metadata.RebateRecordID != nil && *metadata.RebateRecordID <= 0 {
		return ErrKKAIBalanceAdjustmentInvalidInput
	}
	if metadata.PayoutID != nil && *metadata.PayoutID <= 0 {
		return ErrKKAIBalanceAdjustmentInvalidInput
	}
	if metadata.OriginalOperationID != "" && !kkaiBalanceOperationIDPattern.MatchString(metadata.OriginalOperationID) {
		return ErrKKAIBalanceAdjustmentInvalidInput
	}

	switch strings.TrimSpace(request.Reason) {
	case model.KKAIBalanceAdjustmentReasonCredit:
		if request.Reason != model.KKAIBalanceAdjustmentReasonCredit || request.Delta < 0 ||
			metadata.OriginalOperationID != "" {
			return ErrKKAIBalanceAdjustmentInvalidInput
		}
	case model.KKAIBalanceAdjustmentReasonReversal:
		if request.Reason != model.KKAIBalanceAdjustmentReasonReversal || request.Delta > 0 ||
			metadata.OriginalOperationID == "" || metadata.OriginalOperationID == request.OperationID {
			return ErrKKAIBalanceAdjustmentInvalidInput
		}
	default:
		return ErrKKAIBalanceAdjustmentInvalidInput
	}
	return nil
}
