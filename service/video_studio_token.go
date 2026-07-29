package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

const VideoStudioTokenGroup = "Seedance 视频"

const videoStudioTokenName = "视频工作室"

type VideoStudioTokenStatus string

const (
	VideoStudioTokenStatusReady             VideoStudioTokenStatus = "ready"
	VideoStudioTokenStatusMissing           VideoStudioTokenStatus = "missing"
	VideoStudioTokenStatusGroupUnavailable  VideoStudioTokenStatus = "group_unavailable"
	VideoStudioTokenStatusLimitReached      VideoStudioTokenStatus = "limit_reached"
	VideoStudioTokenStatusModelsUnavailable VideoStudioTokenStatus = "models_unavailable"
)

var (
	ErrVideoStudioTokenRequired          = errors.New("video studio token is required")
	ErrVideoStudioTokenInvalid           = errors.New("video studio token is invalid")
	ErrVideoStudioTokenGroupInvalid      = errors.New("video studio token must use the Seedance video group")
	ErrVideoStudioTokenModelForbidden    = errors.New("video studio token does not allow this model")
	ErrVideoStudioTokenGroupUnavailable  = errors.New("Seedance video group is not available for this account")
	ErrVideoStudioTokenLimitReached      = errors.New("video studio token cannot be created because the token limit was reached")
	ErrVideoStudioTokenModelsUnavailable = errors.New("no enabled video studio models are available")
	ErrVideoStudioTokenIPForbidden       = errors.New("video studio token cannot be used from this IP address")
	videoStudioTokenCacheInvalidator     = model.InvalidateUserTokensCache
)

type VideoStudioTokenView struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type VideoStudioTokenCapability struct {
	RequiredGroup   string                 `json:"required_group"`
	HasUsableToken  bool                   `json:"has_usable_token"`
	CanCreate       bool                   `json:"can_create"`
	EffectiveModels []string               `json:"effective_models"`
	Status          VideoStudioTokenStatus `json:"status"`
	Token           *VideoStudioTokenView  `json:"token,omitempty"`
}

type VideoStudioTokenEnsureResult struct {
	VideoStudioTokenCapability
	Created bool `json:"created"`
}

func GetVideoStudioTokenStatus(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (VideoStudioTokenCapability, error) {
	capability := VideoStudioTokenCapability{
		RequiredGroup:   VideoStudioTokenGroup,
		EffectiveModels: []string{},
		Status:          VideoStudioTokenStatusMissing,
	}
	if db == nil || userID <= 0 {
		return capability, ErrVideoStudioTokenInvalid
	}
	user, err := getCurrentVideoStudioUser(ctx, db, userID)
	if err != nil {
		return capability, err
	}
	if !videoStudioUserCanUseGroup(user) {
		capability.Status = VideoStudioTokenStatusGroupUnavailable
		return capability, nil
	}
	token, migrated, err := findUsableVideoStudioToken(ctx, db, userID, "", clientIP)
	invalidateMigratedVideoStudioTokenCache(userID, migrated)
	if err != nil {
		return capability, err
	}
	modelName = strings.TrimSpace(modelName)
	if token == nil {
		effectiveModels, err := enabledVideoStudioTokenModels(ctx, db)
		if err != nil {
			return capability, err
		}
		capability.EffectiveModels = effectiveModels
		if modelName != "" && !containsVideoStudioModel(effectiveModels, modelName) {
			capability.Status = VideoStudioTokenStatusModelsUnavailable
			return capability, nil
		}
		creationStatus, _, err := videoStudioTokenCreationState(ctx, db, userID, modelName)
		if err != nil {
			return capability, err
		}
		capability.CanCreate = creationStatus == VideoStudioTokenStatusMissing
		capability.Status = creationStatus
		return capability, nil
	}

	effectiveModels, err := effectiveVideoStudioModelsForTokenRecord(ctx, db, token)
	if err != nil {
		return capability, err
	}
	capability.EffectiveModels = effectiveModels
	capability.HasUsableToken = true
	capability.Token = videoStudioTokenView(token)
	if modelName != "" && !containsVideoStudioModel(effectiveModels, modelName) {
		capability.Status = VideoStudioTokenStatusModelsUnavailable
		return capability, nil
	}
	capability.Status = VideoStudioTokenStatusReady
	return capability, nil
}

func ValidateVideoStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	tokenID int,
	modelName string,
	clientIP string,
) (*model.Token, error) {
	if tokenID <= 0 {
		return nil, ErrVideoStudioTokenRequired
	}
	if db == nil || userID <= 0 {
		return nil, ErrVideoStudioTokenInvalid
	}
	user, err := getCurrentVideoStudioUser(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if !videoStudioUserCanUseGroup(user) {
		return nil, ErrVideoStudioTokenGroupUnavailable
	}
	modelName = strings.TrimSpace(modelName)

	var token model.Token
	if err := db.WithContext(ctx).First(&token, "id = ? AND user_id = ?", tokenID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoStudioTokenInvalid
		}
		return nil, fmt.Errorf("get video studio token: %w", err)
	}
	if err := model.CheckUserTokenRecord(&token); err != nil {
		return nil, ErrVideoStudioTokenInvalid
	}
	if token.Group != VideoStudioTokenGroup {
		return nil, ErrVideoStudioTokenGroupInvalid
	}
	if err := validateVideoStudioTokenIP(&token, clientIP); err != nil {
		return nil, err
	}
	migrated, err := repairLegacyVideoStudioTokenLimits(ctx, db, &token)
	invalidateMigratedVideoStudioTokenCache(userID, migrated)
	if err != nil {
		return nil, err
	}
	if err := model.CheckUserTokenRecord(&token); err != nil {
		return nil, ErrVideoStudioTokenInvalid
	}
	if token.Group != VideoStudioTokenGroup {
		return nil, ErrVideoStudioTokenGroupInvalid
	}
	if err := validateVideoStudioTokenIP(&token, clientIP); err != nil {
		return nil, err
	}
	if !videoStudioTokenAllowsModel(&token, modelName) {
		return nil, ErrVideoStudioTokenModelForbidden
	}
	return &token, nil
}

