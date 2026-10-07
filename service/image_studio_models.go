package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

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
	var unclassified []imageStudioAbilityChannel
	var modelNames []string
	for _, row := range rows {
		if imageStudioChannelSupportsModel(row) {
			result = append(result, row)
		} else if !constant.IsAdvancedCustomChannel(row.ChannelType) {
			unclassified = append(unclassified, row)
			modelNames = append(modelNames, row.Model)
		}
	}
	if len(unclassified) == 0 {
		return result, nil
	}
	// Custom model names can declare image support in model metadata. Advanced
	// custom channels still require their own matching image endpoint above.
	var metadata []model.Model
	if err := db.WithContext(ctx).Select("model_name", "name_rule", "endpoints").
		Where("model_name IN ? OR name_rule <> ?", modelNames, model.NameRuleExact).
		Find(&metadata).Error; err != nil {
		return nil, fmt.Errorf("list image studio model endpoints: %w", err)
	}
	for _, row := range unclassified {
		entry := imageStudioMatchingModelMetadata(metadata, row.Model)
		if entry == nil {
			continue
		}
		if imageStudioMetadataSupportsImageEndpoint(entry.Endpoints) &&
			imageStudioMetadataCanRouteImages(row.ChannelType) {
			result = append(result, row)
		}
	}
	return result, nil
}

func imageStudioMetadataCanRouteImages(channelType int) bool {
	// Match the relay's OpenAI fallback for compatible/custom channel types.
	// Native Gemini/Vertex only accept Imagen, already inferred by model name.
	apiType, _ := common.ChannelType2APIType(channelType)
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
		constant.APITypeAli, constant.APITypeZhipuV4,
		constant.APITypeSiliconFlow, constant.APITypeVolcEngine,
		constant.APITypeXai, constant.APITypeJimeng, constant.APITypeMiniMax,
		constant.APITypeReplicate,
		constant.APITypeSub2API, constant.APITypeNewAPI:
		return true
	default:
		return false
	}
}

func imageStudioMetadataSupportsImageEndpoint(raw string) bool {
	var value any
	if common.UnmarshalJsonStr(raw, &value) != nil {
		return false
	}
	switch endpoints := value.(type) {
	case []any:
		for _, endpoint := range endpoints {
			if endpoint, ok := endpoint.(string); ok && endpoint == string(relaytypes.EndpointTypeImageGeneration) {
				return true
			}
		}
	case map[string]any:
		_, ok := endpoints[string(relaytypes.EndpointTypeImageGeneration)]
		return ok
	}
	return false
}

func imageStudioMatchingModelMetadata(metadata []model.Model, modelName string) *model.Model {
	for _, rule := range []int{model.NameRuleExact, model.NameRulePrefix, model.NameRuleSuffix, model.NameRuleContains} {
		for index := range metadata {
			entry := &metadata[index]
			if entry.NameRule == rule && entry.MatchesName(modelName) {
				return entry
			}
		}
	}
	return nil
}

func imageStudioChannelSupportsModel(row imageStudioAbilityChannel) bool {
	if constant.IsAdvancedCustomChannel(row.ChannelType) {
		var settings dto.ChannelOtherSettings
		if strings.TrimSpace(row.OtherSettings) != "" && common.UnmarshalJsonStr(row.OtherSettings, &settings) != nil {
			return false
		}
		if settings.AdvancedCustom == nil {
			settings.AdvancedCustom = common.GetAdvancedCustomPreset(row.ChannelType)
		}
		if settings.AdvancedCustom == nil {
			return false
		}
		for _, endpoint := range settings.AdvancedCustom.SupportedEndpointTypesForModel(row.Model) {
			if endpoint == relaytypes.EndpointTypeImageGeneration {
				return true
			}
		}
		return false
	}
	for _, endpoint := range common.GetEndpointTypesByChannelType(row.ChannelType, row.Model) {
		if endpoint == relaytypes.EndpointTypeImageGeneration {
			return true
		}
	}
	return false
}

func enabledImageStudioCatalogModelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]string, error) {
	available, err := enabledImageStudioModelsForGroup(ctx, db, group)
	if err != nil || len(available) == 0 {
		return available, err
	}
	var disabled []string
	if err := db.WithContext(ctx).Model(&model.KKAIImageModelProfile{}).
		Where("enabled = ? AND model IN ?", false, available).
		Pluck("model", &disabled).Error; err != nil {
		return nil, fmt.Errorf("list disabled image studio models: %w", err)
	}
	result := make([]string, 0, len(available))
	for _, modelName := range available {
		if imageModelNamePattern.MatchString(modelName) && !containsImageStudioModel(disabled, modelName) &&
			imageStudioBillingModeSupported(modelName) {
			result = append(result, modelName)
		}
	}
	return result, nil
}

func imageStudioModelAvailableForGroup(ctx context.Context, db *gorm.DB, group string, modelName string) (bool, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return true, nil
	}
	models, err := enabledImageStudioCatalogModelsForGroup(ctx, db, group)
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
