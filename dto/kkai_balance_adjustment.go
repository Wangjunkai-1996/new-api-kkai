package dto

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

type KKAIBalanceAdjustmentMetadata struct {
	RebateRecordID      *int64 `json:"rebate_record_id,omitempty"`
	PayoutID            *int64 `json:"payout_id,omitempty"`
	OriginalOperationID string `json:"original_operation_id,omitempty"`
}

func (metadata *KKAIBalanceAdjustmentMetadata) UnmarshalJSON(data []byte) error {
	if err := rejectUnknownKKAIBalanceAdjustmentFields(data, "metadata", map[string]struct{}{
		"rebate_record_id":      {},
		"payout_id":             {},
		"original_operation_id": {},
	}); err != nil {
		return err
	}
	type metadataAlias KKAIBalanceAdjustmentMetadata
	var decoded metadataAlias
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*metadata = KKAIBalanceAdjustmentMetadata(decoded)
	return nil
}

type KKAIBalanceAdjustmentRequest struct {
	OperationID string                         `json:"operation_id"`
	UserID      int                            `json:"user_id"`
	Delta       int64                          `json:"delta"`
	Reason      string                         `json:"reason"`
	Metadata    *KKAIBalanceAdjustmentMetadata `json:"metadata,omitempty"`
}

func (request *KKAIBalanceAdjustmentRequest) UnmarshalJSON(data []byte) error {
	if err := rejectUnknownKKAIBalanceAdjustmentFields(data, "request", map[string]struct{}{
		"operation_id": {},
		"user_id":      {},
		"delta":        {},
		"reason":       {},
		"metadata":     {},
	}); err != nil {
		return err
	}
	type requestAlias KKAIBalanceAdjustmentRequest
	var decoded requestAlias
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*request = KKAIBalanceAdjustmentRequest(decoded)
	return nil
}

func rejectUnknownKKAIBalanceAdjustmentFields(data []byte, scope string, allowed map[string]struct{}) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("unknown %s field %q", scope, field)
		}
	}
	return nil
}
