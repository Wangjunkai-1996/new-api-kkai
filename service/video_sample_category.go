//go:build !kkai_bridge

package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	videoSampleCategoryFeatureEnabled = true
	videoSampleCategorySelectColumn   = "COALESCE(NULLIF(kkai_video_samples.category, ''), 'other') AS category"
)

func normalizeVideoSampleCategory(value string) (string, error) {
	category := strings.TrimSpace(value)
	if category == "" {
		return model.VideoSampleCategoryOther, nil
	}
	switch category {
	case model.VideoSampleCategoryPeople,
		model.VideoSampleCategoryAnimals,
		model.VideoSampleCategoryNature,
		model.VideoSampleCategoryAnimation,
		model.VideoSampleCategoryProduct,
		model.VideoSampleCategoryArchitecture,
		model.VideoSampleCategoryFood,
		model.VideoSampleCategoryEffects,
		model.VideoSampleCategoryOther:
		return category, nil
	default:
		return "", ErrInvalidVideoSample
	}
}

func normalizeVideoSampleCategoryFilter(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return normalizeVideoSampleCategory(value)
}

func videoSampleCategoryForView(value string) (string, error) {
	category, err := normalizeVideoSampleCategory(value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid stored category", ErrVideoSampleDataCorrupt)
	}
	return category, nil
}

func applyVideoSampleCategoryFilter(query *gorm.DB, category string) (*gorm.DB, error) {
	if category == model.VideoSampleCategoryOther {
		return query.Where(
			"(kkai_video_samples.category = ? OR kkai_video_samples.category IS NULL OR kkai_video_samples.category = '')",
			category,
		), nil
	}
	if category != "" {
		return query.Where("kkai_video_samples.category = ?", category), nil
	}
	return query, nil
}

func createVideoSampleRecord(tx *gorm.DB, sample *model.KKAIVideoSample) error {
	return tx.Create(sample).Error
}

func addVideoSampleCategoryUpdate(updates map[string]any, category string) {
	updates["category"] = category
}
