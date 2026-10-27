package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// TaskVideoPosterSnapshot is derivative metadata only. Source credentials and
// URLs remain in their existing task/channel boundary, never in queue payloads.
type TaskVideoPosterSnapshot struct {
	State       string `json:"state"`
	Version     string `json:"version"`
	ObjectKey   string `json:"object_key,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

// PrepareTaskVideoPoster is an atomic, bounded retry transition. An unavailable
// poster gets one later attempt; duplicate page loads cannot enqueue again.
func PrepareTaskVideoPoster(ctx context.Context, tx *gorm.DB, taskID int64, artifactKey, version string, now int64) (TaskVideoPosterSnapshot, bool, error) {
	var task Task
	if err := lockForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", taskID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return TaskVideoPosterSnapshot{}, false, nil
		}
		return TaskVideoPosterSnapshot{}, false, err
	}
	if task.Status != TaskStatusSuccess || !task.ResultRetrievable() || task.IsAssetHostedResult() || version != TaskVideoPosterVersion(&task, artifactKey) {
		return TaskVideoPosterSnapshot{}, false, nil
	}
	current, exists := task.PrivateData.VideoPosters[artifactKey]
	attempt := 0
	if exists && current.Version == version {
		if current.State != "unavailable" || current.Attempt >= 1 || current.UpdatedAt > now-30*60 {
			return current, false, nil
		}
		attempt = current.Attempt + 1
	}
	poster := TaskVideoPosterSnapshot{State: "pending", Version: version, Attempt: attempt, UpdatedAt: now}
	if task.PrivateData.VideoPosters == nil {
		task.PrivateData.VideoPosters = make(map[string]TaskVideoPosterSnapshot)
	}
	task.PrivateData.VideoPosters[artifactKey] = poster
	result := tx.WithContext(ctx).Model(&Task{}).Where("id = ?", taskID).Update("private_data", task.PrivateData)
	return poster, result.RowsAffected == 1, result.Error
}

func TaskVideoPosterVersion(task *Task, artifactKey string) string {
	if task == nil {
		return ""
	}
	// Retain semantic URL identity. Strip only recognized presign dialects;
	// unknown signatures may render again, but cannot reuse a wrong cover.
	resultURL := strings.TrimSpace(task.GetResultURL())
	if parsed, err := url.Parse(resultURL); err == nil && parsed.IsAbs() && parsed.Host != "" {
		query := parsed.Query()
		aws, google, azure := false, false, false
		for name := range query {
			switch strings.ToLower(name) {
			case "x-amz-signature":
				aws = true
			case "x-goog-signature":
				google = true
			case "sig":
				azure = query.Has("se") && query.Has("sv")
			}
		}
		for name := range query {
			key := strings.ToLower(name)
			if aws && strings.HasPrefix(key, "x-amz-") ||
				google && strings.HasPrefix(key, "x-goog-") ||
				azure && (key == "sig" || key == "se" || key == "sp" || key == "sv" || key == "st" || key == "sr" || strings.HasPrefix(key, "sk")) {
				query.Del(name)
			}
		}
		parsed.RawQuery = query.Encode()
		parsed.Fragment = ""
		resultURL = parsed.String()
	}
	// Plugin outputs can change inside Data without changing the task ID. Legacy
	// response Data may only renew a signed URL, so exclude it from that identity.
	var pluginDataHash [sha256.Size]byte
	if task.PrivateData.Execution != nil && task.PrivateData.Execution.TaskPlugin != nil {
		pluginDataHash = sha256.Sum256(task.Data)
	}
	identity := fmt.Sprintf("v2:%d:%d:%s:%s:%s:%x", task.ID, task.FinishTime, task.GetUpstreamTaskID(), artifactKey, resultURL, pluginDataHash)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

// UpdateTaskVideoPoster must run inside the caller's transaction. Locking and
// merging the fresh private_data preserves concurrent billing/failure updates.
func UpdateTaskVideoPoster(ctx context.Context, tx *gorm.DB, taskID int64, artifactKey string, poster TaskVideoPosterSnapshot, onlyMissing bool) (bool, error) {
	var task Task
	if err := lockForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", taskID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	if task.Status != TaskStatusSuccess || !task.ResultRetrievable() || task.IsAssetHostedResult() ||
		poster.Version != TaskVideoPosterVersion(&task, artifactKey) {
		return false, nil
	}
	current, exists := task.PrivateData.VideoPosters[artifactKey]
	if onlyMissing {
		if exists && current.Version == poster.Version {
			return false, nil
		}
	} else if !exists || current.Version != poster.Version || current.State != "pending" || current.Attempt != poster.Attempt {
		return false, nil
	}
	if task.PrivateData.VideoPosters == nil {
		task.PrivateData.VideoPosters = make(map[string]TaskVideoPosterSnapshot)
	}
	task.PrivateData.VideoPosters[artifactKey] = poster
	result := tx.WithContext(ctx).Model(&Task{}).Where("id = ?", taskID).Update("private_data", task.PrivateData)
	return result.RowsAffected == 1, result.Error
}
