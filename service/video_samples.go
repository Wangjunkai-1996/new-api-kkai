package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrVideoSampleNotFound       = errors.New("video sample not found")
	ErrInvalidVideoSample        = errors.New("invalid video sample")
	ErrVideoSampleNotPublishable = errors.New("video sample assets are not ready for publishing")
)

type VideoSampleInput struct {
	ModelProfileID    int64          `json:"model_profile_id"`
	Title             string         `json:"title"`
	Prompt            string         `json:"prompt"`
	Mode              string         `json:"mode"`
	Parameters        map[string]any `json:"parameters"`
	ReferenceAssetIDs []int64        `json:"reference_asset_ids"`
	VideoAssetID      int64          `json:"video_asset_id"`
	AspectRatio       float64        `json:"aspect_ratio"`
	Status            string         `json:"status"`
	SortOrder         int            `json:"sort_order"`
}

type VideoSampleView struct {
	ID                   int64                          `json:"id"`
	ModelProfileID       int64                          `json:"model_profile_id"`
	Model                string                         `json:"model"`
	ModelDisplayName     string                         `json:"model_display_name"`
	Title                string                         `json:"title"`
	Prompt               string                         `json:"prompt"`
	Mode                 string                         `json:"mode"`
	ModelVersion         int                            `json:"model_version"`
	Parameters           map[string]any                 `json:"parameters"`
	ReferenceAssetIDs    []int64                        `json:"reference_asset_ids"`
	ReferenceAssets      []VideoSampleReferenceSnapshot `json:"reference_assets"`
	ReferenceContentURLs []string                       `json:"reference_content_urls"`
	VideoAssetID         int64                          `json:"video_asset_id"`
	VideoURL             string                         `json:"video_url"`
	PosterURL            string                         `json:"poster_url"`
	PreviewURL           string                         `json:"preview_url"`
	AspectRatio          float64                        `json:"aspect_ratio"`
	Status               string                         `json:"status"`
	SortOrder            int                            `json:"sort_order"`
	CreatedAt            int64                          `json:"created_at"`
	UpdatedAt            int64                          `json:"updated_at"`
}

