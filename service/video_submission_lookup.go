package service

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	VideoSubmissionStatusAccepted = "accepted"
	VideoSubmissionStatusPending  = "pending"
	VideoSubmissionStatusNotFound = "not_found"
)

// VideoSubmissionLookup is deliberately smaller than a generation view. It is
// a recovery receipt for an ambiguous POST and never starts or retries work.
type VideoSubmissionLookup struct {
	Status       string `json:"status"`
	TaskID       string `json:"task_id,omitempty"`
	GenerationID *int64 `json:"generation_id,omitempty"`
}

// LookupVideoSubmission only reads the caller's idempotency binding. A bound
// task is accepted even when it has already failed, because a retry would be a
// second billable submission. A missing bound task remains pending: the
// binding is evidence that the original request cannot safely be replayed.
func LookupVideoSubmission(ctx context.Context, db *gorm.DB, userID int, key string) (VideoSubmissionLookup, error) {
	result := VideoSubmissionLookup{Status: VideoSubmissionStatusNotFound}
	key = strings.TrimSpace(key)
	if db == nil || userID <= 0 || key == "" || len(key) > 128 {
		return result, ErrInvalidIdempotencyRequest
	}

	var binding model.KKAIIdempotencyKey
	err := db.WithContext(ctx).Where(map[string]any{
		"user_id": userID, "operation": model.VideoIdempotencyOperationSubmit, "key": key,
	}).Take(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if binding.ResourceID == "" {
		result.Status = VideoSubmissionStatusPending
		return result, nil
	}
	if binding.ResourceType != model.VideoIdempotencyResourceTask {
		return result, ErrIdempotencyBindingConflict
	}
	var task model.Task
	err = db.WithContext(ctx).Select("id", "task_id").Where("task_id = ? AND user_id = ?", binding.ResourceID, userID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		result.Status = VideoSubmissionStatusPending
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Status = VideoSubmissionStatusAccepted
	result.TaskID = task.TaskID
	var generation model.KKAIVideoGeneration
	if err := db.WithContext(ctx).Select("id").Where("task_id = ? AND user_id = ?", task.ID, userID).First(&generation).Error; err == nil {
		result.GenerationID = &generation.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	return result, nil
}