func ValidateVideoStudioModelAvailability(ctx context.Context, db *gorm.DB, modelName string) error {
	available, err := videoStudioModelAvailableForGroup(ctx, db, VideoStudioTokenGroup, strings.TrimSpace(modelName))
	if err != nil {
		return err
	}
	if !available {
		return ErrVideoStudioTokenModelForbidden
	}
	return nil
}

func findUsableVideoStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (*model.Token, bool, error) {
	tokens, migrated, err := listUsableVideoStudioTokens(ctx, db, userID, clientIP)
	if err != nil {
		return nil, migrated, err
	}
	for index := range tokens {
		if videoStudioTokenAllowsModel(&tokens[index], modelName) {
			return &tokens[index], migrated, nil
		}
	}
	return nil, migrated, nil
}

func listUsableVideoStudioTokens(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	clientIP string,
) ([]model.Token, bool, error) {
	var tokens []model.Token
	if err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").Find(&tokens).Error; err != nil {
		return nil, false, fmt.Errorf("list video studio tokens: %w", err)
	}
	migrated := false
	for index := range tokens {
		if hasVideoStudioManagedTokenSignature(&tokens[index]) {
			repaired, err := repairLegacyVideoStudioTokenLimits(ctx, db, &tokens[index])
			migrated = migrated || repaired
			if err != nil {
				return nil, migrated, err
			}
		}
	}
	usable := make([]model.Token, 0, len(tokens))
	for pass := 0; pass < 2; pass++ {
		for index := range tokens {
			token := &tokens[index]
			isSystemToken := hasVideoStudioManagedTokenSignature(token) && !token.ModelLimitsEnabled
			if (pass == 0) != isSystemToken || token.Group != VideoStudioTokenGroup || model.CheckUserTokenRecord(token) != nil {
				continue
			}
			if err := model.ValidateUserTokenIP(token, clientIP); err != nil {
				if errors.Is(err, model.ErrTokenIPNotAllowed) {
					continue
				}
				return nil, migrated, validateVideoStudioTokenIPError(err)
			}
			usable = append(usable, *token)
		}
	}
	return usable, migrated, nil
}

func repairLegacyVideoStudioTokenLimits(ctx context.Context, db *gorm.DB, token *model.Token) (bool, error) {
	if token == nil || !hasVideoStudioManagedTokenSignature(token) || !token.ModelLimitsEnabled {
		return false, nil
	}
	legacyModels, err := legacyVideoStudioProfileSnapshot(ctx, db)
	if err != nil {
		return false, err
	}
	if len(legacyModels) == 0 || token.ModelLimits != strings.Join(legacyModels, ",") {
		return false, nil
	}
	expectedLimits := token.ModelLimits
	result := db.WithContext(ctx).Model(&model.Token{}).
		Where(map[string]any{
			"id":                   token.Id,
			"user_id":              token.UserId,
			"name":                 videoStudioTokenName,
			"status":               common.TokenStatusEnabled,
			"expired_time":         int64(-1),
			"unlimited_quota":      true,
			"model_limits_enabled": true,
			"model_limits":         expectedLimits,
			"group":                VideoStudioTokenGroup,
			"cross_group_retry":    false,
		}).
		Where("(allow_ips IS NULL OR allow_ips = '')").
		Updates(map[string]any{
			"model_limits_enabled": false,
			"model_limits":         "",
		})
	if result.Error != nil {
		return false, fmt.Errorf("repair legacy video studio token: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		if err := db.WithContext(ctx).First(token, "id = ? AND user_id = ?", token.Id, token.UserId).Error; err != nil {
			return false, fmt.Errorf("reload video studio token after concurrent update: %w", err)
		}
		return false, nil
	}
	token.ModelLimitsEnabled = false
	token.ModelLimits = ""
	return true, nil
}

