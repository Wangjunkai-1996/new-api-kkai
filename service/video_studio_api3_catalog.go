package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type VideoSampleInputAPI3 struct {
	ModelProfileID    int64          `json:"model_profile_id"`
	Title             string         `json:"title"`
	Prompt            string         `json:"prompt"`
	Mode              string         `json:"mode"`
	Parameters        map[string]any `json:"parameters"`
	ReferenceAssetIDs []int64        `json:"reference_asset_ids"`
	VideoAssetID      int64          `json:"video_asset_id"`
	AspectRatio       float64        `json:"aspect_ratio"`
	Category          string         `json:"category"`
	Status            string         `json:"status"`
	SortOrder         int            `json:"sort_order"`
}

type VideoSampleViewAPI3 struct {
	ID                   int64                              `json:"id"`
	ModelProfileID       int64                              `json:"model_profile_id"`
	Model                string                             `json:"model"`
	ModelDisplayName     string                             `json:"model_display_name"`
	Title                string                             `json:"title"`
	Prompt               string                             `json:"prompt"`
	Mode                 string                             `json:"mode"`
	ModelVersion         int                                `json:"model_version"`
	Parameters           map[string]any                     `json:"parameters"`
	ReferenceAssetIDs    []int64                            `json:"reference_asset_ids"`
	ReferenceAssets      []VideoSampleReferenceSnapshotAPI3 `json:"reference_assets"`
	ReferenceContentURLs []string                           `json:"reference_content_urls"`
	VideoAssetID         int64                              `json:"video_asset_id"`
	VideoURL             string                             `json:"video_url"`
	PosterURL            string                             `json:"poster_url"`
	PreviewURL           string                             `json:"preview_url"`
	AspectRatio          float64                            `json:"aspect_ratio"`
	Category             string                             `json:"category"`
	Status               string                             `json:"status"`
	SortOrder            int                                `json:"sort_order"`
	CreatedAt            int64                              `json:"created_at"`
	UpdatedAt            int64                              `json:"updated_at"`
}

