package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func ListVideoSamplesAPI3(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
	category string,
	cursor string,
	limit int,
	includeDrafts bool,
	allowedModels []string,
) (VideoSamplePageAPI3, error) {
	if db == nil {
		return VideoSamplePageAPI3{}, ErrVideoSampleNotFound
	}
	if limit <= 0 {
		limit = 24
	}
	if limit > 50 {
		limit = 50
	}
	normalizedCategory, err := normalizeVideoSampleCategoryFilter(category)
	if err != nil {
		return VideoSamplePageAPI3{}, err
	}
	query := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Joins("JOIN kkai_video_model_profiles ON kkai_video_model_profiles.id = kkai_video_samples.model_profile_id").
		Select(videoSampleSelectColumns()).Where("kkai_video_samples.reference_asset_ids NOT LIKE ?", `{"reference_assets":%`)
	if !includeDrafts {
		query = query.Where(
			"kkai_video_samples.status = ? AND kkai_video_model_profiles.enabled = ?",
			model.VideoSampleStatusPublished, true,
		)
	}
	if allowedModels != nil {
		if len(allowedModels) == 0 {
			return VideoSamplePageAPI3{Items: []VideoSampleViewAPI3{}}, nil
		}
		query = query.Where("kkai_video_model_profiles.model IN ?", allowedModels)
	}
	if strings.TrimSpace(modelName) != "" {
		query = query.Where("kkai_video_model_profiles.model = ?", strings.TrimSpace(modelName))
	}
	query, err = applyVideoSampleCategoryFilter(query, normalizedCategory)
	if err != nil {
		return VideoSamplePageAPI3{}, err
	}
	if cursor != "" {
		position, err := decodeVideoSampleCursor(cursor)
		if err != nil {
			return VideoSamplePageAPI3{}, err
		}
		query = query.Where(
			"kkai_video_samples.sort_order > ? OR (kkai_video_samples.sort_order = ? AND kkai_video_samples.id < ?)",
			position.SortOrder, position.SortOrder, position.ID,
		)
	}
	var samples []model.KKAIVideoSample
	if err := query.Order("kkai_video_samples.sort_order ASC, kkai_video_samples.id DESC").Limit(limit + 1).Find(&samples).Error; err != nil {
		return VideoSamplePageAPI3{}, fmt.Errorf("list video samples: %w", err)
	}
	hasMore := len(samples) > limit
	if hasMore {
		samples = samples[:limit]
	}
	items, err := buildVideoSampleViewsAPI3(ctx, db, samples)
	if err != nil {
		return VideoSamplePageAPI3{}, err
	}
	page := VideoSamplePageAPI3{Items: items}
	if hasMore && len(samples) > 0 {
		last := samples[len(samples)-1]
		page.NextCursor, err = encodeVideoSampleCursor(videoSampleCursor{SortOrder: last.SortOrder, ID: last.ID})
	}
	return page, err
}

