package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"gorm.io/gorm"
)

// TaskVideoOutput is a read-only, authorized association. Object keys remain
// private; only the signing methods below expose browser-facing URLs.
type TaskVideoOutput struct {
	Media  dto.TaskVideoMedia
	taskID int64
	asset  *model.KKAIVideoAsset
}

// TaskUsesStudioAssets keeps legacy capability routes from exposing an upstream
// result before the task's AssetHostedResult flag has been backfilled.
func TaskUsesStudioAssets(ctx context.Context, db *gorm.DB, task *model.Task) (bool, error) {
	if task.IsAssetHostedResult() {
		return true, nil
	}
	switch constant.NormalizeTaskAction(task.Action) {
	case constant.TaskActionImageToVideo, constant.TaskActionTextToVideo, constant.TaskActionFirstTailToVideo,
		constant.TaskActionReferenceToVideo, constant.TaskActionPluginRemix:
		var count int64
		err := db.WithContext(ctx).Model(&model.KKAIVideoGeneration{}).Where("task_id = ?", task.ID).Count(&count).Error
		return count > 0, err
	default:
		return false, nil
	}
}

// LoadTaskVideoOutputs consumes existing Studio assets, never enqueues archive
// work and never contacts object storage. Numeric task IDs avoid legacy public
// task ID collisions. User assets retain their owner binding for admin reads.
func LoadTaskVideoOutputs(ctx context.Context, db *gorm.DB, tasks []*model.Task, viewerID int, adminView bool) (map[int64]*TaskVideoOutput, error) {
	outputs := make(map[int64]*TaskVideoOutput)
	if len(tasks) == 0 {
		return outputs, nil
	}
	taskIDs := make([]int64, 0, len(tasks))
	userIDs := []int{viewerID}
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
		userIDs = append(userIDs, task.UserId)
		if task.IsAssetHostedResult() {
			outputs[task.ID] = &TaskVideoOutput{taskID: task.ID, Media: dto.TaskVideoMedia{Source: "studio", State: "unavailable"}}
		}
	}
	var generations []model.KKAIVideoGeneration
	if err := db.WithContext(ctx).Where("task_id IN ?", taskIDs).Find(&generations).Error; err != nil {
		return outputs, err
	}
	byTask := make(map[int64]model.KKAIVideoGeneration, len(generations))
	for _, generation := range generations {
		byTask[generation.TaskID] = generation
		outputs[generation.TaskID] = &TaskVideoOutput{taskID: generation.TaskID, Media: dto.TaskVideoMedia{Source: "studio", State: "unavailable"}}
	}
	if len(outputs) == 0 && !adminView {
		return outputs, nil
	}
	for _, task := range tasks {
		if outputs[task.ID] != nil {
			// Keep legacy fallback closed even if a later media query fails.
			task.PrivateData.AssetHostedResult = true
		}
	}
	var users []model.User
	if err := db.WithContext(ctx).Select("id", "username", "role", "status", "tenant_id").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return outputs, err
	}
	byUser := make(map[int]model.User, len(users))
	for _, user := range users {
		byUser[user.Id] = user
	}
	if adminView {
		for _, task := range tasks {
			task.Username = byUser[task.UserId].Username
		}
	}
	if len(outputs) == 0 {
		return outputs, nil
	}
	viewer, viewerExists := byUser[viewerID]
	canRead := viewerExists && viewer.Status == common.UserStatusEnabled && video_studio_setting.CanAccess(viewer.Role)
	canReadOthers := adminView && viewer.Role >= common.RoleAdminUser && viewer.TenantID <= model.PlatformTenantID
	var links []model.KKAIVideoTaskAsset
	if err := db.WithContext(ctx).Where("task_id IN ? AND role = ? AND position = ?", taskIDs, model.VideoTaskAssetRoleOutput, 0).Find(&links).Error; err != nil {
		return outputs, err
	}
	byLink := make(map[int64]model.KKAIVideoTaskAsset, len(links))
	duplicate := make(map[int64]bool)
	assetIDs := make([]int64, 0, len(links))
	for _, link := range links {
		if _, exists := byLink[link.TaskID]; exists {
			duplicate[link.TaskID] = true
		}
		byLink[link.TaskID] = link
		assetIDs = append(assetIDs, link.AssetID)
	}
	var assets []model.KKAIVideoAsset
	if len(assetIDs) > 0 {
		if err := db.WithContext(ctx).Where("id IN ?", assetIDs).Find(&assets).Error; err != nil {
			return outputs, err
		}
	}
	byAsset := make(map[int64]model.KKAIVideoAsset, len(assets))
	for _, asset := range assets {
		byAsset[asset.ID] = asset
	}
	for _, task := range tasks {
		output := outputs[task.ID]
		if output == nil {
			continue
		}
		owner, ownerExists := byUser[task.UserId]
		if !canRead || !ownerExists || owner.Status != common.UserStatusEnabled || owner.TenantID != task.TenantID ||
			(task.UserId != viewerID && !canReadOthers) || (!canReadOthers && viewer.TenantID != task.TenantID) {
			output.Media.State = "restricted"
			continue
		}
		generation, hasGeneration := byTask[task.ID]
		if !hasGeneration || generation.UserID != task.UserId || generation.DeletedAt != 0 || duplicate[task.ID] || task.PrivateData.ResultDiscarded {
			continue
		}
		link, hasLink := byLink[task.ID]
		asset, hasAsset := byAsset[link.AssetID]
		if !hasLink {
			switch task.Status {
			case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress:
				output.Media.State = "processing"
			}
			continue
		}
		if !hasAsset || asset.OwnerUserID != task.UserId || asset.Scope != model.VideoAssetScopeUser || asset.Kind != model.VideoAssetKindOutput || asset.DeletedAt != 0 {
			continue
		}
		switch asset.State {
		case model.VideoAssetStatePendingUpload, model.VideoAssetStateUploaded, model.VideoAssetStateProcessing:
			output.Media.State = "processing"
		case model.VideoAssetStateReady:
			if asset.ObjectKey == "" {
				continue
			}
			output.Media.State = "ready"
			output.asset = &asset
			output.Media.Width = max(0, asset.Width)
			output.Media.Height = max(0, asset.Height)
			if asset.DurationSeconds > 0 && !math.IsNaN(asset.DurationSeconds) && !math.IsInf(asset.DurationSeconds, 0) {
				output.Media.DurationSeconds = asset.DurationSeconds
			}
		}
	}
	return outputs, nil
}

