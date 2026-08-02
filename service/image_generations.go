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
	ErrImageGenerationNotFound      = errors.New("image generation not found")
	ErrImageGenerationConflict      = errors.New("image generation state changed concurrently")
	ErrInvalidImageGenerationFilter = errors.New("invalid image generation filter")
)

type ImageGenerationFilter struct {
	Model  string
	Status string
	Cursor string
	Limit  int
}

type ImageGenerationPage struct {
	Items      []ImageGenerationView `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type ImageAssetView struct {
	ID             int64  `json:"id"`
	Position       int    `json:"position"`
	State          string `json:"state"`
	ThumbnailState string `json:"thumbnail_state"`
	MIMEType       string `json:"mime_type"`
	SizeBytes      int64  `json:"size_bytes"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	FailureReason  string `json:"failure_reason,omitempty"`
	ContentURL     string `json:"content_url,omitempty"`
	ThumbnailURL   string `json:"thumbnail_url,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
}

type ImageGenerationView struct {
	ID                   int64            `json:"id"`
	ModelProfileID       int64            `json:"model_profile_id"`
	SampleID             *int64           `json:"sample_id,omitempty"`
	SpecificationVersion int              `json:"specification_version"`
	Model                string           `json:"model"`
	Prompt               string           `json:"prompt"`
	Parameters           map[string]any   `json:"parameters"`
	RequestID            string           `json:"request_id"`
	Status               string           `json:"status"`
	RequestedCount       int              `json:"requested_count"`
	SucceededCount       int              `json:"succeeded_count"`
	FinalQuota           int              `json:"final_quota"`
	FailureStage         string           `json:"failure_stage,omitempty"`
	ErrorCode            string           `json:"error_code,omitempty"`
	ErrorMessage         string           `json:"error_message,omitempty"`
	StartedAt            int64            `json:"started_at"`
	FinishedAt           int64            `json:"finished_at"`
	CreatedAt            int64            `json:"created_at"`
	Assets               []ImageAssetView `json:"assets"`
}

func CreateImageGeneration(
	ctx context.Context,
	db *gorm.DB,
	normalized *NormalizedImageStudioSubmission,
	requestID string,
	reservationID int64,
) (*model.KKAIImageGeneration, error) {
	requestID = strings.TrimSpace(requestID)
	if db == nil || normalized == nil || normalized.UserID <= 0 || normalized.TokenID <= 0 ||
		normalized.ProfileID <= 0 || !validImageStudioHash(normalized.RequestHash) ||
		requestID == "" || len(requestID) > 64 || reservationID <= 0 {
		return nil, ErrInvalidImageStudioSubmission
	}
	parameters, err := common.Marshal(normalized.Parameters)
	if err != nil {
		return nil, fmt.Errorf("encode image generation parameters: %w", err)
	}
	now := time.Now().Unix()
	generation := &model.KKAIImageGeneration{
		UserID: normalized.UserID, TokenID: normalized.TokenID, ModelProfileID: normalized.ProfileID,
		SampleID: normalized.SampleID, SpecificationVersion: normalized.SpecificationVersion,
		Model: normalized.Model, Prompt: normalized.Prompt, Parameters: string(parameters),
		RequestHash: normalized.RequestHash, RequestID: requestID,
		Status: model.ImageGenerationStatusSubmitting, RequestedCount: normalized.RequestedCount,
		BillingState: model.ImageGenerationBillingStatePending,
		HeartbeatAt:  now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(generation).Error; err != nil {
			return err
		}
		return BindIdempotencyResource(
			ctx, tx, reservationID, model.ImageIdempotencyResourceGeneration, strconv.FormatInt(generation.ID, 10),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("create image generation: %w", err)
	}
	return generation, nil
}

func GetOwnedImageGeneration(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	generationID int64,
) (*model.KKAIImageGeneration, error) {
	if db == nil || userID <= 0 || generationID <= 0 {
		return nil, ErrImageGenerationNotFound
	}
	var generation model.KKAIImageGeneration
	if err := db.WithContext(ctx).First(
		&generation, "id = ? AND user_id = ? AND deleted_at = 0", generationID, userID,
	).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrImageGenerationNotFound
		}
		return nil, fmt.Errorf("get image generation: %w", err)
	}
	return &generation, nil
}

func GetImageGenerationView(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	generationID int64,
) (*ImageGenerationView, error) {
	generation, err := GetOwnedImageGeneration(ctx, db, userID, generationID)
	if err != nil {
		return nil, err
	}
	var assets []model.KKAIImageAsset
	if err := db.WithContext(ctx).
		Where("generation_id = ? AND deleted_at = 0", generation.ID).
		Order("position ASC, id ASC").Find(&assets).Error; err != nil {
		return nil, fmt.Errorf("list image generation assets: %w", err)
	}
	view, err := buildImageGenerationView(*generation, assets)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func ListImageGenerations(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	filter ImageGenerationFilter,
) (*ImageGenerationPage, error) {
	filter.Model = strings.TrimSpace(filter.Model)
	filter.Status = strings.TrimSpace(filter.Status)
	if db == nil || userID <= 0 || len(filter.Model) > 191 || len(filter.Status) > 24 ||
		(filter.Status != "" && !validImageGenerationListStatus(filter.Status)) {
		return nil, ErrInvalidImageGenerationFilter
	}
	if filter.Limit == 0 {
		filter.Limit = 24
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return nil, ErrInvalidImageGenerationFilter
	}
	cursorID := int64(0)
	if filter.Cursor != "" {
		parsed, err := strconv.ParseInt(filter.Cursor, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, ErrInvalidImageGenerationFilter
		}
		cursorID = parsed
	}
	query := db.WithContext(ctx).Where("user_id = ? AND deleted_at = 0", userID)
	if filter.Model != "" {
		query = query.Where("model = ?", filter.Model)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if cursorID > 0 {
		query = query.Where("id < ?", cursorID)
	}
	var generations []model.KKAIImageGeneration
	if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&generations).Error; err != nil {
		return nil, fmt.Errorf("list image generations: %w", err)
	}
	hasMore := len(generations) > filter.Limit
	if hasMore {
		generations = generations[:filter.Limit]
	}
	page := &ImageGenerationPage{Items: make([]ImageGenerationView, 0, len(generations))}
	if len(generations) == 0 {
		return page, nil
	}
	ids := make([]int64, 0, len(generations))
	for _, generation := range generations {
		ids = append(ids, generation.ID)
	}
	var assets []model.KKAIImageAsset
	if err := db.WithContext(ctx).Where("generation_id IN ? AND deleted_at = 0", ids).
		Order("generation_id DESC, position ASC, id ASC").Find(&assets).Error; err != nil {
		return nil, fmt.Errorf("list image generation assets: %w", err)
	}
	assetsByGeneration := make(map[int64][]model.KKAIImageAsset, len(generations))
	for _, asset := range assets {
		if asset.GenerationID != nil {
			assetsByGeneration[*asset.GenerationID] = append(assetsByGeneration[*asset.GenerationID], asset)
		}
	}
	for _, generation := range generations {
		view, err := buildImageGenerationView(generation, assetsByGeneration[generation.ID])
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, view)
	}
	if hasMore {
		page.NextCursor = strconv.FormatInt(generations[len(generations)-1].ID, 10)
	}
	return page, nil
}

func DeleteImageGeneration(ctx context.Context, db *gorm.DB, userID int, generationID int64) error {
	if db == nil || userID <= 0 || generationID <= 0 {
		return ErrImageGenerationNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation model.KKAIImageGeneration
		if err := tx.First(&generation, "id = ? AND user_id = ? AND deleted_at = 0", generationID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrImageGenerationNotFound
			}
			return err
		}
		if generation.Status == model.ImageGenerationStatusSubmitting {
			return ErrImageGenerationConflict
		}
		now := time.Now()
		if err := tx.Model(&model.KKAIImageGeneration{}).Where("id = ? AND deleted_at = 0", generation.ID).
			Updates(map[string]any{"deleted_at": now.Unix(), "updated_at": now.Unix()}).Error; err != nil {
			return err
		}
		var assets []model.KKAIImageAsset
		if err := tx.Where("generation_id = ? AND deleted_at = 0", generation.ID).Find(&assets).Error; err != nil {
			return err
		}
		for _, asset := range assets {
			updated := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND deleted_at = 0", asset.ID).
				Updates(map[string]any{
					"state": model.ImageAssetStateDeleted, "deleted_at": now.Unix(), "updated_at": now.Unix(),
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrImageGenerationConflict
			}
			if err := EnqueueKKAIOutboxEvent(
				ctx, tx, "image-delete:"+strconv.FormatInt(asset.ID, 10)+":v1",
				ImageAssetDeleteTopic, strconv.FormatInt(asset.ID, 10), now,
				imageAssetDeletePayload{
					AssetID: asset.ID, ObjectKey: asset.ObjectKey, ThumbnailObjectKey: asset.ThumbnailObjectKey,
				},
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func DiscardSubmittingImageGenerationAssets(
	ctx context.Context, db *gorm.DB, generationID int64,
) error {
	return discardImageGenerationAssets(
		ctx, db, generationID, model.ImageGenerationStatusSubmitting,
	)
}

func discardRecoveringImageGenerationAssets(
	ctx context.Context, db *gorm.DB, generationID int64,
) error {
	return discardImageGenerationAssets(
		ctx, db, generationID, model.ImageGenerationStatusRecovering,
	)
}

func discardImageGenerationAssets(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	expectedStatus string,
) error {
	if db == nil || generationID <= 0 {
		return ErrImageGenerationNotFound
	}
	if expectedStatus != model.ImageGenerationStatusSubmitting &&
		expectedStatus != model.ImageGenerationStatusRecovering {
		return ErrInvalidImageStudioSubmission
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation model.KKAIImageGeneration
		if err := lockRowsForUpdate(tx).First(&generation, "id = ?", generationID).Error; err != nil {
			return err
		}
		if generation.Status != expectedStatus {
			return ErrImageGenerationConflict
		}
		var assets []model.KKAIImageAsset
		if err := tx.Where("generation_id = ? AND deleted_at = 0", generationID).Find(&assets).Error; err != nil {
			return err
		}
		now := time.Now()
		for _, asset := range assets {
			updated := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND deleted_at = 0", asset.ID).
				Updates(map[string]any{
					"state": model.ImageAssetStateDeleted, "deleted_at": now.Unix(), "updated_at": now.Unix(),
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrImageGenerationConflict
			}
			if err := EnqueueKKAIOutboxEvent(
				ctx, tx, "image-delete:"+strconv.FormatInt(asset.ID, 10)+":v1",
				ImageAssetDeleteTopic, strconv.FormatInt(asset.ID, 10), now,
				imageAssetDeletePayload{
					AssetID: asset.ID, ObjectKey: asset.ObjectKey, ThumbnailObjectKey: asset.ThumbnailObjectKey,
				},
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildImageGenerationView(
	generation model.KKAIImageGeneration,
	assets []model.KKAIImageAsset,
) (ImageGenerationView, error) {
	parameters := map[string]any{}
	if err := common.UnmarshalJsonStr(generation.Parameters, &parameters); err != nil {
		return ImageGenerationView{}, fmt.Errorf("decode image generation parameters: %w", err)
	}
	assetViews := make([]ImageAssetView, 0, len(assets))
	status := generation.Status
	if status == model.ImageGenerationStatusRecovering {
		status = model.ImageGenerationStatusSubmitting
	}
	deliverable := generation.BillingState == model.ImageGenerationBillingStateSettled &&
		(generation.Status == model.ImageGenerationStatusSucceeded || generation.Status == model.ImageGenerationStatusPartial)
	for _, asset := range assets {
		view := ImageAssetView{
			ID: asset.ID, Position: asset.Position, State: asset.State,
			ThumbnailState: asset.ThumbnailState, MIMEType: asset.MIMEType,
			SizeBytes: asset.SizeBytes, Width: asset.Width, Height: asset.Height,
			FailureReason: asset.FailureReason,
		}
		if deliverable && asset.State == model.ImageAssetStateReady {
			view.ContentURL = imageAssetContentPath(asset.ID, false)
			view.DownloadURL = imageAssetDownloadPath(asset.ID)
			if asset.ThumbnailState == model.ImageThumbnailStateReady && asset.ThumbnailObjectKey != "" {
				view.ThumbnailURL = imageAssetContentPath(asset.ID, true)
			}
		}
		assetViews = append(assetViews, view)
	}
	return ImageGenerationView{
		ID: generation.ID, ModelProfileID: generation.ModelProfileID, SampleID: generation.SampleID,
		SpecificationVersion: generation.SpecificationVersion, Model: generation.Model,
		Prompt: generation.Prompt, Parameters: parameters, RequestID: generation.RequestID,
		Status: status, RequestedCount: generation.RequestedCount,
		SucceededCount: generation.SucceededCount, FinalQuota: generation.FinalQuota,
		FailureStage: generation.FailureStage, ErrorCode: generation.ErrorCode,
		ErrorMessage: generation.ErrorMessage, StartedAt: generation.StartedAt,
		FinishedAt: generation.FinishedAt, CreatedAt: generation.CreatedAt, Assets: assetViews,
	}, nil
}

func FinishImageGeneration(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	status string,
	succeededCount int,
	finalQuota int,
	failureStage string,
	errorCode string,
	errorMessage string,
) error {
	return finishImageGeneration(
		ctx, db, generationID, model.ImageGenerationStatusSubmitting,
		status, succeededCount, finalQuota, failureStage, errorCode, errorMessage,
	)
}

func finishRecoveringImageGeneration(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	status string,
	succeededCount int,
	finalQuota int,
	failureStage string,
	errorCode string,
	errorMessage string,
) error {
	return finishImageGeneration(
		ctx, db, generationID, model.ImageGenerationStatusRecovering,
		status, succeededCount, finalQuota, failureStage, errorCode, errorMessage,
	)
}

func finishImageGeneration(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	expectedStatus string,
	status string,
	succeededCount int,
	finalQuota int,
	failureStage string,
	errorCode string,
	errorMessage string,
) error {
	if db == nil || generationID <= 0 || succeededCount < 0 || finalQuota < 0 || !validImageGenerationTerminalStatus(status) {
		return ErrInvalidImageStudioSubmission
	}
	if expectedStatus != model.ImageGenerationStatusSubmitting &&
		expectedStatus != model.ImageGenerationStatusRecovering {
		return ErrInvalidImageStudioSubmission
	}
	failureStage = truncateImageGenerationField(failureStage, 32)
	errorCode = truncateImageGenerationField(errorCode, 64)
	errorMessage = truncateImageGenerationField(errorMessage, 1024)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation model.KKAIImageGeneration
		if err := lockRowsForUpdate(tx).First(
			&generation, "id = ?", generationID,
		).Error; err != nil {
			return fmt.Errorf("finish image generation: %w", err)
		}
		if generation.Status != expectedStatus {
			return ErrImageGenerationConflict
		}
		switch generation.BillingState {
		case model.ImageGenerationBillingStateSettled:
			if generation.ReservedQuota != finalQuota {
				return ErrImageGenerationConflict
			}
		case model.ImageGenerationBillingStateRefunded:
			if finalQuota != 0 || status == model.ImageGenerationStatusSucceeded ||
				status == model.ImageGenerationStatusPartial {
				return ErrImageGenerationConflict
			}
		default:
			return ErrImageGenerationConflict
		}
		now := time.Now().Unix()
		updated := tx.Model(&model.KKAIImageGeneration{}).
			Where("id = ? AND status = ? AND billing_state = ?", generationID, expectedStatus, generation.BillingState).
			Updates(map[string]any{
				"status": status, "succeeded_count": succeededCount, "final_quota": finalQuota,
				"failure_stage": failureStage, "error_code": errorCode, "error_message": errorMessage,
				"finished_at": now, "updated_at": now,
			})
		if updated.Error != nil {
			return fmt.Errorf("finish image generation: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ErrImageGenerationConflict
		}
		return nil
	})
}

func ReconcileStaleImageGenerations(ctx context.Context, db *gorm.DB, staleBefore time.Time, limit int) (int, error) {
	if db == nil || staleBefore.IsZero() || limit <= 0 || limit > 1000 {
		return 0, ErrInvalidImageStudioSubmission
	}
	var ids []int64
	if err := db.WithContext(ctx).Model(&model.KKAIImageGeneration{}).
		Where(
			"status IN ? AND heartbeat_at < ?",
			[]string{model.ImageGenerationStatusSubmitting, model.ImageGenerationStatusRecovering},
			staleBefore.Unix(),
		).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("list stale image generations: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	reconciled := 0
	for _, id := range ids {
		applied, err := reconcileStaleImageGeneration(ctx, db, id, staleBefore)
		if err != nil {
			return reconciled, fmt.Errorf("reconcile stale image generation %d: %w", id, err)
		}
		if applied {
			reconciled++
		}
	}
	return reconciled, nil
}

func reconcileStaleImageGeneration(
	ctx context.Context, db *gorm.DB, generationID int64, staleBefore time.Time,
) (bool, error) {
	now := time.Now().Unix()
	claimed := db.WithContext(ctx).Model(&model.KKAIImageGeneration{}).Where(
		"id = ? AND status IN ? AND heartbeat_at < ?",
		generationID,
		[]string{model.ImageGenerationStatusSubmitting, model.ImageGenerationStatusRecovering},
		staleBefore.Unix(),
	).Updates(map[string]any{
		"status": model.ImageGenerationStatusRecovering, "heartbeat_at": now, "updated_at": now,
	})
	if claimed.Error != nil {
		return false, claimed.Error
	}
	if claimed.RowsAffected != 1 {
		return false, nil
	}
	var generation model.KKAIImageGeneration
	if err := db.WithContext(ctx).First(&generation, "id = ?", generationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if generation.Status != model.ImageGenerationStatusRecovering {
		return false, ErrImageGenerationConflict
	}
	var readyAssets int64
	if err := db.WithContext(ctx).Model(&model.KKAIImageAsset{}).Where(
		"generation_id = ? AND state = ? AND deleted_at = 0",
		generationID, model.ImageAssetStateReady,
	).Count(&readyAssets).Error; err != nil {
		return false, err
	}
	if readyAssets == int64(generation.RequestedCount) && readyAssets > 0 {
		accounting, err := model.GetImageGenerationAccounting(ctx, db, generationID)
		if err == nil {
			if _, err := model.SettleRecoveringImageGenerationBilling(
				ctx, db, generationID, accounting.TargetQuota,
			); err != nil {
				return false, err
			}
			if err := finishRecoveringImageGeneration(
				ctx, db, generationID, model.ImageGenerationStatusSucceeded,
				int(readyAssets), accounting.TargetQuota, "", "", "",
			); err != nil && !errors.Is(err, ErrImageGenerationConflict) {
				return false, err
			}
			return true, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
	}
	if err := discardRecoveringImageGenerationAssets(ctx, db, generationID); err != nil {
		return false, err
	}
	if _, err := model.RefundRecoveringImageGenerationBilling(ctx, db, generationID); err != nil {
		return false, err
	}
	status := model.ImageGenerationStatusUnknown
	failureStage := "reconcile"
	errorCode := "submission_interrupted"
	errorMessage := "submission outcome is unknown"
	if readyAssets > 0 {
		status = model.ImageGenerationStatusArchiveFailed
		failureStage = "archive"
		errorCode = "partial_archive_rejected"
		errorMessage = "partial image delivery was discarded and refunded"
	}
	if err := finishRecoveringImageGeneration(
		ctx, db, generationID, status, 0, 0, failureStage, errorCode, errorMessage,
	); err != nil && !errors.Is(err, ErrImageGenerationConflict) {
		return false, err
	}
	return true, nil
}

func MaintainImageGenerationHeartbeat(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	interval time.Duration,
) {
	if ctx == nil || db == nil || generationID <= 0 || interval <= 0 {
		return
	}
	touch := func() bool {
		alive, err := model.TouchImageGeneration(ctx, db, generationID)
		if err != nil {
			common.SysError(fmt.Sprintf("touch image generation %d: %s", generationID, err.Error()))
			return true
		}
		return alive
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !touch() {
				return
			}
		}
	}
}

func validImageGenerationTerminalStatus(status string) bool {
	switch status {
	case model.ImageGenerationStatusSucceeded, model.ImageGenerationStatusPartial,
		model.ImageGenerationStatusFailed, model.ImageGenerationStatusArchiveFailed,
		model.ImageGenerationStatusUnknown:
		return true
	default:
		return false
	}
}

func validImageGenerationListStatus(status string) bool {
	return status == model.ImageGenerationStatusSubmitting || validImageGenerationTerminalStatus(status)
}

func truncateImageGenerationField(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
