package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var (
	ErrVideoGenerationNotFound      = errors.New("video generation not found")
	ErrVideoGenerationDeleted       = errors.New("video generation is already deleted")
	ErrInvalidVideoGenerationFilter = errors.New("invalid video generation filter")
)

type VideoGenerationView struct {
	ID             int64          `json:"id"`
	TaskID         string         `json:"task_id"`
	ModelProfileID int64          `json:"model_profile_id"`
	SampleID       *int64         `json:"sample_id,omitempty"`
	Model          string         `json:"model"`
	Mode           string         `json:"mode"`
	Prompt         string         `json:"prompt"`
	Parameters     map[string]any `json:"parameters"`
	Status         string         `json:"status"`
	Progress       string         `json:"progress"`
	FailureReason  string         `json:"failure_reason,omitempty"`
	Quota          int            `json:"quota"`
	OutputAssetID  *int64         `json:"output_asset_id,omitempty"`
	VideoURL       string         `json:"video_url,omitempty"`
	PosterURL      string         `json:"poster_url,omitempty"`
	DownloadURL    string         `json:"download_url,omitempty"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
}

type VideoGenerationPage struct {
	Items      []VideoGenerationView `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type VideoGenerationListRequest struct {
	Cursor string
	Status string
	Limit  int
}

func CreateVideoGeneration(ctx context.Context, tx *gorm.DB, normalized *NormalizedVideoStudioSubmission, taskID int64) (*model.KKAIVideoGeneration, error) {
	if tx == nil || normalized == nil || taskID <= 0 {
		return nil, ErrInvalidVideoStudioSubmission
	}
	parameters, err := common.Marshal(normalized.Parameters)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	generation := model.KKAIVideoGeneration{
		TaskID: taskID, ModelProfileID: normalized.ProfileID, SampleID: normalized.SampleID,
		Model: normalized.Model, Mode: normalized.Mode, Prompt: normalized.Prompt,
		Parameters: string(parameters), CreatedAt: now, UpdatedAt: now,
	}
	var task model.Task
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", taskID).Error; err != nil {
		return nil, fmt.Errorf("load video task owner: %w", err)
	}
	generation.UserID = task.UserId
	if !task.PrivateData.AssetHostedResult {
		task.PrivateData.AssetHostedResult = true
		if err := tx.WithContext(ctx).Model(&model.Task{}).Where("id = ?", task.ID).
			Update("private_data", task.PrivateData).Error; err != nil {
			return nil, fmt.Errorf("mark video task result as asset hosted: %w", err)
		}
	}
	if err := tx.WithContext(ctx).Create(&generation).Error; err != nil {
		return nil, fmt.Errorf("create video generation: %w", err)
	}
	for position, reference := range normalized.ReferenceAssets {
		var current model.KKAIVideoAsset
		if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).Select("id, owner_user_id, scope, kind, state, deleted_at").
			First(&current, "id = ?", reference.Asset.ID).Error; err != nil {
			return nil, ErrInvalidVideoStudioSubmission
		}
		if current.Kind != model.VideoAssetKindReference || current.State != model.VideoAssetStateReady || current.DeletedAt != 0 {
			return nil, ErrInvalidVideoStudioSubmission
		}
		if current.Scope == model.VideoAssetScopeCatalog {
			published, err := isPublishedVideoCatalogAsset(ctx, tx, current.ID)
			if err != nil {
				return nil, err
			}
			if !published {
				return nil, ErrInvalidVideoStudioSubmission
			}
		} else if current.Scope != model.VideoAssetScopeUser || current.OwnerUserID != generation.UserID {
			return nil, ErrInvalidVideoStudioSubmission
		}
		link := model.KKAIVideoTaskAsset{
			TaskID: taskID, AssetID: reference.Asset.ID, Role: reference.Role,
			Position: position, CreatedAt: now,
		}
		if err := tx.WithContext(ctx).Create(&link).Error; err != nil {
			return nil, fmt.Errorf("link video reference asset: %w", err)
		}
	}
	return &generation, nil
}