func GetVideoSampleAPI3(
	ctx context.Context,
	db *gorm.DB,
	id int64,
	includeDrafts bool,
	allowedModels []string,
) (*VideoSampleViewAPI3, error) {
	query := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Joins("JOIN kkai_video_model_profiles ON kkai_video_model_profiles.id = kkai_video_samples.model_profile_id").
		Select(videoSampleSelectColumns()).Where("kkai_video_samples.reference_asset_ids NOT LIKE ?", `{"reference_assets":%`).Where("kkai_video_samples.id = ?", id)
	if !includeDrafts {
		query = query.Where("kkai_video_samples.status = ? AND kkai_video_model_profiles.enabled = ?", model.VideoSampleStatusPublished, true)
	}
	if allowedModels != nil {
		if len(allowedModels) == 0 {
			return nil, ErrVideoSampleNotFound
		}
		query = query.Where("kkai_video_model_profiles.model IN ?", allowedModels)
	}
	var sample model.KKAIVideoSample
	if err := query.First(&sample).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoSampleNotFound
		}
		return nil, fmt.Errorf("get video sample: %w", err)
	}
	views, err := buildVideoSampleViewsAPI3(ctx, db, []model.KKAIVideoSample{sample})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func CreateVideoSampleAPI3(ctx context.Context, db *gorm.DB, adminUserID int, input VideoSampleInputAPI3) (*VideoSampleViewAPI3, error) {
	var created model.KKAIVideoSample
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prepared, _, err := prepareVideoSampleInputAPI3(ctx, tx, adminUserID, input, nil, nil)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		created = model.KKAIVideoSample{
			ModelProfileID: prepared.ModelProfileID, Title: prepared.Title, Prompt: prepared.Prompt,
			Mode: prepared.Mode, ModelVersion: prepared.modelVersion, Parameters: prepared.parametersJSON,
			ReferenceAssetIDs: prepared.referenceAssetIDsJSON, VideoAssetID: prepared.VideoAssetID,
			AspectRatio: prepared.AspectRatio, Category: prepared.Category, Status: prepared.Status, SortOrder: prepared.SortOrder,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := createVideoSampleRecord(tx, &created); err != nil {
			return err
		}
		assetIDs := append([]int64{prepared.VideoAssetID}, prepared.ReferenceAssetIDs...)
		if err := tx.Model(&model.KKAIVideoAsset{}).Where("id IN ?", assetIDs).
			Updates(map[string]any{"scope": model.VideoAssetScopeCatalog, "updated_at": now}).Error; err != nil {
			return err
		}
		return enqueueVideoSamplePreparation(ctx, tx, created.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("create video sample: %w", err)
	}
	return GetVideoSampleAPI3(ctx, db, created.ID, true, nil)
}

func UpdateVideoSampleAPI3(ctx context.Context, db *gorm.DB, id int64, adminUserID int, input VideoSampleInputAPI3) (*VideoSampleViewAPI3, error) {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.KKAIVideoSample
		if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).Where("reference_asset_ids NOT LIKE ?", `{"reference_assets":%`).First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoSampleNotFound
			}
			return err
		}
		previousAssetIDs, err := videoSampleAssetIDs(existing)
		if err != nil {
			return err
		}
		prepared, _, err := prepareVideoSampleInputAPI3(
			ctx, tx, adminUserID, input, previousAssetIDs, []int64{existing.ModelProfileID},
		)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		updates := map[string]any{
			"model_profile_id": prepared.ModelProfileID, "title": prepared.Title, "prompt": prepared.Prompt,
			"mode": prepared.Mode, "model_version": prepared.modelVersion, "parameters": prepared.parametersJSON,
			"reference_asset_ids": prepared.referenceAssetIDsJSON, "video_asset_id": prepared.VideoAssetID,
			"aspect_ratio": prepared.AspectRatio, "status": prepared.Status, "sort_order": prepared.SortOrder, "updated_at": now,
		}
		addVideoSampleCategoryUpdate(updates, prepared.Category)
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		assetIDs := append([]int64{prepared.VideoAssetID}, prepared.ReferenceAssetIDs...)
		if err := tx.Model(&model.KKAIVideoAsset{}).Where("id IN ?", assetIDs).
			Updates(map[string]any{"scope": model.VideoAssetScopeCatalog, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := queueUnusedVideoCatalogAssetsForDeletion(ctx, tx, previousAssetIDs, assetIDs); err != nil {
			return err
		}
		return enqueueVideoSamplePreparation(ctx, tx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("update video sample: %w", err)
	}
	return GetVideoSampleAPI3(ctx, db, id, true, nil)
}

func DeleteVideoSampleAPI3(ctx context.Context, db *gorm.DB, id int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sample model.KKAIVideoSample
		if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&sample, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoSampleNotFound
			}
			return err
		}
		assetIDs, err := videoSampleAssetIDs(sample)
		if err != nil {
			return err
		}
		if _, err := lockVideoAssetRowsForUpdate(ctx, tx, assetIDs); err != nil {
			return err
		}
		if err := tx.Delete(&sample).Error; err != nil {
			return err
		}
		return queueUnusedVideoCatalogAssetsForDeletion(ctx, tx, assetIDs, nil)
	})
}

type preparedVideoSampleInputAPI3 struct {
	VideoSampleInputAPI3
	modelVersion          int
	parametersJSON        string
	referenceAssetIDsJSON string
}