type VideoSamplePageAPI3 struct {
	Items      []VideoSampleViewAPI3 `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func ListVideoModelProfilesAPI3(ctx context.Context, db *gorm.DB, includeDisabled bool) ([]VideoModelProfileView, error) {
	if db == nil {
		return nil, ErrVideoModelProfileNotFound
	}
	query := db.WithContext(ctx).Order("sort_order ASC, id ASC")
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	var profiles []model.KKAIVideoModelProfile
	if err := query.Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("list video model profiles: %w", err)
	}
	views := make([]VideoModelProfileView, 0, len(profiles))
	for _, profile := range profiles {
		view, err := videoModelProfileViewAPI3(profile)
		if errors.Is(err, ErrVideoModelProfileNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if err := hydrateVideoModelPublishedSampleState(ctx, db, views); err != nil {
		return nil, err
	}
	return views, nil
}

func ListEffectiveVideoModelProfilesAPI3(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	tokenID int,
	clientIP string,
) ([]VideoModelProfileView, error) {
	if db == nil || userID <= 0 || tokenID <= 0 {
		return nil, ErrVideoStudioTokenInvalid
	}
	models, err := effectiveVideoStudioModelsForToken(ctx, db, userID, tokenID, clientIP)
	if err != nil || len(models) == 0 {
		return []VideoModelProfileView{}, err
	}

	var profiles []model.KKAIVideoModelProfile
	if err := db.WithContext(ctx).
		Where("model IN ? AND enabled = ?", models, true).
		Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("list effective video model profiles: %w", err)
	}
	profilesByModel := make(map[string]model.KKAIVideoModelProfile, len(profiles))
	for _, profile := range profiles {
		profilesByModel[profile.Model] = profile
	}
	views := make([]VideoModelProfileView, 0, len(models))
	for _, modelName := range models {
		profile, exists := profilesByModel[modelName]
		if !exists {
			continue
		}
		view, err := videoModelProfileViewAPI3(profile)
		if errors.Is(err, ErrVideoModelProfileNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(left, right int) bool {
		if views[left].SortOrder != views[right].SortOrder {
			return views[left].SortOrder < views[right].SortOrder
		}
		return views[left].Model < views[right].Model
	})
	return views, nil
}

func GetVideoModelProfileByIDAPI3(ctx context.Context, db *gorm.DB, id int64) (*VideoModelProfileView, error) {
	var profile model.KKAIVideoModelProfile
	if db == nil || id <= 0 {
		return nil, ErrVideoModelProfileNotFound
	}
	if err := db.WithContext(ctx).First(&profile, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoModelProfileNotFound
		}
		return nil, fmt.Errorf("get video model profile: %w", err)
	}
	return adminVideoModelProfileViewAPI3(ctx, db, profile)
}

func GetEnabledVideoModelProfileByModelAPI3(ctx context.Context, db *gorm.DB, modelName string) (*model.KKAIVideoModelProfile, VideoModelSpec, map[string]any, error) {
	var profile model.KKAIVideoModelProfile
	if db == nil || strings.TrimSpace(modelName) == "" {
		return nil, VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
	}
	if err := db.WithContext(ctx).Where("model = ? AND enabled = ?", strings.TrimSpace(modelName), true).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
		}
		return nil, VideoModelSpec{}, nil, fmt.Errorf("get enabled video model profile: %w", err)
	}
	specification, defaults, err := decodeVideoModelProfileAPI3(profile)
	if err != nil {
		return nil, VideoModelSpec{}, nil, err
	}
	profile.SpecificationVersion = specification.Version
	return &profile, specification, defaults, nil
}

func CreateVideoModelProfileAPI3(ctx context.Context, db *gorm.DB, input VideoModelProfileInput) (*VideoModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeVideoModelProfileInputAPI3(input)
	if err != nil {
		return nil, err
	}
	available, err := videoStudioModelHasEnabledAbility(ctx, db, VideoStudioTokenGroup, normalized.Model)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, ErrVideoModelAbilityUnavailable
	}
	var duplicateCount int64
	if err := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
		Where("model = ?", normalized.Model).Count(&duplicateCount).Error; err != nil {
		return nil, err
	}
	if duplicateCount > 0 {
		return nil, ErrVideoModelProfileDuplicate
	}
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: normalized.Model, DisplayName: normalized.DisplayName, Description: normalized.Description,
		ProviderLabel: normalized.ProviderLabel, SpecificationVersion: normalized.Specification.Version,
		Specification: specificationJSON, DefaultParameters: defaultsJSON, Enabled: normalized.Enabled,
		SortOrder: normalized.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&profile).Error; err != nil {
		if countErr := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
			Where("model = ?", normalized.Model).Count(&duplicateCount).Error; countErr == nil && duplicateCount > 0 {
			return nil, ErrVideoModelProfileDuplicate
		}
		return nil, fmt.Errorf("create video model profile: %w", err)
	}
	return adminVideoModelProfileViewAPI3(ctx, db, profile)
}

func UpdateVideoModelProfileAPI3(ctx context.Context, db *gorm.DB, id int64, input VideoModelProfileInput) (*VideoModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeVideoModelProfileInputAPI3(input)
	if err != nil {
		return nil, err
	}
	var updated model.KKAIVideoModelProfile
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		profiles, err := lockVideoModelProfileRowsForUpdate(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		if len(profiles) != 1 {
			return ErrVideoModelProfileNotFound
		}
		updated = profiles[0]
		if normalized.Model != updated.Model {
			return ErrVideoModelProfileModelImmutable
		}
		if normalized.Enabled {
			available, err := videoStudioModelHasEnabledAbility(ctx, tx, VideoStudioTokenGroup, normalized.Model)
			if err != nil {
				return err
			}
			if !available {
				return ErrVideoModelAbilityUnavailable
			}
		}
		previousSpecification, _, err := decodeVideoModelProfileAPI3(updated)
		if err != nil {
			return err
		}
		previousJSON, err := common.Marshal(previousSpecification)
		if err != nil {
			return err
		}
		if string(previousJSON) != specificationJSON && normalized.Specification.Version <= previousSpecification.Version {
			return fmt.Errorf("%w: specification changes require a higher version", ErrInvalidVideoModelSpec)
		}
		if err := validatePublishedSamplesAgainstSpecAPI3(tx, id, previousSpecification, normalized.Specification); err != nil {
			return err
		}
		updates := map[string]any{
			"model": normalized.Model, "display_name": normalized.DisplayName, "description": normalized.Description,
			"provider_label": normalized.ProviderLabel, "specification_version": normalized.Specification.Version,
			"specification": specificationJSON, "default_parameters": defaultsJSON, "enabled": normalized.Enabled,
			"sort_order": normalized.SortOrder, "updated_at": time.Now().Unix(),
		}
		if err := updateVideoModelAPI3Snapshot(updated, updates, normalized.Specification, normalized.DefaultParameters); err != nil {
			return err
		}
		if err := tx.Model(&updated).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("update video model profile: %w", err)
	}
	return adminVideoModelProfileViewAPI3(ctx, db, updated)
}

func DeleteVideoModelProfileAPI3(ctx context.Context, db *gorm.DB, id int64) error {
	if db == nil || id <= 0 {
		return ErrVideoModelProfileNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile model.KKAIVideoModelProfile
		if err := tx.First(&profile, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoModelProfileNotFound
			}
			return err
		}
		var references int64
		if err := tx.Model(&model.KKAIVideoSample{}).Where("model_profile_id = ?", id).Count(&references).Error; err != nil {
			return err
		}
		if references == 0 {
			if err := tx.Model(&model.KKAIVideoGeneration{}).Where("model_profile_id = ?", id).Count(&references).Error; err != nil {
				return err
			}
		}
		if references > 0 {
			return ErrVideoModelProfileInUse
		}
		return tx.Delete(&profile).Error
	})
}

func normalizeVideoModelProfileInputAPI3(input VideoModelProfileInput) (VideoModelProfileInput, string, string, error) {
	input.Model = strings.TrimSpace(input.Model)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Description = strings.TrimSpace(input.Description)
	input.ProviderLabel = strings.TrimSpace(input.ProviderLabel)
	if !videoModelNamePattern.MatchString(input.Model) || input.DisplayName == "" || len(input.DisplayName) > 191 ||
		len(input.Description) > 4000 || len(input.ProviderLabel) > 128 || input.SortOrder < -100000 || input.SortOrder > 100000 {
		return VideoModelProfileInput{}, "", "", ErrInvalidVideoModelSpec
	}
	if input.DefaultParameters == nil {
		input.DefaultParameters = map[string]any{}
	}
	if err := ValidateVideoModelSpecAPI3(input.Specification, input.DefaultParameters); err != nil {
		return VideoModelProfileInput{}, "", "", err
	}
	specification, err := common.Marshal(input.Specification)
	if err != nil {
		return VideoModelProfileInput{}, "", "", err
	}
	defaults, err := common.Marshal(input.DefaultParameters)
	if err != nil {
		return VideoModelProfileInput{}, "", "", err
	}
	return input, string(specification), string(defaults), nil
}

func videoModelProfileViewAPI3(profile model.KKAIVideoModelProfile) (VideoModelProfileView, error) {
	specification, defaults, err := decodeVideoModelProfileAPI3(profile)
	if err != nil {
		return VideoModelProfileView{}, err
	}
	return VideoModelProfileView{
		ID: profile.ID, Model: profile.Model, DisplayName: profile.DisplayName, Description: profile.Description,
		ProviderLabel: profile.ProviderLabel, SpecificationVersion: specification.Version,
		Specification: specification, DefaultParameters: defaults, Enabled: profile.Enabled,
		SortOrder: profile.SortOrder, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}, nil
}

func adminVideoModelProfileViewAPI3(ctx context.Context, db *gorm.DB, profile model.KKAIVideoModelProfile) (*VideoModelProfileView, error) {
	view, err := videoModelProfileViewAPI3(profile)
	if err != nil {
		return nil, err
	}
	views := []VideoModelProfileView{view}
	if err := hydrateVideoModelPublishedSampleState(ctx, db, views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func validatePublishedSamplesAgainstSpecAPI3(tx *gorm.DB, profileID int64, previous VideoModelSpec, next VideoModelSpec) error {
	var samples []model.KKAIVideoSample
	if err := tx.Where("model_profile_id = ? AND status = ? AND reference_asset_ids NOT LIKE ?", profileID, model.VideoSampleStatusPublished, `{"reference_assets":%`).Find(&samples).Error; err != nil {
		return err
	}
	for _, sample := range samples {
		parameters := map[string]any{}
		if err := common.UnmarshalJsonStr(sample.Parameters, &parameters); err != nil {
			return fmt.Errorf("decode published sample %d parameters: %w", sample.ID, err)
		}
		if _, err := ValidateVideoParameters(next, sample.Mode, parameters, false); err != nil {
			return fmt.Errorf("published sample %d is incompatible with specification: %w", sample.ID, err)
		}
		previousInputs := expectedVideoReferenceInputsAPI3(previous, sample.Mode)
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshotsAPI3(sample.ReferenceAssetIDs, previousInputs)
		if err != nil {
			return fmt.Errorf("%w: published sample %d has an invalid reference snapshot: %v", ErrInvalidVideoModelSpec, sample.ID, err)
		}
		nextInputs := expectedVideoReferenceInputsAPI3(next, sample.Mode)
		if !videoSampleReferenceSnapshotsMatchAPI3(referenceSnapshots, previousInputs) ||
			!videoSampleReferenceSnapshotsMatchAPI3(referenceSnapshots, nextInputs) {
			return fmt.Errorf("%w: published sample %d reference schema changed", ErrInvalidVideoModelSpec, sample.ID)
		}
	}
	return nil
}
