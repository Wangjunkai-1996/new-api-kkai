package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

const ImageStudioTokenGroup = "图片工作室"

const imageStudioTokenName = "图片工作室"

type ImageStudioTokenStatus string

const (
	ImageStudioTokenStatusReady             ImageStudioTokenStatus = "ready"
	ImageStudioTokenStatusMissing           ImageStudioTokenStatus = "missing"
	ImageStudioTokenStatusGroupUnavailable  ImageStudioTokenStatus = "group_unavailable"
	ImageStudioTokenStatusLimitReached      ImageStudioTokenStatus = "limit_reached"
	ImageStudioTokenStatusModelsUnavailable ImageStudioTokenStatus = "models_unavailable"
)

var (
	ErrImageStudioTokenRequired          = errors.New("image studio token is required")
	ErrImageStudioTokenInvalid           = errors.New("image studio token is invalid")
	ErrImageStudioTokenGroupInvalid      = errors.New("image studio token must use the image studio group")
	ErrImageStudioTokenModelForbidden    = errors.New("image studio token does not allow this model")
	ErrImageStudioTokenGroupUnavailable  = errors.New("image studio group is not available for this account")
	ErrImageStudioTokenLimitReached      = errors.New("image studio token cannot be created because the token limit was reached")
	ErrImageStudioTokenModelsUnavailable = errors.New("no enabled image studio models are available")
	ErrImageStudioTokenIPForbidden       = errors.New("image studio token cannot be used from this IP address")
)

type ImageStudioTokenView struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type ImageStudioTokenCapability struct {
	RequiredGroup   string                 `json:"required_group"`
	HasUsableToken  bool                   `json:"has_usable_token"`
	CanCreate       bool                   `json:"can_create"`
	EffectiveModels []string               `json:"effective_models"`
	Status          ImageStudioTokenStatus `json:"status"`
	Token           *ImageStudioTokenView  `json:"token,omitempty"`
}

type ImageStudioTokenEnsureResult struct {
	ImageStudioTokenCapability
	Created bool `json:"created"`
}

