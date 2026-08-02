package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"gorm.io/gorm"
)

var (
	ErrImageModelProfileNotFound       = errors.New("image model profile not found")
	ErrImageModelProfileInUse          = errors.New("image model profile is in use")
	ErrImageModelProfileDuplicate      = errors.New("image model profile already exists")
	ErrImageModelProfileModelImmutable = errors.New("image model profile model cannot be changed")
	ErrImageModelProfileConflict       = errors.New("image model profile was changed concurrently")
	ErrImageModelAbilityUnavailable    = errors.New("image model has no enabled image-generation ability")
	ErrImageModelBillingUnsupported    = errors.New("image studio first phase does not support tiered-expression billing")
	imageModelNamePattern              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,190}$`)
)

type ImageModelProfileInput struct {
	Model             string         `json:"model"`
	DisplayName       string         `json:"display_name"`
	Description       string         `json:"description"`
	ProviderLabel     string         `json:"provider_label"`
	Specification     ImageModelSpec `json:"specification"`
	DefaultParameters map[string]any `json:"default_parameters"`
	Enabled           bool           `json:"enabled"`
	SortOrder         int            `json:"sort_order"`
}

type ImageModelProfileView struct {
	ID                   int64          `json:"id"`
	Model                string         `json:"model"`
	DisplayName          string         `json:"display_name"`
	Description          string         `json:"description"`
	ProviderLabel        string         `json:"provider_label"`
	SpecificationVersion int            `json:"specification_version"`
	Specification        ImageModelSpec `json:"specification"`
	DefaultParameters    map[string]any `json:"default_parameters"`
	HasPublishedSample   *bool          `json:"has_published_sample,omitempty"`
	Enabled              bool           `json:"enabled"`
	SortOrder            int            `json:"sort_order"`
	CreatedAt            int64          `json:"created_at"`
	UpdatedAt            int64          `json:"updated_at"`
}

func ListImageModelProfiles(ctx context.Context, db *gorm.DB, includeDisabled bool) ([]ImageModelProfileView, error) {
	if db == nil {
		return nil, ErrImageModelProfileNotFound
	}
	query := db.WithContext(ctx).Order("sort_order ASC, id ASC")
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	var profiles []model.KKAIImageModelProfile
	if err := query.Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("list image model profiles: %w", err)
	}
	views := make([]ImageModelProfileView, 0, len(profiles))
	for _, profile := range profiles {
		view, err := imageModelProfileView(profile)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if includeDisabled {
		if err := hydrateImageModelPublishedSampleState(ctx, db, views); err != nil {
			return nil, err
		}
	}
	return views, nil
}

func ListImageModelCandidates(ctx context.Context, db *gorm.DB) ([]string, error) {
	models, err := enabledImageStudioModelsForGroup(ctx, db, ImageStudioTokenGroup)
	if err != nil {
		return nil, err
	}
	return imageStudioModelsWithSupportedBilling(models), nil
}

func ListEffectiveImageModelProfiles(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	tokenID int,
	clientIP string,
) ([]ImageModelProfileView, error) {
	token, err := ValidateImageStudioToken(ctx, db, userID, tokenID, "", clientIP)
	if err != nil {
		return nil, err
	}
	models, err := enabledConfiguredImageStudioModelsForGroup(ctx, db, token.Group)
	if err != nil || len(models) == 0 {
		return []ImageModelProfileView{}, err
	}
	var profiles []model.KKAIImageModelProfile
	if err := db.WithContext(ctx).Where("enabled = ? AND model IN ?", true, models).Find(&profiles).Error; err != nil {
		return nil, fmt.Errorf("list effective image model profiles: %w", err)
	}
	views := make([]ImageModelProfileView, 0, len(profiles))
	for _, profile := range profiles {
		if !imageStudioBillingModeSupported(profile.Model) {
			continue
		}
		view, err := imageModelProfileView(profile)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(left int, right int) bool {
		if views[left].SortOrder != views[right].SortOrder {
			return views[left].SortOrder < views[right].SortOrder
		}
		return views[left].Model < views[right].Model
	})
	return views, nil
}

func GetImageModelProfileByID(ctx context.Context, db *gorm.DB, id int64) (*ImageModelProfileView, error) {
	if db == nil || id <= 0 {
		return nil, ErrImageModelProfileNotFound
	}
	var profile model.KKAIImageModelProfile
	if err := db.WithContext(ctx).First(&profile, "id = ?", id).Error; err != nil {
		return nil, imageModelProfileLookupError(err)
	}
	view, err := imageModelProfileView(profile)
	if err != nil {
		return nil, err
	}
	views := []ImageModelProfileView{view}
	if err := hydrateImageModelPublishedSampleState(ctx, db, views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func resolveImageModelProfile(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
) (*model.KKAIImageModelProfile, ImageModelSpec, map[string]any, error) {
	modelName = strings.TrimSpace(modelName)
	if db == nil || !imageModelNamePattern.MatchString(modelName) {
		return nil, ImageModelSpec{}, nil, ErrImageModelProfileNotFound
	}
	var profile model.KKAIImageModelProfile
	if err := db.WithContext(ctx).Where("model = ? AND enabled = ?", modelName, true).First(&profile).Error; err != nil {
		return nil, ImageModelSpec{}, nil, imageModelProfileLookupError(err)
	}
	if !imageStudioBillingModeSupported(profile.Model) {
		return nil, ImageModelSpec{}, nil, ErrImageModelBillingUnsupported
	}
	specification, defaults, err := decodeImageModelProfile(profile)
	return &profile, specification, defaults, err
}

func CreateImageModelProfile(ctx context.Context, db *gorm.DB, input ImageModelProfileInput) (*ImageModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeImageModelProfileInput(input)
	if err != nil {
		return nil, err
	}
	if err := ensureImageModelAbility(ctx, db, normalized.Model); err != nil {
		return nil, err
	}
	if normalized.Enabled && !imageStudioBillingModeSupported(normalized.Model) {
		return nil, ErrImageModelBillingUnsupported
	}
	now := time.Now().Unix()
	profile := model.KKAIImageModelProfile{
		Model: normalized.Model, DisplayName: normalized.DisplayName, Description: normalized.Description,
		ProviderLabel: normalized.ProviderLabel, SpecificationVersion: normalized.Specification.Version,
		Specification: specificationJSON, DefaultParameters: defaultsJSON, Enabled: normalized.Enabled,
		SortOrder: normalized.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&profile).Error; err != nil {
		var count int64
		if countErr := db.WithContext(ctx).Model(&model.KKAIImageModelProfile{}).
			Where("model = ?", normalized.Model).Count(&count).Error; countErr == nil && count > 0 {
			return nil, ErrImageModelProfileDuplicate
		}
		return nil, fmt.Errorf("create image model profile: %w", err)
	}
	return GetImageModelProfileByID(ctx, db, profile.ID)
}

func UpdateImageModelProfile(ctx context.Context, db *gorm.DB, id int64, input ImageModelProfileInput) (*ImageModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeImageModelProfileInput(input)
	if err != nil || db == nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrImageModelProfileNotFound
	}
	var current model.KKAIImageModelProfile
	if err := db.WithContext(ctx).First(&current, "id = ?", id).Error; err != nil {
		return nil, imageModelProfileLookupError(err)
	}
	if normalized.Model != current.Model {
		return nil, ErrImageModelProfileModelImmutable
	}
	if normalized.Enabled {
		if err := ensureImageModelAbility(ctx, db, normalized.Model); err != nil {
			return nil, err
		}
		if !imageStudioBillingModeSupported(normalized.Model) {
			return nil, ErrImageModelBillingUnsupported
		}
	}
	if current.Specification != specificationJSON && normalized.Specification.Version <= current.SpecificationVersion {
		return nil, fmt.Errorf("%w: specification changes require a higher version", ErrInvalidImageModelSpec)
	}
	if err := validatePublishedImageSamplesAgainstSpec(ctx, db, id, normalized.Specification); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now <= current.UpdatedAt {
		now = current.UpdatedAt + 1
	}
	result := db.WithContext(ctx).Model(&model.KKAIImageModelProfile{}).
		Where("id = ? AND updated_at = ?", id, current.UpdatedAt).
		Updates(map[string]any{
			"display_name": normalized.DisplayName, "description": normalized.Description,
			"provider_label": normalized.ProviderLabel, "specification_version": normalized.Specification.Version,
			"specification": specificationJSON, "default_parameters": defaultsJSON,
			"enabled": normalized.Enabled, "sort_order": normalized.SortOrder, "updated_at": now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("update image model profile: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrImageModelProfileConflict
	}
	return GetImageModelProfileByID(ctx, db, id)
}

func imageStudioBillingModeSupported(modelName string) bool {
	return billing_setting.GetBillingMode(strings.TrimSpace(modelName)) != billing_setting.BillingModeTieredExpr
}

func imageStudioModelsWithSupportedBilling(models []string) []string {
	supported := make([]string, 0, len(models))
	for _, modelName := range models {
		if imageStudioBillingModeSupported(modelName) {
			supported = append(supported, modelName)
		}
	}
	return supported
}

func DeleteImageModelProfile(ctx context.Context, db *gorm.DB, id int64) error {
	if db == nil || id <= 0 {
		return ErrImageModelProfileNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile model.KKAIImageModelProfile
		if err := tx.First(&profile, "id = ?", id).Error; err != nil {
			return imageModelProfileLookupError(err)
		}
		var references int64
		if err := tx.Model(&model.KKAIImageSample{}).Where("model_profile_id = ?", id).Count(&references).Error; err != nil {
			return err
		}
		if references == 0 {
			if err := tx.Model(&model.KKAIImageGeneration{}).Where("model_profile_id = ?", id).Count(&references).Error; err != nil {
				return err
			}
		}
		if references > 0 {
			return ErrImageModelProfileInUse
		}
		return tx.Delete(&profile).Error
	})
}

func normalizeImageModelProfileInput(input ImageModelProfileInput) (ImageModelProfileInput, string, string, error) {
	input.Model = strings.TrimSpace(input.Model)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Description = strings.TrimSpace(input.Description)
	input.ProviderLabel = strings.TrimSpace(input.ProviderLabel)
	if !imageModelNamePattern.MatchString(input.Model) || input.DisplayName == "" || len(input.DisplayName) > 191 ||
		len(input.Description) > 4000 || len(input.ProviderLabel) > 128 || input.SortOrder < -100000 || input.SortOrder > 100000 {
		return ImageModelProfileInput{}, "", "", ErrInvalidImageModelSpec
	}
	if input.DefaultParameters == nil {
		input.DefaultParameters = map[string]any{}
	}
	if err := ValidateImageModelSpec(input.Specification, input.DefaultParameters); err != nil {
		return ImageModelProfileInput{}, "", "", err
	}
	specification, err := common.Marshal(input.Specification)
	if err != nil {
		return ImageModelProfileInput{}, "", "", err
	}
	defaults, err := common.Marshal(input.DefaultParameters)
	if err != nil {
		return ImageModelProfileInput{}, "", "", err
	}
	return input, string(specification), string(defaults), nil
}

func decodeImageModelProfile(profile model.KKAIImageModelProfile) (ImageModelSpec, map[string]any, error) {
	var specification ImageModelSpec
	if err := common.UnmarshalJsonStr(profile.Specification, &specification); err != nil {
		return ImageModelSpec{}, nil, fmt.Errorf("decode image model specification %d: %w", profile.ID, err)
	}
	defaults := map[string]any{}
	if err := common.UnmarshalJsonStr(profile.DefaultParameters, &defaults); err != nil {
		return ImageModelSpec{}, nil, fmt.Errorf("decode image model defaults %d: %w", profile.ID, err)
	}
	if specification.Version != profile.SpecificationVersion || ValidateImageModelSpec(specification, defaults) != nil {
		return ImageModelSpec{}, nil, fmt.Errorf("%w: profile %d is inconsistent", ErrInvalidImageModelSpec, profile.ID)
	}
	return specification, defaults, nil
}

func imageModelProfileView(profile model.KKAIImageModelProfile) (ImageModelProfileView, error) {
	specification, defaults, err := decodeImageModelProfile(profile)
	if err != nil {
		return ImageModelProfileView{}, err
	}
	return ImageModelProfileView{
		ID: profile.ID, Model: profile.Model, DisplayName: profile.DisplayName, Description: profile.Description,
		ProviderLabel: profile.ProviderLabel, SpecificationVersion: profile.SpecificationVersion,
		Specification: specification, DefaultParameters: defaults, Enabled: profile.Enabled,
		SortOrder: profile.SortOrder, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}, nil
}

func hydrateImageModelPublishedSampleState(ctx context.Context, db *gorm.DB, views []ImageModelProfileView) error {
	if len(views) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.ID)
	}
	type countRow struct {
		ModelProfileID int64 `gorm:"column:model_profile_id"`
		Count          int64 `gorm:"column:count"`
	}
	var rows []countRow
	if err := db.WithContext(ctx).Model(&model.KKAIImageSample{}).
		Select("model_profile_id, COUNT(*) AS count").
		Where("model_profile_id IN ? AND status = ?", ids, model.ImageSampleStatusPublished).
		Group("model_profile_id").Scan(&rows).Error; err != nil {
		return fmt.Errorf("count published image samples: %w", err)
	}
	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.ModelProfileID] = row.Count
	}
	for index := range views {
		hasPublished := counts[views[index].ID] > 0
		views[index].HasPublishedSample = &hasPublished
	}
	return nil
}

func validatePublishedImageSamplesAgainstSpec(ctx context.Context, db *gorm.DB, profileID int64, specification ImageModelSpec) error {
	var samples []model.KKAIImageSample
	if err := db.WithContext(ctx).Where(
		"model_profile_id = ? AND status = ?", profileID, model.ImageSampleStatusPublished,
	).Find(&samples).Error; err != nil {
		return err
	}
	for _, sample := range samples {
		parameters := map[string]any{}
		if err := common.UnmarshalJsonStr(sample.Parameters, &parameters); err != nil {
			return fmt.Errorf("decode published image sample %d parameters: %w", sample.ID, err)
		}
		if _, err := ValidateImageParameters(specification, parameters, true); err != nil {
			return fmt.Errorf("published image sample %d is incompatible: %w", sample.ID, err)
		}
	}
	return nil
}

func ensureImageModelAbility(ctx context.Context, db *gorm.DB, modelName string) error {
	models, err := enabledImageStudioModelsForGroup(ctx, db, ImageStudioTokenGroup)
	if err != nil {
		return err
	}
	if !containsImageStudioModel(models, modelName) {
		return ErrImageModelAbilityUnavailable
	}
	return nil
}

func imageModelProfileLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrImageModelProfileNotFound
	}
	return fmt.Errorf("get image model profile: %w", err)
}
