//go:build kkai_bridge

package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	videoSampleCategoryFeatureEnabled = false
	videoSampleCategorySelectColumn   = "'other' AS category"
)

func normalizeVideoSampleCategory(value string) (string, error) {
	category := strings.TrimSpace(value)
	if category == "" || category == model.VideoSampleCategoryOther {
		return model.VideoSampleCategoryOther, nil
	}
	return "", ErrInvalidVideoSample
}

func normalizeVideoSampleCategoryFilter(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return normalizeVideoSampleCategory(value)
}

func videoSampleCategoryForView(string) (string, error) {
	return model.VideoSampleCategoryOther, nil
}

func applyVideoSampleCategoryFilter(query *gorm.DB, _ string) (*gorm.DB, error) {
	return query, nil
}

func createVideoSampleRecord(tx *gorm.DB, sample *model.KKAIVideoSample) error {
	return tx.Omit("category").Create(sample).Error
}

func addVideoSampleCategoryUpdate(map[string]any, string) {}