func ListVideoGenerations(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	request VideoGenerationListRequest,
) (VideoGenerationPage, error) {
	if db == nil || userID <= 0 {
		return VideoGenerationPage{}, ErrVideoGenerationNotFound
	}
	request.Status = strings.TrimSpace(request.Status)
	if request.Status != "" && request.Status != model.VideoAssetStateReady {
		return VideoGenerationPage{}, ErrInvalidVideoGenerationFilter
	}
	if request.Limit <= 0 {
		request.Limit = 20
	}
	if request.Limit > 50 {
		request.Limit = 50
	}
	query := db.WithContext(ctx).Model(&model.KKAIVideoGeneration{}).
		Where("kkai_video_generations.user_id = ? AND kkai_video_generations.deleted_at = 0", userID)
	if request.Status == model.VideoAssetStateReady {
		query = query.
			Joins("JOIN tasks AS video_tasks ON video_tasks.id = kkai_video_generations.task_id").
			Joins("JOIN kkai_video_task_assets AS video_outputs ON video_outputs.task_id = kkai_video_generations.task_id AND video_outputs.role = ? AND video_outputs.position = ?", model.VideoTaskAssetRoleOutput, 0).
			Joins("JOIN kkai_video_assets AS video_output_assets ON video_output_assets.id = video_outputs.asset_id AND video_output_assets.state = ? AND video_output_assets.deleted_at = 0", model.VideoAssetStateReady).
			Where("video_tasks.status = ?", model.TaskStatusSuccess).
			Select("kkai_video_generations.*")
	}
	if request.Cursor != "" {
		cursorID, err := strconv.ParseInt(request.Cursor, 10, 64)
		if err != nil || cursorID <= 0 {
			return VideoGenerationPage{}, ErrVideoGenerationNotFound
		}
		query = query.Where("kkai_video_generations.id < ?", cursorID)
	}
	var generations []model.KKAIVideoGeneration
	if err := query.Order("kkai_video_generations.id DESC").Limit(request.Limit + 1).Find(&generations).Error; err != nil {
		return VideoGenerationPage{}, fmt.Errorf("list video generations: %w", err)
	}
	hasMore := len(generations) > request.Limit
	if hasMore {
		generations = generations[:request.Limit]
	}
	items, err := buildVideoGenerationViews(ctx, db, generations)
	if err != nil {
		return VideoGenerationPage{}, err
	}
	page := VideoGenerationPage{Items: items}
	if hasMore && len(generations) > 0 {
		page.NextCursor = strconv.FormatInt(generations[len(generations)-1].ID, 10)
	}
	return page, nil
}