type VideoSamplePage struct {
	Items      []VideoSampleView `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type videoSampleCursor struct {
	SortOrder int   `json:"sort_order"`
	ID        int64 `json:"id"`
}

func ListVideoSamples(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
	cursor string,
	limit int,
	includeDrafts bool,
	allowedModels []string,
) (VideoSamplePage, error) {
	if db == nil {
		return VideoSamplePage{}, ErrVideoSampleNotFound
	}
	if limit <= 0 {
		limit = 24
	}
	if limit > 50 {
		limit = 50
	}
	query := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Joins("JOIN kkai_video_model_profiles ON kkai_video_model_profiles.id = kkai_video_samples.model_profile_id")
	if includeDrafts {
		query = query.Select("kkai_video_samples.*")
	} else {
		query = query.Select("kkai_video_samples.*").
			Where("kkai_video_samples.status = ? AND kkai_video_model_profiles.enabled = ?", model.VideoSampleStatusPublished, true)
	}
	if allowedModels != nil {
		if len(allowedModels) == 0 {
			return VideoSamplePage{Items: []VideoSampleView{}}, nil
		}
		query = query.Where("kkai_video_model_profiles.model IN ?", allowedModels)
	}
	if strings.TrimSpace(modelName) != "" {
		query = query.Where("kkai_video_model_profiles.model = ?", strings.TrimSpace(modelName))
	}
	if cursor != "" {
		position, err := decodeVideoSampleCursor(cursor)
		if err != nil {
			return VideoSamplePage{}, err
		}
		query = query.Where(
			"kkai_video_samples.sort_order > ? OR (kkai_video_samples.sort_order = ? AND kkai_video_samples.id < ?)",
			position.SortOrder, position.SortOrder, position.ID,
		)
	}
	var samples []model.KKAIVideoSample
	if err := query.Order("kkai_video_samples.sort_order ASC, kkai_video_samples.id DESC").Limit(limit + 1).Find(&samples).Error; err != nil {
		return VideoSamplePage{}, fmt.Errorf("list video samples: %w", err)
	}
	hasMore := len(samples) > limit
	if hasMore {
		samples = samples[:limit]
	}
	items, err := buildVideoSampleViews(ctx, db, samples)
	if err != nil {
		return VideoSamplePage{}, err
	}
	page := VideoSamplePage{Items: items}
	if hasMore && len(samples) > 0 {
		last := samples[len(samples)-1]
		page.NextCursor, err = encodeVideoSampleCursor(videoSampleCursor{SortOrder: last.SortOrder, ID: last.ID})
	}
	return page, err
}

func GetVideoSample(
	ctx context.Context,
	db *gorm.DB,
	id int64,
	includeDrafts bool,
	allowedModels []string,
) (*VideoSampleView, error) {
	query := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Joins("JOIN kkai_video_model_profiles ON kkai_video_model_profiles.id = kkai_video_samples.model_profile_id").
		Select("kkai_video_samples.*").Where("kkai_video_samples.id = ?", id)
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
	views, err := buildVideoSampleViews(ctx, db, []model.KKAIVideoSample{sample})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func CreateVideoSample(ctx context.Context, db *gorm.DB, adminUserID int, input VideoSampleInput) (*VideoSampleView, error) {
	var created model.KKAIVideoSample
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prepared, assets, err := prepareVideoSampleInput(ctx, tx, adminUserID, input)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		created = model.KKAIVideoSample{
			ModelProfileID: prepared.ModelProfileID, Title: prepared.Title, Prompt: prepared.Prompt,
			Mode: prepared.Mode, ModelVersion: prepared.modelVersion, Parameters: prepared.parametersJSON,
			ReferenceAssetIDs: prepared.referenceAssetIDsJSON, VideoAssetID: prepared.VideoAssetID,
			AspectRatio: prepared.AspectRatio, Status: prepared.Status, SortOrder: prepared.SortOrder,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		assetIDs := append([]int64{prepared.VideoAssetID}, prepared.ReferenceAssetIDs...)
		if err := tx.Model(&model.KKAIVideoAsset{}).Where("id IN ?", assetIDs).
			Updates(map[string]any{"scope": model.VideoAssetScopeCatalog, "updated_at": now}).Error; err != nil {
			return err
		}
		_ = assets
		return enqueueVideoSamplePreparation(ctx, tx, created.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("create video sample: %w", err)
	}
	return GetVideoSample(ctx, db, created.ID, true, nil)
}

func UpdateVideoSample(ctx context.Context, db *gorm.DB, id int64, adminUserID int, input VideoSampleInput) (*VideoSampleView, error) {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.KKAIVideoSample
		if err := tx.First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoSampleNotFound
			}
			return err
		}
		prepared, _, err := prepareVideoSampleInput(ctx, tx, adminUserID, input)
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
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		assetIDs := append([]int64{prepared.VideoAssetID}, prepared.ReferenceAssetIDs...)
		if err := tx.Model(&model.KKAIVideoAsset{}).Where("id IN ?", assetIDs).
			Updates(map[string]any{"scope": model.VideoAssetScopeCatalog, "updated_at": now}).Error; err != nil {
			return err
		}
		return enqueueVideoSamplePreparation(ctx, tx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("update video sample: %w", err)
	}
	return GetVideoSample(ctx, db, id, true, nil)
}

func DeleteVideoSample(ctx context.Context, db *gorm.DB, id int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sample model.KKAIVideoSample
		if err := tx.First(&sample, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoSampleNotFound
			}
			return err
		}
		if sample.Status == model.VideoSampleStatusPublished {
			var profile model.KKAIVideoModelProfile
			if err := tx.First(&profile, "id = ?", sample.ModelProfileID).Error; err != nil {
				return err
			}
			if profile.Enabled {
				var remaining int64
				if err := tx.Model(&model.KKAIVideoSample{}).Where(
					"model_profile_id = ? AND status = ? AND id <> ?", sample.ModelProfileID, model.VideoSampleStatusPublished, id,
				).Count(&remaining).Error; err != nil {
					return err
				}
				if remaining == 0 {
					return ErrVideoModelNeedsSample
				}
			}
		}
		return tx.Delete(&sample).Error
	})
}

type preparedVideoSampleInput struct {
	VideoSampleInput
	modelVersion          int
	parametersJSON        string
	referenceAssetIDsJSON string
}

func prepareVideoSampleInput(ctx context.Context, tx *gorm.DB, adminUserID int, input VideoSampleInput) (preparedVideoSampleInput, []model.KKAIVideoAsset, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.ModelProfileID <= 0 || input.Title == "" || len(input.Title) > 191 || input.Prompt == "" || len(input.Prompt) > 8000 ||
		input.VideoAssetID <= 0 || input.SortOrder < -100000 || input.SortOrder > 100000 {
		return preparedVideoSampleInput{}, nil, ErrInvalidVideoSample
	}
	if input.Status == "" {
		input.Status = model.VideoSampleStatusDraft
	}
	if input.Status != model.VideoSampleStatusDraft && input.Status != model.VideoSampleStatusPublished {
		return preparedVideoSampleInput{}, nil, ErrInvalidVideoSample
	}
	var profile model.KKAIVideoModelProfile
	if err := tx.WithContext(ctx).First(&profile, "id = ?", input.ModelProfileID).Error; err != nil {
		return preparedVideoSampleInput{}, nil, ErrVideoModelProfileNotFound
	}
	specification, _, err := decodeVideoModelProfile(profile)
	if err != nil {
		return preparedVideoSampleInput{}, nil, err
	}
	parameters, err := ValidateVideoParameters(specification, input.Mode, input.Parameters, true)
	if err != nil {
		return preparedVideoSampleInput{}, nil, err
	}
	expectedReferences := expectedVideoReferenceInputs(specification, input.Mode)
	if len(input.ReferenceAssetIDs) != len(expectedReferences) {
		return preparedVideoSampleInput{}, nil, ErrInvalidVideoSample
	}
	assetIDs := append([]int64{input.VideoAssetID}, input.ReferenceAssetIDs...)
	var assets []model.KKAIVideoAsset
	if err := tx.WithContext(ctx).Where("id IN ?", assetIDs).Find(&assets).Error; err != nil || len(assets) != len(assetIDs) {
		return preparedVideoSampleInput{}, nil, ErrVideoSampleNotPublishable
	}
	assetsByID := make(map[int64]model.KKAIVideoAsset, len(assets))
	for _, asset := range assets {
		if asset.DeletedAt != 0 || (asset.Scope != model.VideoAssetScopeCatalog && asset.OwnerUserID != adminUserID) {
			return preparedVideoSampleInput{}, nil, ErrVideoSampleNotPublishable
		}
		assetsByID[asset.ID] = asset
	}
	videoAsset := assetsByID[input.VideoAssetID]
	if videoAsset.State != model.VideoAssetStateReady || !strings.HasPrefix(videoAsset.MIMEType, "video/") {
		return preparedVideoSampleInput{}, nil, ErrVideoSampleNotPublishable
	}
	for index, referenceID := range input.ReferenceAssetIDs {
		reference := assetsByID[referenceID]
		expectsVideo := expectedReferences[index].Role == model.VideoTaskAssetRoleReferenceVideo
		validMIME := strings.HasPrefix(reference.MIMEType, "image/")
		if expectsVideo {
			validMIME = strings.HasPrefix(reference.MIMEType, "video/")
		}
		if reference.State != model.VideoAssetStateReady || !validMIME {
			return preparedVideoSampleInput{}, nil, ErrVideoSampleNotPublishable
		}
	}
	if input.Status == model.VideoSampleStatusPublished && (videoAsset.PosterObjectKey == "" || videoAsset.PreviewObjectKey == "") {
		return preparedVideoSampleInput{}, nil, ErrVideoSampleNotPublishable
	}
	if input.AspectRatio <= 0 && videoAsset.Width > 0 && videoAsset.Height > 0 {
		input.AspectRatio = float64(videoAsset.Width) / float64(videoAsset.Height)
	}
	if input.AspectRatio <= 0 || input.AspectRatio > 10 {
		return preparedVideoSampleInput{}, nil, ErrInvalidVideoSample
	}
	parametersJSON, err := common.Marshal(parameters)
	if err != nil {
		return preparedVideoSampleInput{}, nil, err
	}
	referenceSnapshots, err := newVideoSampleReferenceSnapshots(input.ReferenceAssetIDs, expectedReferences)
	if err != nil {
		return preparedVideoSampleInput{}, nil, err
	}
	referenceJSON, err := common.Marshal(referenceSnapshots)
	if err != nil {
		return preparedVideoSampleInput{}, nil, err
	}
	input.Parameters = parameters
	return preparedVideoSampleInput{
		VideoSampleInput: input, modelVersion: profile.SpecificationVersion,
		parametersJSON: string(parametersJSON), referenceAssetIDsJSON: string(referenceJSON),
	}, assets, nil
}

func buildVideoSampleViews(ctx context.Context, db *gorm.DB, samples []model.KKAIVideoSample) ([]VideoSampleView, error) {
	if len(samples) == 0 {
		return []VideoSampleView{}, nil
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
	views := make([]VideoSampleView, 0, len(samples))
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
		specification, _, err := decodeVideoModelProfile(profile)
		if err != nil {
			return nil, err
		}
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshots(
			sample.ReferenceAssetIDs, expectedVideoReferenceInputs(specification, sample.Mode),
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
		views = append(views, VideoSampleView{
			ID: sample.ID, ModelProfileID: sample.ModelProfileID, Model: profile.Model, ModelDisplayName: profile.DisplayName,
			Title: sample.Title, Prompt: sample.Prompt, Mode: sample.Mode, ModelVersion: sample.ModelVersion,
			Parameters: parameters, ReferenceAssetIDs: references, ReferenceAssets: referenceSnapshots,
			ReferenceContentURLs: referenceURLs,
			VideoAssetID:         sample.VideoAssetID, VideoURL: assetView.ContentURL,
			PosterURL: assetView.PosterURL, PreviewURL: assetView.PreviewURL,
			AspectRatio: sample.AspectRatio, Status: sample.Status, SortOrder: sample.SortOrder,
			CreatedAt: sample.CreatedAt, UpdatedAt: sample.UpdatedAt,
		})
	}
	return views, nil
}

func videoAssetContentPath(assetID int64, variant string) string {
	path := "/api/video-studio/assets/" + strconv.FormatInt(assetID, 10) + "/content"
	if variant != "" {
		path += "?variant=" + variant
	}
	return path
}

func enqueueVideoSamplePreparation(ctx context.Context, tx *gorm.DB, sampleID int64) error {
	if sampleID <= 0 {
		return ErrInvalidVideoSample
	}
	return EnqueueVideoOutboxEvent(ctx, tx,
		fmt.Sprintf("video:sample:%d:prepare:v1:%s", sampleID, uuid.NewString()),
		VideoOutboxTopicSamplePrepare, strconv.FormatInt(sampleID, 10),
		VideoSamplePrepareEventPayload{SampleID: sampleID},
	)
}

func expectedVideoReferenceRoles(specification VideoModelSpec, mode string) []string {
	inputs := expectedVideoReferenceInputs(specification, mode)
	roles := make([]string, 0, len(inputs))
	for _, input := range inputs {
		roles = append(roles, input.Role)
	}
	return roles
}

func expectedVideoReferenceInputs(specification VideoModelSpec, mode string) []VideoReferenceInputSpec {
	if mode == VideoModeTextToVideo {
		return nil
	}
	if mode == VideoModeImageToVideo {
		inputs := make([]VideoReferenceInputSpec, 0, 1)
		for _, input := range specification.ReferenceInputs {
			if input.Required && (input.Role == model.VideoTaskAssetRoleReference || input.Role == model.VideoTaskAssetRoleReferenceVideo) {
				inputs = append(inputs, input)
			}
		}
		return inputs
	}
	roles := []string{model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame}
	inputs := make([]VideoReferenceInputSpec, 0, len(roles))
	for _, role := range roles {
		for _, input := range specification.ReferenceInputs {
			if input.Role == role && input.Required {
				inputs = append(inputs, input)
				break
			}
		}
	}
	return inputs
}

func encodeVideoSampleCursor(cursor videoSampleCursor) (string, error) {
	encoded, err := common.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeVideoSampleCursor(value string) (videoSampleCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return videoSampleCursor{}, ErrInvalidVideoSample
	}
	var cursor videoSampleCursor
	if err := common.Unmarshal(decoded, &cursor); err != nil || cursor.ID <= 0 {
		return videoSampleCursor{}, ErrInvalidVideoSample
	}
	return cursor, nil
}
