package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	videoTaskResultArchiveSourcePrefix = "task-result://"
	videoTaskOutputReconcileScanPages  = 2
)

func videoTaskResultArchiveSource(taskID int64) string {
	return videoTaskResultArchiveSourcePrefix + strconv.FormatInt(taskID, 10)
}

func parseVideoTaskResultArchiveSource(source string) (int64, bool) {
	if !strings.HasPrefix(source, videoTaskResultArchiveSourcePrefix) {
		return 0, false
	}
	taskID, err := strconv.ParseInt(strings.TrimPrefix(source, videoTaskResultArchiveSourcePrefix), 10, 64)
	return taskID, err == nil && taskID > 0
}

func videoTaskArchiveSource(task model.Task) string {
	archiveSource := strings.TrimSpace(task.PrivateData.ArchiveSource)
	if archiveSource == "" {
		archiveSource = strings.TrimSpace(task.PrivateData.ResultURL)
	}
	if archiveSource != "" {
		return archiveSource
	}
	legacySource := strings.TrimSpace(task.FailReason)
	normalizedLegacySource := strings.ToLower(legacySource)
	if strings.HasPrefix(normalizedLegacySource, "http://") ||
		strings.HasPrefix(normalizedLegacySource, "https://") ||
		strings.HasPrefix(normalizedLegacySource, "data:video/") {
		return legacySource
	}
	return ""
}

type videoTaskOutputReconciler struct {
	lastGenerationID int64
}

func ReconcileVideoTaskOutputs(ctx context.Context, db *gorm.DB, limit int) (int, error) {
	reconciler := &videoTaskOutputReconciler{}
	return reconciler.Reconcile(ctx, db, limit)
}

func (reconciler *videoTaskOutputReconciler) Reconcile(ctx context.Context, db *gorm.DB, limit int) (int, error) {
	if reconciler == nil || db == nil || limit <= 0 {
		return 0, ErrInvalidVideoOutboxEvent
	}
	if limit > 500 {
		limit = 500
	}
	scanBudget := limit * videoTaskOutputReconcileScanPages
	created := 0
	scanned := 0
	startID := reconciler.lastGenerationID
	lastID := startID
	wrapped := false
	for scanned < scanBudget && created < limit {
		pageSize := limit
		if remaining := scanBudget - scanned; remaining < pageSize {
			pageSize = remaining
		}
		query := db.WithContext(ctx).Model(&model.KKAIVideoGeneration{}).
			Joins("JOIN tasks ON tasks.id = kkai_video_generations.task_id").
			Where("kkai_video_generations.deleted_at = 0 AND tasks.status = ?", model.TaskStatusSuccess).
			Where("kkai_video_generations.id > ?", lastID).
			Where("NOT EXISTS (SELECT 1 FROM kkai_video_task_assets WHERE kkai_video_task_assets.task_id = kkai_video_generations.task_id AND kkai_video_task_assets.role = ?)", model.VideoTaskAssetRoleOutput)
		if wrapped {
			query = query.Where("kkai_video_generations.id <= ?", startID)
		}
		var generations []model.KKAIVideoGeneration
		err := query.
			Order("kkai_video_generations.id ASC").Limit(pageSize).
			Select("kkai_video_generations.*").Find(&generations).Error
		if err != nil {
			return created, fmt.Errorf("find video tasks awaiting archive: %w", err)
		}
		if len(generations) == 0 {
			if wrapped || startID == 0 {
				lastID = 0
				break
			}
			wrapped = true
			lastID = 0
			continue
		}
		for _, generation := range generations {
			scanned++
			lastID = generation.ID
			wasCreated, err := reconcileVideoTaskOutput(ctx, db, generation.ID)
			if err != nil {
				return created, fmt.Errorf("create video archive asset: %w", err)
			}
			if wasCreated {
				created++
				if created == limit {
					break
				}
			}
		}
		if created == limit {
			break
		}
		if len(generations) < pageSize {
			if wrapped || startID == 0 {
				lastID = 0
				break
			}
			wrapped = true
			lastID = 0
		}
	}
	reconciler.lastGenerationID = lastID
	return created, nil
}

func reconcileVideoTaskOutput(ctx context.Context, db *gorm.DB, generationID int64) (bool, error) {
	return reconcileVideoTaskOutputExpected(ctx, db, generationID, nil)
}

func reconcileVideoTaskOutputExpected(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	expected *VideoTaskArchiveOnceInput,
) (bool, error) {
	created := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation model.KKAIVideoGeneration
		if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&generation, "id = ?", generationID).Error; err != nil {
			return err
		}
		if generation.DeletedAt != 0 {
			return nil
		}
		var task model.Task
		if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", generation.TaskID).Error; err != nil {
			return err
		}
		if expected != nil {
			if err := validateVideoTaskArchiveOncePair(*expected, generation, task); err != nil {
				return err
			}
			var links []model.KKAIVideoTaskAsset
			if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).Where(
				"task_id = ? AND role = ?", task.ID, model.VideoTaskAssetRoleOutput,
			).Order("position ASC").Find(&links).Error; err != nil {
				return err
			}
			if len(links) > 0 {
				if len(links) != 1 || links[0].Position != 0 || links[0].AssetID <= 0 {
					return ErrVideoTaskArchiveOnceCorrupt
				}
				var asset model.KKAIVideoAsset
				if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&asset, "id = ?", links[0].AssetID).Error; err != nil {
					return err
				}
				if err := validateVideoTaskArchiveOnceAsset(*expected, task, asset); err != nil {
					return err
				}
				return nil
			}
		}
		if task.Status != model.TaskStatusSuccess {
			return nil
		}
		archiveSource := videoTaskArchiveSource(task)
		if archiveSource == "" {
			if expected != nil {
				return ErrVideoTaskArchiveOnceBlocked
			}
			return nil
		}
		privateDataChanged := task.PrivateData.ArchiveSource != archiveSource
		task.PrivateData.ArchiveSource = archiveSource
		if !task.PrivateData.AssetHostedResult {
			task.PrivateData.AssetHostedResult = true
			privateDataChanged = true
		}
		if privateDataChanged {
			if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error; err != nil {
				return err
			}
		}
		var existing int64
		if err := tx.Model(&model.KKAIVideoTaskAsset{}).Where(
			"task_id = ? AND role = ?", generation.TaskID, model.VideoTaskAssetRoleOutput,
		).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		now := time.Now().Unix()
		asset := model.KKAIVideoAsset{
			OwnerUserID: generation.UserID, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
			State:            model.VideoAssetStateProcessing,
			ObjectKey:        fmt.Sprintf("users/%d/generations/%d/source.mp4", generation.UserID, generation.ID),
			ArchiveSourceURL: videoTaskResultArchiveSource(task.ID), OriginalFilename: "generated-video.mp4", MIMEType: "video/mp4",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		link := model.KKAIVideoTaskAsset{
			TaskID: generation.TaskID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput,
			Position: 0, CreatedAt: now,
		}
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		archiveTopic := VideoOutboxTopicArchive
		if expected != nil {
			archiveTopic = videoOutboxTopicArchiveOnce
		}
		if err := EnqueueVideoOutboxEvent(ctx, tx,
			fmt.Sprintf("video:task:%d:archive:v1", generation.TaskID), archiveTopic,
			strconv.FormatInt(asset.ID, 10), VideoAssetEventPayload{AssetID: asset.ID},
		); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}