func GetVideoGeneration(ctx context.Context, db *gorm.DB, userID int, id int64) (*VideoGenerationView, error) {
	var generation model.KKAIVideoGeneration
	if db == nil || userID <= 0 || id <= 0 {
		return nil, ErrVideoGenerationNotFound
	}
	if err := db.WithContext(ctx).First(&generation, "id = ? AND user_id = ? AND deleted_at = 0", id, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoGenerationNotFound
		}
		return nil, fmt.Errorf("get video generation: %w", err)
	}
	views, err := buildVideoGenerationViews(ctx, db, []model.KKAIVideoGeneration{generation})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func DeleteVideoGeneration(ctx context.Context, db *gorm.DB, userID int, id int64) error {
	if db == nil || userID <= 0 || id <= 0 {
		return ErrVideoGenerationNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation model.KKAIVideoGeneration
		if err := lockVideoRowsForUpdate(tx).First(&generation, "id = ? AND user_id = ?", id, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoGenerationNotFound
			}
			return err
		}
		if generation.DeletedAt != 0 {
			return nil
		}
		now := time.Now().Unix()
		if err := tx.Model(&generation).Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		var assets []model.KKAIVideoAsset
		if err := tx.Model(&model.KKAIVideoAsset{}).
			Joins("JOIN kkai_video_task_assets ON kkai_video_task_assets.asset_id = kkai_video_assets.id").
			Where("kkai_video_task_assets.task_id = ? AND kkai_video_task_assets.role = ? AND kkai_video_assets.owner_user_id = ?",
				generation.TaskID, model.VideoTaskAssetRoleOutput, userID).
			Select("kkai_video_assets.*").Find(&assets).Error; err != nil {
			return err
		}
		for _, asset := range assets {
			if asset.State == model.VideoAssetStateDeleted || asset.State == model.VideoAssetStateDeleting {
				continue
			}
			if err := tx.Model(&asset).Updates(map[string]any{"state": model.VideoAssetStateDeleting, "updated_at": now}).Error; err != nil {
				return err
			}
			if err := EnqueueVideoOutboxEvent(ctx, tx,
				fmt.Sprintf("video:asset:%d:delete", asset.ID), VideoOutboxTopicDelete,
				strconv.FormatInt(asset.ID, 10), VideoAssetEventPayload{AssetID: asset.ID},
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildVideoGenerationViews(ctx context.Context, db *gorm.DB, generations []model.KKAIVideoGeneration) ([]VideoGenerationView, error) {
	if len(generations) == 0 {
		return []VideoGenerationView{}, nil
	}
	taskIDs := make([]int64, 0, len(generations))
	for _, generation := range generations {
		taskIDs = append(taskIDs, generation.TaskID)
	}
	var tasks []model.Task
	if err := db.WithContext(ctx).Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
		return nil, err
	}
	var outputLinks []model.KKAIVideoTaskAsset
	if err := db.WithContext(ctx).Where(
		"task_id IN ? AND role = ? AND position = ?", taskIDs, model.VideoTaskAssetRoleOutput, 0,
	).Find(&outputLinks).Error; err != nil {
		return nil, err
	}
	assetIDs := make([]int64, 0, len(outputLinks))
	for _, link := range outputLinks {
		assetIDs = append(assetIDs, link.AssetID)
	}
	var assets []model.KKAIVideoAsset
	if len(assetIDs) > 0 {
		if err := db.WithContext(ctx).Where("id IN ?", assetIDs).Find(&assets).Error; err != nil {
			return nil, err
		}
	}
	tasksByID := make(map[int64]model.Task, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
	}
	outputByTask := make(map[int64]int64, len(outputLinks))
	for _, link := range outputLinks {
		outputByTask[link.TaskID] = link.AssetID
	}
	assetsByID := make(map[int64]model.KKAIVideoAsset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}
	views := make([]VideoGenerationView, 0, len(generations))
	for _, generation := range generations {
		parameters := map[string]any{}
		if err := common.UnmarshalJsonStr(generation.Parameters, &parameters); err != nil {
			return nil, fmt.Errorf("decode video generation %d parameters: %w", generation.ID, err)
		}
		task, ok := tasksByID[generation.TaskID]
		if !ok {
			return nil, fmt.Errorf("video generation %d references missing task %d", generation.ID, generation.TaskID)
		}
		// A generation row is authoritative even before legacy tasks are backfilled.
		task.PrivateData.AssetHostedResult = true
		view := VideoGenerationView{
			ID: generation.ID, TaskID: task.TaskID, ModelProfileID: generation.ModelProfileID,
			SampleID: generation.SampleID, Model: generation.Model, Mode: generation.Mode,
			Prompt: generation.Prompt, Parameters: parameters, Progress: task.Progress,
			FailureReason: task.PublicFailReason(), Quota: task.Quota, CreatedAt: generation.CreatedAt, UpdatedAt: generation.UpdatedAt,
		}
		assetID, hasOutput := outputByTask[generation.TaskID]
		var output *model.KKAIVideoAsset
		if hasOutput {
			asset := assetsByID[assetID]
			output = &asset
			view.OutputAssetID = &assetID
		}
		view.Status = videoGenerationStatus(task, output)
		if output != nil && output.State == model.VideoAssetStateReady {
			view.VideoURL = videoAssetContentPath(output.ID, "")
			view.PosterURL = videoAssetContentPath(output.ID, "poster")
			view.DownloadURL = "/api/video-studio/assets/" + strconv.FormatInt(output.ID, 10) + "/download"
		}
		views = append(views, view)
	}
	return views, nil
}

func videoGenerationStatus(task model.Task, output *model.KKAIVideoAsset) string {
	switch task.Status {
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued:
		return "queued"
	case model.TaskStatusInProgress:
		return "processing"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusUnknown:
		return "processing"
	case model.TaskStatusSuccess:
		if output == nil {
			return "archiving"
		}
		switch output.State {
		case model.VideoAssetStateReady:
			return "ready"
		case model.VideoAssetStateFailed, model.VideoAssetStateDeleted:
			return "failed"
		default:
			return "archiving"
		}
	default:
		return "queued"
	}
}
