package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

const VideoStudioMultimodalOption = "video_studio.multimodal_enabled"

// LoadVideoStudioConfiguration keeps batch preparation and switch changes in
// the same lock order. A missing option is returned as nil, never inserted.
func LoadVideoStudioConfiguration(ctx context.Context, tx *gorm.DB, profileIDs []int64, forUpdate bool) ([]KKAIVideoModelProfile, *Option, error) {
	profilesQuery := tx.WithContext(ctx).Where("id IN ?", profileIDs).Order("id ASC")
	if forUpdate {
		profilesQuery = lockForUpdate(profilesQuery)
	}
	var profiles []KKAIVideoModelProfile
	if err := profilesQuery.Find(&profiles).Error; err != nil {
		return nil, nil, err
	}
	optionQuery := tx.WithContext(ctx).Where("key = ?", VideoStudioMultimodalOption)
	if forUpdate {
		optionQuery = lockForUpdate(optionQuery)
	}
	var option Option
	err := optionQuery.Take(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return profiles, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return profiles, &option, nil
}