func prepareVideoSampleInputAPI3(
	ctx context.Context,
	tx *gorm.DB,
	adminUserID int,
	input VideoSampleInputAPI3,
	additionalAssetIDs []int64,
	additionalProfileIDs []int64,
) (preparedVideoSampleInputAPI3, []model.KKAIVideoAsset, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.ModelProfileID <= 0 || input.Title == "" || len(input.Title) > 191 || input.Prompt == "" || len(input.Prompt) > 8000 ||
		input.VideoAssetID <= 0 || input.SortOrder < -100000 || input.SortOrder > 100000 {
		return preparedVideoSampleInputAPI3{}, nil, ErrInvalidVideoSample
	}
	if input.Status == "" {
		input.Status = model.VideoSampleStatusDraft
	}
	if input.Status != model.VideoSampleStatusDraft && input.Status != model.VideoSampleStatusPublished {
		return preparedVideoSampleInputAPI3{}, nil, ErrInvalidVideoSample
	}
	category, err := normalizeVideoSampleCategory(input.Category)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	input.Category = category
	assetIDs := append([]int64{input.VideoAssetID}, input.ReferenceAssetIDs...)
	lockAssetIDs := append([]int64{}, assetIDs...)
	lockAssetIDs = append(lockAssetIDs, additionalAssetIDs...)
	assets, err := lockVideoAssetRowsForUpdate(ctx, tx, lockAssetIDs)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
	}
	assetsByID := make(map[int64]model.KKAIVideoAsset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}
	seenInputAssetIDs := make(map[int64]struct{}, len(assetIDs))
	for _, assetID := range assetIDs {
		if _, duplicate := seenInputAssetIDs[assetID]; duplicate {
			return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
		}
		seenInputAssetIDs[assetID] = struct{}{}
		asset, exists := assetsByID[assetID]
		if !exists || asset.DeletedAt != 0 ||
			(asset.Scope != model.VideoAssetScopeCatalog && asset.OwnerUserID != adminUserID) {
			return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
		}
	}
	profileIDs := append([]int64{input.ModelProfileID}, additionalProfileIDs...)
	profiles, err := lockVideoModelProfileRowsForUpdate(ctx, tx, profileIDs)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	profilesByID := make(map[int64]model.KKAIVideoModelProfile, len(profiles))
	for _, profile := range profiles {
		profilesByID[profile.ID] = profile
	}
	for _, profileID := range profileIDs {
		if _, exists := profilesByID[profileID]; !exists {
			return preparedVideoSampleInputAPI3{}, nil, ErrVideoModelProfileNotFound
		}
	}
	profile, exists := profilesByID[input.ModelProfileID]
	if !exists {
		return preparedVideoSampleInputAPI3{}, nil, ErrVideoModelProfileNotFound
	}
	specification, defaults, err := decodeVideoModelProfileAPI3(profile)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	modeDefaults := filterVideoParametersForMode(specification, input.Mode, defaults)
	mergedParameters := make(map[string]any, len(modeDefaults)+len(input.Parameters))
	for key, value := range modeDefaults {
		mergedParameters[key] = value
	}
	for key, value := range input.Parameters {
		mergedParameters[key] = value
	}
	parameters, err := ValidateVideoParameters(specification, input.Mode, mergedParameters, true)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	expectedReferences := expectedVideoReferenceInputsAPI3(specification, input.Mode)
	if len(input.ReferenceAssetIDs) != len(expectedReferences) {
		return preparedVideoSampleInputAPI3{}, nil, ErrInvalidVideoSample
	}
	videoAsset := assetsByID[input.VideoAssetID]
	if videoAsset.State != model.VideoAssetStateReady || !strings.HasPrefix(videoAsset.MIMEType, "video/") {
		return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
	}
	for index, referenceID := range input.ReferenceAssetIDs {
		reference := assetsByID[referenceID]
		expectsVideo := expectedReferences[index].Role == model.VideoTaskAssetRoleReferenceVideo
		validMIME := strings.HasPrefix(reference.MIMEType, "image/")
		if expectsVideo {
			validMIME = strings.HasPrefix(reference.MIMEType, "video/")
		}
		if reference.State != model.VideoAssetStateReady || !validMIME {
			return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
		}
	}
	if input.Status == model.VideoSampleStatusPublished && (videoAsset.PosterObjectKey == "" || videoAsset.PreviewObjectKey == "") {
		return preparedVideoSampleInputAPI3{}, nil, ErrVideoSampleNotPublishable
	}
	if input.AspectRatio <= 0 && videoAsset.Width > 0 && videoAsset.Height > 0 {
		input.AspectRatio = float64(videoAsset.Width) / float64(videoAsset.Height)
	}
	if input.AspectRatio <= 0 || input.AspectRatio > 10 {
		return preparedVideoSampleInputAPI3{}, nil, ErrInvalidVideoSample
	}
	parametersJSON, err := common.Marshal(parameters)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	referenceSnapshots, err := newVideoSampleReferenceSnapshotsAPI3(input.ReferenceAssetIDs, expectedReferences)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	referenceJSON, err := common.Marshal(referenceSnapshots)
	if err != nil {
		return preparedVideoSampleInputAPI3{}, nil, err
	}
	input.Parameters = parameters
	return preparedVideoSampleInputAPI3{
		VideoSampleInputAPI3: input, modelVersion: specification.Version,
		parametersJSON: string(parametersJSON), referenceAssetIDsJSON: string(referenceJSON),
	}, assets, nil
}