func GetImageStudioTokenStatus(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (ImageStudioTokenCapability, error) {
	capability := ImageStudioTokenCapability{
		RequiredGroup: ImageStudioTokenGroup, EffectiveModels: []string{}, Status: ImageStudioTokenStatusMissing,
	}
	user, err := getImageStudioUser(ctx, db, userID)
	if err != nil {
		return capability, err
	}
	if !imageStudioUserCanUseGroup(user) {
		capability.Status = ImageStudioTokenStatusGroupUnavailable
		return capability, nil
	}
	token, err := findImageStudioToken(ctx, db, userID, clientIP)
	if err != nil {
		return capability, err
	}
	models, err := enabledConfiguredImageStudioModelsForGroup(ctx, db, ImageStudioTokenGroup)
	if err != nil {
		return capability, err
	}
	capability.EffectiveModels = models
	if len(models) == 0 || (strings.TrimSpace(modelName) != "" && !containsImageStudioModel(models, modelName)) {
		capability.Status = ImageStudioTokenStatusModelsUnavailable
		return capability, nil
	}
	if token != nil {
		capability.HasUsableToken = true
		capability.Status = ImageStudioTokenStatusReady
		capability.Token = imageStudioTokenView(token)
		return capability, nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Token{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return capability, fmt.Errorf("count user tokens: %w", err)
	}
	if count >= int64(operation_setting.GetMaxUserTokens()) {
		capability.Status = ImageStudioTokenStatusLimitReached
		return capability, nil
	}
	capability.CanCreate = true
	return capability, nil
}

func EnsureImageStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (ImageStudioTokenEnsureResult, error) {
	result := ImageStudioTokenEnsureResult{ImageStudioTokenCapability: ImageStudioTokenCapability{
		RequiredGroup: ImageStudioTokenGroup, EffectiveModels: []string{}, Status: ImageStudioTokenStatusMissing,
	}}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		token, created, models, err := ensureImageStudioTokenOnce(ctx, db, userID, strings.TrimSpace(modelName), clientIP)
		if err == nil {
			result.HasUsableToken = true
			result.Status = ImageStudioTokenStatusReady
			result.Token = imageStudioTokenView(token)
			result.EffectiveModels = models
			result.Created = created
			if created {
				if cacheErr := model.InvalidateUserTokensCache(userID); cacheErr != nil {
					common.SysLog("failed to invalidate image studio token cache: " + cacheErr.Error())
				}
			}
			return result, nil
		}
		lastErr = err
		if !isRetryableImageStudioTokenCreationError(err) {
			return result, err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return result, lastErr
}

func ensureImageStudioTokenOnce(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (*model.Token, bool, []string, error) {
	if db == nil || userID <= 0 {
		return nil, false, nil, ErrImageStudioTokenInvalid
	}
	var selected *model.Token
	created := false
	models := []string{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := model.LockUserForTokenCreation(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("lock image studio token owner: %w", err)
		}
		if !imageStudioUserCanUseGroup(user) {
			return ErrImageStudioTokenGroupUnavailable
		}
		models, err = enabledConfiguredImageStudioModelsForGroup(ctx, tx, ImageStudioTokenGroup)
		if err != nil {
			return err
		}
		if len(models) == 0 || (modelName != "" && !containsImageStudioModel(models, modelName)) {
			return ErrImageStudioTokenModelsUnavailable
		}
		existing, err := findImageStudioToken(ctx, tx, userID, clientIP)
		if err != nil {
			return err
		}
		if existing != nil {
			selected = existing
			return nil
		}
		var count int64
		if err := tx.Model(&model.Token{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
			return fmt.Errorf("count user tokens: %w", err)
		}
		if count >= int64(operation_setting.GetMaxUserTokens()) {
			return ErrImageStudioTokenLimitReached
		}
		key, err := common.GenerateKey()
		if err != nil {
			return fmt.Errorf("generate image studio token key: %w", err)
		}
		now := common.GetTimestamp()
		selected = &model.Token{
			UserId: userID, Key: key, Status: common.TokenStatusEnabled, Name: imageStudioTokenName,
			CreatedTime: now, AccessedTime: now, ExpiredTime: -1, UnlimitedQuota: true,
			ModelLimitsEnabled: false, ModelLimits: "", Group: ImageStudioTokenGroup, CrossGroupRetry: false,
		}
		if err := tx.Create(selected).Error; err != nil {
			return fmt.Errorf("create image studio token: %w", err)
		}
		created = true
		return nil
	})
	return selected, created, models, err
}

func ValidateImageStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	tokenID int,
	modelName string,
	clientIP string,
) (*model.Token, error) {
	if db == nil || userID <= 0 {
		return nil, ErrImageStudioTokenInvalid
	}
	if tokenID <= 0 {
		return nil, ErrImageStudioTokenRequired
	}
	user, err := getImageStudioUser(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if !imageStudioUserCanUseGroup(user) {
		return nil, ErrImageStudioTokenGroupUnavailable
	}
	var token model.Token
	if err := db.WithContext(ctx).First(&token, "id = ? AND user_id = ?", tokenID, userID).Error; err != nil {
		return nil, ErrImageStudioTokenInvalid
	}
	if !hasImageStudioTokenSignature(&token) || model.CheckUserTokenRecord(&token) != nil {
		return nil, ErrImageStudioTokenInvalid
	}
	if err := model.ValidateUserTokenIP(&token, clientIP); err != nil {
		if errors.Is(err, model.ErrTokenIPNotAllowed) || errors.Is(err, model.ErrTokenClientIPInvalid) {
			return nil, ErrImageStudioTokenIPForbidden
		}
		return nil, ErrImageStudioTokenInvalid
	}
	available, err := imageStudioModelAvailableForGroup(ctx, db, token.Group, modelName)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, ErrImageStudioTokenModelForbidden
	}
	return &token, nil
}

func findImageStudioToken(ctx context.Context, db *gorm.DB, userID int, clientIP string) (*model.Token, error) {
	if db == nil || userID <= 0 {
		return nil, ErrImageStudioTokenInvalid
	}
	var tokens []model.Token
	if err := db.WithContext(ctx).Where("user_id = ? AND name = ?", userID, imageStudioTokenName).Order("id ASC").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("list image studio tokens: %w", err)
	}
	for index := range tokens {
		if !hasImageStudioTokenSignature(&tokens[index]) || model.CheckUserTokenRecord(&tokens[index]) != nil {
			continue
		}
		if err := model.ValidateUserTokenIP(&tokens[index], clientIP); err == nil {
			return &tokens[index], nil
		}
	}
	return nil, nil
}

func getImageStudioUser(ctx context.Context, db *gorm.DB, userID int) (*model.User, error) {
	if db == nil || userID <= 0 {
		return nil, ErrImageStudioTokenInvalid
	}
	var user model.User
	if err := db.WithContext(ctx).Select("id", "group", "status").First(&user, "id = ?", userID).Error; err != nil {
		return nil, ErrImageStudioTokenInvalid
	}
	return &user, nil
}

func imageStudioUserCanUseGroup(user *model.User) bool {
	return user != nil && user.Status == common.UserStatusEnabled &&
		GroupInUserUsableGroups(strings.TrimSpace(user.Group), ImageStudioTokenGroup) &&
		ratio_setting.ContainsGroupRatio(ImageStudioTokenGroup)
}

func hasImageStudioTokenSignature(token *model.Token) bool {
	if token == nil || token.Name != imageStudioTokenName || token.Group != ImageStudioTokenGroup ||
		token.Status != common.TokenStatusEnabled || token.ExpiredTime != -1 || !token.UnlimitedQuota ||
		token.CrossGroupRetry || token.ModelLimitsEnabled {
		return false
	}
	return token.AllowIps == nil || *token.AllowIps == ""
}

func imageStudioTokenView(token *model.Token) *ImageStudioTokenView {
	if token == nil {
		return nil
	}
	return &ImageStudioTokenView{ID: token.Id, Name: token.Name, Group: token.Group}
}

func containsImageStudioModel(models []string, modelName string) bool {
	modelName = strings.TrimSpace(modelName)
	for _, available := range models {
		if available == modelName {
			return true
		}
	}
	return false
}

func isRetryableImageStudioTokenCreationError(err error) bool {
	if err == nil || !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") || strings.Contains(message, "database is deadlocked")
}
