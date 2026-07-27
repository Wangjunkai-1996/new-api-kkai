package model

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

const AssetHostedTaskPublicFailureReason = "video generation failed"

func (t *Task) IsAssetHostedResult() bool {
	return t != nil && t.PrivateData.AssetHostedResult
}

func (t *Task) PublicResultURL() string {
	if t == nil || t.IsAssetHostedResult() {
		return ""
	}
	return t.GetResultURL()
}

func (t *Task) PublicData() json.RawMessage {
	if t == nil || t.IsAssetHostedResult() {
		return nil
	}
	return t.Data
}

func (t *Task) PublicFailReason() string {
	if t == nil || strings.TrimSpace(t.FailReason) == "" {
		return ""
	}
	return t.PublicFailureReason(t.FailReason)
}

// PublicFailureReason is the single disclosure boundary for provider failure
// text. Asset-hosted tasks keep provider diagnostics out of user-facing task
// and billing responses regardless of the provider's message format.
func (t *Task) PublicFailureReason(reason string) string {
	if t == nil {
		return ""
	}
	if t.IsAssetHostedResult() {
		return AssetHostedTaskPublicFailureReason
	}
	return reason
}

// ClearAssetHostedTaskResultSource clears temporary provider material without
// replacing concurrently updated billing or accounting fields in private_data.
// The expected source guards against a stale archive worker clearing a newer
// provider result.
func ClearAssetHostedTaskResultSource(ctx context.Context, tx *gorm.DB, taskID int64, expectedSource string) (bool, error) {
	if tx == nil || taskID <= 0 {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var task Task
	if err := lockForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", taskID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	if !task.IsAssetHostedResult() || task.PrivateData.ArchiveSource != expectedSource {
		return false, nil
	}
	if task.PrivateData.ResultURL == "" && task.PrivateData.ArchiveSource == "" {
		return true, nil
	}
	privateData := task.PrivateData
	privateData.ResultURL = ""
	privateData.ArchiveSource = ""
	result := tx.WithContext(ctx).Model(&Task{}).Where("id = ?", taskID).Update("private_data", privateData)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
