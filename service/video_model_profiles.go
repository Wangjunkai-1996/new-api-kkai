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

	"gorm.io/gorm"
)

var (
	ErrVideoModelProfileNotFound       = errors.New("video model profile not found")
	ErrVideoModelProfileInUse          = errors.New("video model profile is in use")
	ErrVideoModelProfileDuplicate      = errors.New("video model profile already exists")
	ErrVideoModelProfileModelImmutable = errors.New("video model profile model cannot be changed")
	ErrVideoModelAbilityUnavailable    = errors.New("video model has no enabled Seedance ability")
	ErrVideoModelNeedsSample           = errors.New("an enabled video model needs at least one published sample")
	videoModelNamePattern              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,190}$`)
)

type VideoModelProfileInput struct {
	Model             string         `json:"model"`
	DisplayName       string         `json:"display_name"`
	Description       string         `json:"description"`
	ProviderLabel     string         `json:"provider_label"`
	Specification     VideoModelSpec `json:"specification"`
	DefaultParameters map[string]any `json:"default_parameters"`
	Enabled           bool           `json:"enabled"`
	SortOrder         int            `json:"sort_order"`
}

type VideoModelProfileView struct {
	ID                   int64          `json:"id"`
	Model                string         `json:"model"`
	DisplayName          string         `json:"display_name"`
	Description          string         `json:"description"`
	ProviderLabel        string         `json:"provider_label"`
	SpecificationVersion int            `json:"specification_version"`
	Specification        VideoModelSpec `json:"specification"`
	DefaultParameters    map[string]any `json:"default_parameters"`
	HasPublishedSample   *bool          `json:"has_published_sample,omitempty"`
	Enabled              bool           `json:"enabled"`
	SortOrder            int            `json:"sort_order"`
	CreatedAt            int64          `json:"created_at"`
	UpdatedAt            int64          `json:"updated_at"`
}

func ListVideoModelProfiles(ctx context.Context, db *gorm.DB, includeDisabled bool) ([]VideoModelProfileView, error) {
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
		view, err := videoModelProfileView(profile)
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

func ListVideoModelCandidates(ctx context.Context, db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, ErrVideoModelProfileNotFound
	}
	return enabledVideoStudioModelsForGroup(ctx, db, VideoStudioTokenGroup)
}

func ListEffectiveVideoModelProfiles(
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
		view, err := videoModelProfileView(profile)
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

func GetVideoModelProfileByID(ctx context.Context, db *gorm.DB, id int64) (*VideoModelProfileView, error) {
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
	return adminVideoModelProfileView(ctx, db, profile)
}

func GetEnabledVideoModelProfileByModel(ctx context.Context, db *gorm.DB, modelName string) (*model.KKAIVideoModelProfile, VideoModelSpec, map[string]any, error) {
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
	specification, defaults, err := decodeVideoModelProfile(profile)
	if err != nil {
		return nil, VideoModelSpec{}, nil, err
	}
	return &profile, specification, defaults, nil
}

func resolveVideoModelProfile(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
) (int64, int, string, VideoModelSpec, map[string]any, error) {
	modelName = strings.TrimSpace(modelName)
	if db == nil || !videoModelNamePattern.MatchString(modelName) {
		return 0, 0, "", VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
	}
	var profile model.KKAIVideoModelProfile
	if err := db.WithContext(ctx).Where("model = ? AND enabled = ?", modelName, true).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, "", VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
		}
		return 0, 0, "", VideoModelSpec{}, nil, err
	}
	specification, defaults, err := decodeVideoModelProfile(profile)
	if err != nil {
		return 0, 0, "", VideoModelSpec{}, nil, err
	}
	return profile.ID, profile.SpecificationVersion, profile.Model, specification, defaults, nil
}

func CreateVideoModelProfile(ctx context.Context, db *gorm.DB, input VideoModelProfileInput) (*VideoModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeVideoModelProfileInput(input)
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
	if normalized.Enabled {
		return nil, ErrVideoModelNeedsSample
	}
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: normalized.Model, DisplayName: normalized.DisplayName, Description: normalized.Description,
		ProviderLabel: normalized.ProviderLabel, SpecificationVersion: normalized.Specification.Version,
		Specification: specificationJSON, DefaultParameters: defaultsJSON, Enabled: false,
		SortOrder: normalized.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&profile).Error; err != nil {
		if countErr := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
			Where("model = ?", normalized.Model).Count(&duplicateCount).Error; countErr == nil && duplicateCount > 0 {
			return nil, ErrVideoModelProfileDuplicate
		}
		return nil, fmt.Errorf("create video model profile: %w", err)
	}
	return adminVideoModelProfileView(ctx, db, profile)
}

func UpdateVideoModelProfile(ctx context.Context, db *gorm.DB, id int64, input VideoModelProfileInput) (*VideoModelProfileView, error) {
	normalized, specificationJSON, defaultsJSON, err := normalizeVideoModelProfileInput(input)
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
		if updated.Specification != specificationJSON && normalized.Specification.Version <= updated.SpecificationVersion {
			return fmt.Errorf("%w: specification changes require a higher version", ErrInvalidVideoModelSpec)
		}
		previousSpecification, _, err := decodeVideoModelProfile(updated)
		if err != nil {
			return err
		}
		if err := validatePublishedSamplesAgainstSpec(tx, id, previousSpecification, normalized.Specification); err != nil {
			return err
		}
		if normalized.Enabled {
			var published int64
			if err := tx.Model(&model.KKAIVideoSample{}).
				Where("model_profile_id = ? AND status = ?", id, model.VideoSampleStatusPublished).
				Count(&published).Error; err != nil {
				return err
			}
			if published == 0 {
				return ErrVideoModelNeedsSample
			}
		}
		updates := map[string]any{
			"model": normalized.Model, "display_name": normalized.DisplayName, "description": normalized.Description,
			"provider_label": normalized.ProviderLabel, "specification_version": normalized.Specification.Version,
			"specification": specificationJSON, "default_parameters": defaultsJSON, "enabled": normalized.Enabled,
			"sort_order": normalized.SortOrder, "updated_at": time.Now().Unix(),
		}
		if err := tx.Model(&updated).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("update video model profile: %w", err)
	}
	return adminVideoModelProfileView(ctx, db, updated)
}

func DeleteVideoModelProfile(ctx context.Context, db *gorm.DB, id int64) error {
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

func normalizeVideoModelProfileInput(input VideoModelProfileInput) (VideoModelProfileInput, string, string, error) {
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
	if err := ValidateVideoModelSpec(input.Specification, input.DefaultParameters); err != nil {
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

func videoModelProfileView(profile model.KKAIVideoModelProfile) (VideoModelProfileView, error) {
	specification, defaults, err := decodeVideoModelProfile(profile)
	if err != nil {
		return VideoModelProfileView{}, err
	}
	return VideoModelProfileView{
		ID: profile.ID, Model: profile.Model, DisplayName: profile.DisplayName, Description: profile.Description,
		ProviderLabel: profile.ProviderLabel, SpecificationVersion: profile.SpecificationVersion,
		Specification: specification, DefaultParameters: defaults, Enabled: profile.Enabled,
		SortOrder: profile.SortOrder, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}, nil
}

func adminVideoModelProfileView(ctx context.Context, db *gorm.DB, profile model.KKAIVideoModelProfile) (*VideoModelProfileView, error) {
	view, err := videoModelProfileView(profile)
	if err != nil {
		return nil, err
	}
	views := []VideoModelProfileView{view}
	if err := hydrateVideoModelPublishedSampleState(ctx, db, views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func hydrateVideoModelPublishedSampleState(ctx context.Context, db *gorm.DB, views []VideoModelProfileView) error {
	if len(views) == 0 {
		return nil
	}
	profileIDs := make([]int64, 0, len(views))
	for _, view := range views {
		profileIDs = append(profileIDs, view.ID)
	}
	type publishedSampleCount struct {
		ModelProfileID int64
		Count          int64
	}
	var rows []publishedSampleCount
	if err := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Select("model_profile_id, COUNT(*) AS count").
		Where("model_profile_id IN ? AND status = ?", profileIDs, model.VideoSampleStatusPublished).
		Group("model_profile_id").Scan(&rows).Error; err != nil {
		return fmt.Errorf("count published video samples: %w", err)
	}
	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.ModelProfileID] = row.Count
	}
	for index := range views {
		hasPublishedSample := counts[views[index].ID] > 0
		views[index].HasPublishedSample = &hasPublishedSample
	}
	return nil
}

func decodeVideoModelProfile(profile model.KKAIVideoModelProfile) (VideoModelSpec, map[string]any, error) {
	var specification VideoModelSpec
	if err := common.UnmarshalJsonStr(profile.Specification, &specification); err != nil {
		return VideoModelSpec{}, nil, fmt.Errorf("decode video model specification %d: %w", profile.ID, err)
	}
	defaults := map[string]any{}
	if err := common.UnmarshalJsonStr(profile.DefaultParameters, &defaults); err != nil {
		return VideoModelSpec{}, nil, fmt.Errorf("decode video model defaults %d: %w", profile.ID, err)
	}
	if profile.SpecificationVersion != specification.Version {
		return VideoModelSpec{}, nil, fmt.Errorf("%w: profile %d version mismatch", ErrInvalidVideoModelSpec, profile.ID)
	}
	return specification, defaults, nil
}

func validatePublishedSamplesAgainstSpec(tx *gorm.DB, profileID int64, previous VideoModelSpec, next VideoModelSpec) error {
	var samples []model.KKAIVideoSample
	if err := tx.Where("model_profile_id = ? AND status = ?", profileID, model.VideoSampleStatusPublished).Find(&samples).Error; err != nil {
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
		previousInputs := expectedVideoReferenceInputs(previous, sample.Mode)
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshots(sample.ReferenceAssetIDs, previousInputs)
		if err != nil {
			return fmt.Errorf("%w: published sample %d has an invalid reference snapshot: %v", ErrInvalidVideoModelSpec, sample.ID, err)
		}
		nextInputs := expectedVideoReferenceInputs(next, sample.Mode)
		if !videoSampleReferenceSnapshotsMatch(referenceSnapshots, previousInputs) ||
			!videoSampleReferenceSnapshotsMatch(referenceSnapshots, nextInputs) {
			return fmt.Errorf("%w: published sample %d reference schema changed", ErrInvalidVideoModelSpec, sample.ID)
		}
	}
	return nil
}
