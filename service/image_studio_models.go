package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type imageStudioAbilityChannel struct {
	Model         string `gorm:"column:model"`
	ChannelID     int    `gorm:"column:channel_id"`
	ChannelType   int    `gorm:"column:channel_type"`
	OtherSettings string `gorm:"column:other_settings"`
}

func enabledImageStudioModelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]string, error) {
	rows, err := enabledImageStudioAbilityChannelsForGroup(ctx, db, group)
	if err != nil {
		return nil, err
	}
	models := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		models[row.Model] = struct{}{}
	}
	result := make([]string, 0, len(models))
	for modelName := range models {
		result = append(result, modelName)
	}
	sort.Strings(result)
	return result, nil
}

func enabledImageStudioAbilityChannelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]imageStudioAbilityChannel, error) {
	if db == nil || strings.TrimSpace(group) == "" {
		return []imageStudioAbilityChannel{}, nil
	}
	var rows []imageStudioAbilityChannel
	err := db.WithContext(ctx).Model(&model.Ability{}).
		Select("abilities.model, abilities.channel_id, channels.type AS channel_type, channels.settings AS other_settings").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where(&model.Ability{Group: strings.TrimSpace(group), Enabled: true}).
		Where("channels.status = ?", common.ChannelStatusEnabled).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list image studio abilities: %w", err)
	}
	result := make([]imageStudioAbilityChannel, 0, len(rows))
	for _, row := range rows {
		if imageStudioChannelSupportsModel(row) {
			result = append(result, row)
		}
	}
	return result, nil
}

func imageStudioChannelSupportsModel(row imageStudioAbilityChannel) bool {
	if row.ChannelType == constant.ChannelTypeAdvancedCustom {
		var settings dto.ChannelOtherSettings
		if strings.TrimSpace(row.OtherSettings) == "" || common.UnmarshalJsonStr(row.OtherSettings, &settings) != nil || settings.AdvancedCustom == nil {
			return false
		}
		for _, endpoint := range settings.AdvancedCustom.SupportedEndpointTypesForModel(row.Model) {
			if endpoint == constant.EndpointTypeImageGeneration {
				return true
			}
		}
		return false
	}
	for _, endpoint := range common.GetEndpointTypesByChannelType(row.ChannelType, row.Model) {
		if endpoint == constant.EndpointTypeImageGeneration {
			return true
		}
	}
	return false
}

func enabledConfiguredImageStudioModelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]string, error) {
	available, err := enabledImageStudioModelsForGroup(ctx, db, group)
	if err != nil || len(available) == 0 {
		return available, err
	}
	var configured []string
	if err := db.WithContext(ctx).Model(&model.KKAIImageModelProfile{}).
		Where("enabled = ? AND model IN ?", true, available).
		Distinct("model").Pluck("model", &configured).Error; err != nil {
		return nil, fmt.Errorf("list configured image studio models: %w", err)
	}
	sort.Strings(configured)
	return configured, nil
}

func imageStudioModelAvailableForGroup(ctx context.Context, db *gorm.DB, group string, modelName string) (bool, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return true, nil
	}
	models, err := enabledConfiguredImageStudioModelsForGroup(ctx, db, group)
	if err != nil {
		return false, err
	}
	for _, available := range models {
		if available == modelName {
			return true, nil
		}
	}
	return false, nil
}
