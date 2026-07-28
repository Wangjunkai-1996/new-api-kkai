package service

import (
	"context"
	"errors"
	"fmt"
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
)

type VideoStudioTokenView struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type VideoStudioTokenCapability struct {
	RequiredGroup  string                 `json:"required_group"`
	HasUsableToken bool                   `json:"has_usable_token"`
	CanCreate      bool                   `json:"can_create"`
	Status         VideoStudioTokenStatus `json:"status"`
	Token          *VideoStudioTokenView  `json:"token,omitempty"`
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
		RequiredGroup: VideoStudioTokenGroup,
		Status:        VideoStudioTokenStatusMissing,
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

	modelName = strings.TrimSpace(modelName)
	modelAvailable, err := videoStudioModelAvailable(ctx, db, modelName)
	if err != nil {
		return capability, err
	}
	if !modelAvailable {
		capability.Status = VideoStudioTokenStatusModelsUnavailable
		return capability, nil
	}
	token, err := findUsableVideoStudioToken(ctx, db, userID, modelName, clientIP)
	if err != nil {
		return capability, err
	}
	creationStatus, _, err := videoStudioTokenCreationState(ctx, db, userID, modelName)
	if err != nil {
		return capability, err
	}
	capability.CanCreate = creationStatus == VideoStudioTokenStatusMissing
	if token == nil {
		capability.Status = creationStatus
		return capability, nil
	}
	capability.HasUsableToken = true
	capability.Status = VideoStudioTokenStatusReady
	capability.Token = videoStudioTokenView(token)
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
	if !videoStudioTokenAllowsModel(&token, modelName) {
		return nil, ErrVideoStudioTokenModelForbidden
	}
	if err := validateVideoStudioTokenIP(&token, clientIP); err != nil {
		return nil, err
	}
	return &token, nil
}

func findUsableVideoStudioToken(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	modelName string,
	clientIP string,
) (*model.Token, error) {
	var tokens []model.Token
	if err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("list video studio tokens: %w", err)
	}
	for index := range tokens {
		token := &tokens[index]
		if token.Group != VideoStudioTokenGroup || model.CheckUserTokenRecord(token) != nil ||
			!videoStudioTokenAllowsModel(token, modelName) {
			continue
		}
		if err := model.ValidateUserTokenIP(token, clientIP); err != nil {
			if errors.Is(err, model.ErrTokenIPNotAllowed) {
				continue
			}
			return nil, validateVideoStudioTokenIPError(err)
		}
		return token, nil
	}
	return nil, nil
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

func videoStudioModelAvailable(ctx context.Context, db *gorm.DB, modelName string) (bool, error) {
	if modelName == "" {
		return true, nil
	}
	var count int64
	err := db.WithContext(ctx).Model(&model.KKAIVideoModelProfile{}).
		Where("enabled = ? AND model = ?", true, modelName).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check video studio model availability: %w", err)
	}
	return count > 0, nil
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
