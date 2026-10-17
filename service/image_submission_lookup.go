package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type ImageSubmissionLookup struct {
	State      string               `json:"state"`
	Generation *ImageGenerationView `json:"generation,omitempty"`
}

// LookupImageSubmission never reserves, expires, or retries an operation. An
// absent binding is inconclusive: the original request may still be uploading.
func LookupImageSubmission(ctx context.Context, db *gorm.DB, userID int, key string) (ImageSubmissionLookup, error) {
	result := ImageSubmissionLookup{State: "not_found"}
	key = strings.TrimSpace(key)
	if db == nil || userID <= 0 || key == "" || len(key) > 128 {
		return result, ErrInvalidIdempotencyRequest
	}
	var binding model.KKAIIdempotencyKey
	err := db.WithContext(ctx).Where(map[string]any{
		"user_id": userID, "operation": model.ImageIdempotencyOperationSubmit, "key": key,
	}).Take(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if binding.ResourceID == "" {
		result.State = "pending"
		return result, nil
	}
	if binding.ResourceType != model.ImageIdempotencyResourceGeneration {
		return result, ErrIdempotencyBindingConflict
	}
	generationID, err := strconv.ParseInt(binding.ResourceID, 10, 64)
	if err != nil || generationID <= 0 {
		return result, ErrIdempotencyBindingConflict
	}
	view, err := GetImageGenerationView(ctx, db, userID, generationID)
	if errors.Is(err, ErrImageGenerationNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.State, result.Generation = "found", view
	return result, nil
}