func buildVideoSampleViewsAPI3(ctx context.Context, db *gorm.DB, samples []model.KKAIVideoSample) ([]VideoSampleViewAPI3, error) {
	if len(samples) == 0 {
		return []VideoSampleViewAPI3{}, nil
	}
	profileIDs := make([]int64, 0, len(samples))
	assetIDs := make([]int64, 0, len(samples)*3)
	for _, sample := range samples {
		profileIDs = append(profileIDs, sample.ModelProfileID)
		assetIDs = append(assetIDs, sample.VideoAssetID)
		references, err := decodeVideoSampleReferenceAssetIDs(sample.ReferenceAssetIDs)
		if err != nil {
			return nil, fmt.Errorf("decode video sample %d references: %w", sample.ID, err)
		}
		assetIDs = append(assetIDs, references...)
	}
	var profiles []model.KKAIVideoModelProfile
	if err := db.WithContext(ctx).Where("id IN ?", profileIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	var assets []model.KKAIVideoAsset
	if err := db.WithContext(ctx).Where("id IN ?", assetIDs).Find(&assets).Error; err != nil {
		return nil, err
	}
	profilesByID := make(map[int64]model.KKAIVideoModelProfile, len(profiles))
	for _, profile := range profiles {
		profilesByID[profile.ID] = profile
	}
	assetsByID := make(map[int64]model.KKAIVideoAsset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}
	views := make([]VideoSampleViewAPI3, 0, len(samples))
	for _, sample := range samples {
		parameters := map[string]any{}
		var references []int64
		if err := common.UnmarshalJsonStr(sample.Parameters, &parameters); err != nil {
			return nil, err
		}
		profile, hasProfile := profilesByID[sample.ModelProfileID]
		if !hasProfile {
			return nil, fmt.Errorf("video sample %d references missing profile %d", sample.ID, sample.ModelProfileID)
		}
		specification, _, err := decodeVideoModelProfileAPI3(profile)
		if err != nil {
			return nil, err
		}
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshotsAPI3(
			sample.ReferenceAssetIDs, expectedVideoReferenceInputsAPI3(specification, sample.Mode),
		)
		if err != nil {
			return nil, fmt.Errorf("decode video sample %d reference snapshot: %w", sample.ID, err)
		}
		references = make([]int64, 0, len(referenceSnapshots))
		for _, snapshot := range referenceSnapshots {
			references = append(references, snapshot.AssetID)
		}
		referenceURLs := make([]string, 0, len(references))
		for _, assetID := range references {
			referenceURLs = append(referenceURLs, videoAssetContentPath(assetID, ""))
		}
		videoAsset, hasVideoAsset := assetsByID[sample.VideoAssetID]
		if !hasVideoAsset {
			return nil, fmt.Errorf("video sample %d references missing asset %d", sample.ID, sample.VideoAssetID)
		}
		assetView := videoAssetView(videoAsset)
		category, err := videoSampleCategoryForView(sample.Category)
		if err != nil {
			return nil, fmt.Errorf("video sample %d has invalid category: %w", sample.ID, err)
		}
		views = append(views, VideoSampleViewAPI3{
			ID: sample.ID, ModelProfileID: sample.ModelProfileID, Model: profile.Model, ModelDisplayName: profile.DisplayName,
			Title: sample.Title, Prompt: sample.Prompt, Mode: sample.Mode, ModelVersion: sample.ModelVersion,
			Parameters: parameters, ReferenceAssetIDs: references, ReferenceAssets: referenceSnapshots,
			ReferenceContentURLs: referenceURLs,
			VideoAssetID:         sample.VideoAssetID, VideoURL: assetView.ContentURL,
			PosterURL: assetView.PosterURL, PreviewURL: assetView.PreviewURL,
			AspectRatio: sample.AspectRatio, Category: category,
			Status: sample.Status, SortOrder: sample.SortOrder,
			CreatedAt: sample.CreatedAt, UpdatedAt: sample.UpdatedAt,
		})
	}
	return views, nil
}
