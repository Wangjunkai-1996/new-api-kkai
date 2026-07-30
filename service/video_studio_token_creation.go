package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

func EnsureVideoStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (VideoStudioTokenEnsureResult, error) {
	result := VideoStudioTokenEnsureResult{
		VideoStudioTokenCapability: VideoStudioTokenCapability{
			RequiredGroup:   VideoStudioTokenGroup,
			EffectiveModels: []string{},
			Status:          VideoStudioTokenStatusMissing,
		},
	}
	if db == nil || userID <= 0 {
		return result, ErrVideoStudioTokenInvalid
	}
	user, err := getCurrentVideoStudioUser(ctx, db, userID)
	if err != nil {
		return result, err
	}
	if !videoStudioUserCanUseGroup(user) {
		result.Status = VideoStudioTokenStatusGroupUnavailable
		return result, ErrVideoStudioTokenGroupUnavailable
	}

	modelName = strings.TrimSpace(modelName)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		selected, created, migrated, err := ensureVideoStudioTokenOnce(ctx, db, userID, modelName, clientIP)
		if err == nil {
			invalidateMigratedVideoStudioTokenCache(userID, migrated)
			result.HasUsableToken = true
			result.Status = VideoStudioTokenStatusReady
			result.Token = videoStudioTokenView(selected)
			result.Created = created
			result.EffectiveModels, err = effectiveVideoStudioModelsForToken(ctx, db, userID, selected.Id, clientIP)
			if err != nil {
				return result, err
			}
			if modelName != "" && !containsVideoStudioModel(result.EffectiveModels, modelName) {
				return result, ErrVideoStudioTokenModelsUnavailable
			}
			result.CanCreate = false
			return result, nil
		}
		lastErr = err
		if !isRetryableVideoStudioTokenCreationError(err) {
			return result, err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return result, lastErr
}

func ensureVideoStudioTokenOnce(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (*model.Token, bool, bool, error) {
	var selected *model.Token
	createdToken := false
	migratedToken := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := model.LockUserForTokenCreation(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("lock video studio token owner: %w", err)
		}
		if !videoStudioUserCanUseGroup(user) {
			return ErrVideoStudioTokenGroupUnavailable
		}
		existing, migrated, err := findUsableVideoStudioToken(ctx, tx, userID, "", clientIP)
		migratedToken = migratedToken || migrated
		if err != nil {
			return err
		}
		if existing != nil {
			effectiveModels, err := effectiveVideoStudioModelsForTokenRecord(ctx, tx, existing)
			if err != nil {
				return err
			}
			if modelName != "" && !containsVideoStudioModel(effectiveModels, modelName) {
				return ErrVideoStudioTokenModelsUnavailable
			}
			selected = existing
			return nil
		}

		modelAvailable, err := videoStudioModelAvailableForGroup(ctx, tx, VideoStudioTokenGroup, modelName)
		if err != nil {
			return err
		}
		if !modelAvailable {
			return ErrVideoStudioTokenModelsUnavailable
		}

		creationStatus, _, err := videoStudioTokenCreationState(ctx, tx, userID, modelName)
		if err != nil {
			return err
		}
		switch creationStatus {
		case VideoStudioTokenStatusLimitReached:
			return ErrVideoStudioTokenLimitReached
		case VideoStudioTokenStatusModelsUnavailable:
			return ErrVideoStudioTokenModelsUnavailable
		case VideoStudioTokenStatusMissing:
		default:
			return ErrVideoStudioTokenInvalid
		}

		key, err := common.GenerateKey()
		if err != nil {
			return fmt.Errorf("generate video studio token key: %w", err)
		}
		now := common.GetTimestamp()
		created := &model.Token{
			UserId: userID, Key: key, Status: common.TokenStatusEnabled, Name: videoStudioTokenName,
			CreatedTime: now, AccessedTime: now, ExpiredTime: -1, UnlimitedQuota: true,
			ModelLimitsEnabled: false, ModelLimits: "",
			Group: VideoStudioTokenGroup, CrossGroupRetry: false,
		}
		if err := tx.Create(created).Error; err != nil {
			return fmt.Errorf("create video studio token: %w", err)
		}
		selected = created
		createdToken = true
		return nil
	})
	if err != nil {
		return nil, false, false, err
	}
	return selected, createdToken, migratedToken, nil
}

func videoStudioTokenCreationState(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
) (VideoStudioTokenStatus, []string, error) {
	var tokenCount int64
	if err := db.WithContext(ctx).Model(&model.Token{}).Where("user_id = ?", userID).Count(&tokenCount).Error; err != nil {
		return "", nil, fmt.Errorf("count user tokens: %w", err)
	}
	if tokenCount >= int64(operation_setting.GetMaxUserTokens()) {
		return VideoStudioTokenStatusLimitReached, nil, nil
	}
	enabledModels, err := enabledVideoStudioTokenModels(ctx, db)
	if err != nil {
		return "", nil, err
	}
	if len(enabledModels) == 0 || (modelName != "" && !containsVideoStudioModel(enabledModels, modelName)) {
		return VideoStudioTokenStatusModelsUnavailable, enabledModels, nil
	}
	return VideoStudioTokenStatusMissing, enabledModels, nil
}

func enabledVideoStudioTokenModels(ctx context.Context, db *gorm.DB) ([]string, error) {
	return enabledConfiguredVideoStudioModelsForGroup(ctx, db, VideoStudioTokenGroup)
}

func enabledVideoStudioModelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]string, error) {
	models := []string{}
	if err := db.WithContext(ctx).Model(&model.Ability{}).
		Where(&model.Ability{Group: strings.TrimSpace(group), Enabled: true}).
		Distinct("model").Pluck("model", &models).Error; err != nil {
		return nil, fmt.Errorf("list enabled video studio models: %w", err)
	}
	sort.Strings(models)
	return models, nil
}

func enabledConfiguredVideoStudioModelsForGroup(ctx context.Context, db *gorm.DB, group string) ([]string, error) {
	abilityModels, err := enabledVideoStudioModelsForGroup(ctx, db, group)
	if err != nil || len(abilityModels) == 0 {
		return abilityModels, err
	}
	var models []string
	if err := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
		Where("enabled = ? AND model IN ?", true, abilityModels).
		Distinct("model").Pluck("model", &models).Error; err != nil {
		return nil, fmt.Errorf("list configured video studio models: %w", err)
	}
	sort.Strings(models)
	return models, nil
}

func containsVideoStudioModel(models []string, modelName string) bool {
	for _, enabledModel := range models {
		if enabledModel == modelName {
			return true
		}
	}
	return false
}

func isRetryableVideoStudioTokenCreationError(err error) bool {
	if err == nil || !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") || strings.Contains(message, "database is deadlocked")
}