func invalidateMigratedVideoStudioTokenCache(userID int, migrated bool) {
	if !migrated {
		return
	}
	if err := videoStudioTokenCacheInvalidator(userID); err != nil {
		common.SysLog("failed to invalidate repaired video studio token cache: " + err.Error())
	}
}

func hasVideoStudioManagedTokenSignature(token *model.Token) bool {
	if token == nil || token.Name != videoStudioTokenName || token.Group != VideoStudioTokenGroup ||
		token.Status != common.TokenStatusEnabled || token.ExpiredTime != -1 || !token.UnlimitedQuota || token.CrossGroupRetry {
		return false
	}
	return token.AllowIps == nil || *token.AllowIps == ""
}

func legacyVideoStudioProfileSnapshot(ctx context.Context, db *gorm.DB) ([]string, error) {
	var models []string
	if err := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
		Where("enabled = ?", true).
		Distinct("model").
		Pluck("model", &models).Error; err != nil {
		return nil, fmt.Errorf("list legacy video studio profile snapshot: %w", err)
	}
	sort.Strings(models)
	return models, nil
}

func getCurrentVideoStudioUser(ctx context.Context, db *gorm.DB, userID int) (*model.User, error) {
	var user model.User
	err := db.WithContext(ctx).
		Select("id", "group", "status").
		First(&user, "id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoStudioTokenInvalid
		}
		return nil, fmt.Errorf("get video studio user: %w", err)
	}
	return &user, nil
}

func videoStudioUserCanUseGroup(user *model.User) bool {
	return user != nil && user.Status == common.UserStatusEnabled && videoStudioTokenGroupAvailable(user.Group)
}

func videoStudioTokenGroupAvailable(userGroup string) bool {
	return GroupInUserUsableGroups(strings.TrimSpace(userGroup), VideoStudioTokenGroup) &&
		ratio_setting.ContainsGroupRatio(VideoStudioTokenGroup)
}

func videoStudioTokenAllowsModel(token *model.Token, modelName string) bool {
	if token == nil || modelName == "" || !token.ModelLimitsEnabled {
		return token != nil
	}
	_, allowed := token.GetModelLimitsMap()[ratio_setting.FormatMatchingModelName(modelName)]
	return allowed
}

func videoStudioModelAvailableForGroup(ctx context.Context, db *gorm.DB, group string, modelName string) (bool, error) {
	if modelName == "" {
		return true, nil
	}
	var count int64
	err := db.WithContext(ctx).Model(&model.Ability{}).
		Where(&model.Ability{Group: strings.TrimSpace(group), Model: modelName, Enabled: true}).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check video studio model availability: %w", err)
	}
	return count > 0, nil
}

func effectiveVideoStudioModelsForToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	tokenID int,
	clientIP string,
) ([]string, error) {
	token, err := ValidateVideoStudioToken(ctx, db, userID, tokenID, "", clientIP)
	if err != nil {
		return nil, err
	}
	return effectiveVideoStudioModelsForTokenRecord(ctx, db, token)
}

func effectiveVideoStudioModelsForTokenRecord(ctx context.Context, db *gorm.DB, token *model.Token) ([]string, error) {
	if db == nil || token == nil {
		return nil, ErrVideoStudioTokenInvalid
	}
	models, err := enabledVideoStudioModelsForGroup(ctx, db, token.Group)
	if err != nil || !token.ModelLimitsEnabled {
		return models, err
	}
	filtered := make([]string, 0, len(models))
	for _, modelName := range models {
		if videoStudioTokenAllowsModel(token, modelName) {
			filtered = append(filtered, modelName)
		}
	}
	return filtered, nil
}

func videoStudioTokenView(token *model.Token) *VideoStudioTokenView {
	if token == nil {
		return nil
	}
	return &VideoStudioTokenView{ID: token.Id, Name: token.Name, Group: token.Group}
}

func validateVideoStudioTokenIP(token *model.Token, clientIP string) error {
	err := model.ValidateUserTokenIP(token, clientIP)
	if err == nil {
		return nil
	}
	return validateVideoStudioTokenIPError(err)
}

func validateVideoStudioTokenIPError(err error) error {
	if errors.Is(err, model.ErrTokenIPNotAllowed) || errors.Is(err, model.ErrTokenClientIPInvalid) {
		return ErrVideoStudioTokenIPForbidden
	}
	return ErrVideoStudioTokenInvalid
}