func (output *TaskVideoOutput) SignPoster(ctx context.Context, store VideoAssetStore) error {
	if output.Media.State != "ready" || output.asset == nil || output.asset.PosterObjectKey == "" {
		return nil
	}
	if store == nil {
		return ErrInvalidVideoAssetStoreRequest
	}
	ttl := time.Duration(video_studio_setting.Get().SignedURLSeconds) * time.Second
	location, err := store.PresignDownload(ctx, output.asset.PosterObjectKey, "poster.jpg", false, ttl)
	if err != nil {
		return err
	}
	output.Media.PosterURL = location
	output.Media.PosterExpiresAt = time.Now().Add(ttl).Unix()
	return nil
}

func (output *TaskVideoOutput) SignContent(ctx context.Context, store VideoAssetStore) (*dto.TaskHostedVideo, error) {
	view := &dto.TaskHostedVideo{TaskRecordID: output.taskID, State: output.Media.State,
		Width: output.Media.Width, Height: output.Media.Height, DurationSeconds: output.Media.DurationSeconds}
	if output.Media.State != "ready" || output.asset == nil {
		return view, nil
	}
	if store == nil {
		return nil, ErrInvalidVideoAssetStoreRequest
	}
	ttl := time.Duration(video_studio_setting.Get().SignedURLSeconds) * time.Second
	filename := sanitizeVideoAssetFilename(output.asset.OriginalFilename)
	if filename == "" {
		filename = fmt.Sprintf("video-%d.mp4", output.taskID)
	}
	var err error
	view.ContentURL, err = store.PresignDownload(ctx, output.asset.ObjectKey, filename, false, ttl)
	if err != nil {
		return nil, err
	}
	view.DownloadURL, err = store.PresignDownload(ctx, output.asset.ObjectKey, filename, true, ttl)
	if err != nil {
		return nil, err
	}
	view.ExpiresAt = time.Now().Add(ttl).Unix()
	return view, nil
}
