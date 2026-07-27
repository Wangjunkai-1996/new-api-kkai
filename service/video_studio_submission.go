package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"gorm.io/gorm"
)

var ErrInvalidVideoStudioSubmission = errors.New("invalid video studio submission")
var ErrVideoStudioQuoteMismatch = errors.New("video studio quote does not match the normalized request")
var ErrVideoStudioQuoteExpired = errors.New("video studio quote has expired")

type VideoStudioReferenceAssetInput struct {
	AssetID    int64  `json:"asset_id"`
	Role       string `json:"role"`
	requestKey string
}

type VideoStudioSubmissionRequest struct {
	Model           string                           `json:"model"`
	Group           string                           `json:"group,omitempty"`
	Mode            string                           `json:"mode"`
	Prompt          string                           `json:"prompt"`
	Parameters      map[string]any                   `json:"parameters"`
	ReferenceAssets []VideoStudioReferenceAssetInput `json:"reference_assets,omitempty"`
	SampleID        *int64                           `json:"sample_id,omitempty"`
	MaxQuota        *int                             `json:"max_quota,omitempty"`
	QuoteHash       string                           `json:"quote_hash,omitempty"`
	QuoteExpiresAt  int64                            `json:"quote_expires_at,omitempty"`
}

type NormalizedVideoStudioSubmission struct {
	UserID               int
	ProfileID            int64
	SpecificationVersion int
	Model                string
	Group                string
	Mode                 string
	Prompt               string
	Parameters           map[string]any
	ReferenceAssets      []NormalizedVideoReferenceAsset
	SampleID             *int64
	MaxQuota             *int
	QuoteHash            string
	QuoteExpiresAt       int64
	RequestHash          string
	TaskPayload          []byte
	specification        VideoModelSpec
}

type NormalizedVideoReferenceAsset struct {
	Role       string
	RequestKey string
	Asset      model.KKAIVideoAsset
	SignedURL  string
}

type VideoStudioQuote struct {
	Quota         int                `json:"quota"`
	DisplayAmount string             `json:"display_amount"`
	RequestHash   string             `json:"request_hash"`
	ExpiresAt     int64              `json:"expires_at"`
	OtherRatios   map[string]float64 `json:"other_ratios"`
}

func ValidateVideoStudioSubmitRequest(request VideoStudioSubmissionRequest) error {
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.MaxQuota == nil || *request.MaxQuota < 0 || request.QuoteHash == "" ||
		!validVideoQuoteHash(request.QuoteHash) || request.QuoteExpiresAt <= 0 {
		return ErrInvalidVideoStudioSubmission
	}
	return nil
}

func NormalizeVideoStudioSubmission(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	request VideoStudioSubmissionRequest,
) (*NormalizedVideoStudioSubmission, error) {
	if db == nil || store == nil || userID <= 0 {
		return nil, ErrInvalidVideoStudioSubmission
	}
	request.Model = strings.TrimSpace(request.Model)
	request.Group = strings.TrimSpace(request.Group)
	request.Mode = strings.TrimSpace(request.Mode)
	request.Prompt = strings.TrimSpace(request.Prompt)
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if len(request.Group) > 64 || (request.MaxQuota != nil && *request.MaxQuota < 0) || !validVideoQuoteHash(request.QuoteHash) {
		return nil, ErrInvalidVideoStudioSubmission
	}

	var sample *model.KKAIVideoSample
	if request.SampleID != nil {
		if *request.SampleID <= 0 {
			return nil, ErrInvalidVideoStudioSubmission
		}
		var found model.KKAIVideoSample
		if err := db.WithContext(ctx).First(&found, "id = ? AND status = ?", *request.SampleID, model.VideoSampleStatusPublished).Error; err != nil {
			return nil, ErrVideoSampleNotFound
		}
		sample = &found
		if request.Model == "" {
			var profile model.KKAIVideoModelProfile
			if err := db.WithContext(ctx).Select("model").First(&profile, "id = ?", found.ModelProfileID).Error; err != nil {
				return nil, ErrVideoModelProfileNotFound
			}
			request.Model = profile.Model
		}
		if request.Prompt == "" {
			request.Prompt = found.Prompt
		}
		if request.Mode == "" {
			request.Mode = found.Mode
		}
		if len(request.Parameters) == 0 {
			request.Parameters = map[string]any{}
			if err := common.UnmarshalJsonStr(found.Parameters, &request.Parameters); err != nil {
				return nil, fmt.Errorf("decode sample parameters: %w", err)
			}
		}
	}
	if request.Prompt == "" || len(request.Prompt) > 8000 {
		return nil, ErrInvalidVideoStudioSubmission
	}

	profile, specification, defaults, err := GetEnabledVideoModelProfileByModel(ctx, db, request.Model)
	if err != nil {
		return nil, err
	}
	if sample != nil && sample.ModelProfileID != profile.ID {
		return nil, ErrInvalidVideoStudioSubmission
	}
	if sample != nil && len(request.ReferenceAssets) == 0 {
		expectedReferences := expectedVideoReferenceInputs(specification, request.Mode)
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshots(sample.ReferenceAssetIDs, expectedReferences)
		if err != nil {
			return nil, fmt.Errorf("decode sample references: %w", err)
		}
		if !videoSampleReferenceSnapshotsMatch(referenceSnapshots, expectedReferences) {
			return nil, ErrInvalidVideoStudioSubmission
		}
		request.ReferenceAssets = sampleReferenceInputs(referenceSnapshots)
	}
	mergedParameters := make(map[string]any, len(defaults)+len(request.Parameters))
	for key, value := range defaults {
		mergedParameters[key] = value
	}
	for key, value := range request.Parameters {
		mergedParameters[key] = value
	}
	parameters, err := ValidateVideoParameters(specification, request.Mode, mergedParameters, true)
	if err != nil {
		return nil, err
	}
	references, err := normalizeVideoReferences(ctx, db, store, userID, specification, request.Mode, request.ReferenceAssets)
	if err != nil {
		return nil, err
	}

	normalized := &NormalizedVideoStudioSubmission{
		UserID: userID, ProfileID: profile.ID, SpecificationVersion: profile.SpecificationVersion,
		Model: profile.Model, Group: request.Group, Mode: request.Mode,
		Prompt: request.Prompt, Parameters: parameters, ReferenceAssets: references,
		SampleID: request.SampleID, MaxQuota: request.MaxQuota, QuoteHash: request.QuoteHash,
		QuoteExpiresAt: request.QuoteExpiresAt,
		specification:  specification,
	}
	if err := ApplyVideoStudioEffectiveGroup(normalized, request.Group); err != nil {
		return nil, err
	}
	return normalized, nil
}

func ApplyVideoStudioEffectiveGroup(normalized *NormalizedVideoStudioSubmission, group string) error {
	if normalized == nil || strings.TrimSpace(normalized.Model) == "" || normalized.SpecificationVersion <= 0 {
		return ErrInvalidVideoStudioSubmission
	}
	group = strings.TrimSpace(group)
	if len(group) > 64 {
		return ErrInvalidVideoStudioSubmission
	}
	taskPayload, err := buildVideoTaskPayload(
		normalized.Model, group, normalized.Mode, normalized.Prompt,
		normalized.Parameters, normalized.ReferenceAssets, normalized.specification,
	)
	if err != nil {
		return err
	}
	type canonicalParameter struct {
		Key        string `json:"key"`
		RequestKey string `json:"request_key"`
		Value      any    `json:"value"`
	}
	type canonicalReference struct {
		AssetID    int64  `json:"asset_id"`
		Role       string `json:"role"`
		RequestKey string `json:"request_key"`
	}
	parameterValues := make([]canonicalParameter, 0, len(normalized.Parameters))
	for _, parameter := range normalized.specification.Parameters {
		value, exists := normalized.Parameters[parameter.Key]
		if !exists {
			continue
		}
		requestKey := parameter.RequestKey
		if requestKey == "" {
			requestKey = parameter.Key
		}
		parameterValues = append(parameterValues, canonicalParameter{Key: parameter.Key, RequestKey: requestKey, Value: value})
	}
	referenceValues := make([]canonicalReference, 0, len(normalized.ReferenceAssets))
	for _, reference := range normalized.ReferenceAssets {
		referenceValues = append(referenceValues, canonicalReference{
			AssetID: reference.Asset.ID, Role: reference.Role, RequestKey: reference.RequestKey,
		})
	}
	canonical := struct {
		ProfileID            int64                `json:"profile_id"`
		SpecificationVersion int                  `json:"specification_version"`
		Model                string               `json:"model"`
		Group                string               `json:"group"`
		Mode                 string               `json:"mode"`
		Prompt               string               `json:"prompt"`
		Parameters           []canonicalParameter `json:"parameters"`
		References           []canonicalReference `json:"references,omitempty"`
		SampleID             *int64               `json:"sample_id,omitempty"`
	}{
		ProfileID: normalized.ProfileID, SpecificationVersion: normalized.SpecificationVersion,
		Model: normalized.Model, Group: group, Mode: normalized.Mode, Prompt: normalized.Prompt,
		Parameters: parameterValues, References: referenceValues, SampleID: normalized.SampleID,
	}
	canonicalJSON, err := common.Marshal(canonical)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonicalJSON)
	normalized.Group = group
	normalized.RequestHash = hex.EncodeToString(digest[:])
	normalized.TaskPayload = taskPayload
	return nil
}

func validVideoQuoteHash(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func normalizeVideoReferences(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	specification VideoModelSpec,
	mode string,
	inputs []VideoStudioReferenceAssetInput,
) ([]NormalizedVideoReferenceAsset, error) {
	requiredRoles := expectedVideoReferenceRoles(specification, mode)
	if len(inputs) != len(requiredRoles) {
		return nil, ErrInvalidVideoStudioSubmission
	}
	inputByRole := make(map[string]VideoStudioReferenceAssetInput, len(inputs))
	for _, input := range inputs {
		if input.AssetID <= 0 || input.Role == "" {
			return nil, ErrInvalidVideoStudioSubmission
		}
		if _, duplicate := inputByRole[input.Role]; duplicate {
			return nil, ErrInvalidVideoStudioSubmission
		}
		inputByRole[input.Role] = input
	}
	requestKeyByRole := make(map[string]string, len(specification.ReferenceInputs))
	for _, input := range specification.ReferenceInputs {
		requestKeyByRole[input.Role] = input.RequestKey
	}
	settings := video_studio_setting.Get()
	result := make([]NormalizedVideoReferenceAsset, 0, len(requiredRoles))
	for _, role := range requiredRoles {
		input, ok := inputByRole[role]
		if !ok {
			return nil, ErrInvalidVideoStudioSubmission
		}
		requestKey := requestKeyByRole[role]
		if input.requestKey != "" {
			if input.requestKey != requestKey {
				return nil, ErrInvalidVideoStudioSubmission
			}
			requestKey = input.requestKey
		}
		var asset model.KKAIVideoAsset
		if err := db.WithContext(ctx).First(&asset, "id = ?", input.AssetID).Error; err != nil {
			return nil, ErrInvalidVideoStudioSubmission
		}
		if asset.DeletedAt != 0 || asset.Kind != model.VideoAssetKindReference ||
			asset.State != model.VideoAssetStateReady || !strings.HasPrefix(asset.MIMEType, "image/") {
			return nil, ErrInvalidVideoStudioSubmission
		}
		if asset.Scope == model.VideoAssetScopeCatalog {
			published, err := isPublishedVideoCatalogAsset(ctx, db, asset.ID)
			if err != nil {
				return nil, err
			}
			if !published {
				return nil, ErrInvalidVideoStudioSubmission
			}
		} else if asset.Scope != model.VideoAssetScopeUser || asset.OwnerUserID != userID {
			return nil, ErrInvalidVideoStudioSubmission
		}
		signedURL, err := store.PresignDownload(ctx, asset.ObjectKey, asset.OriginalFilename, false, time.Duration(settings.SignedURLSeconds)*time.Second)
		if err != nil {
			return nil, err
		}
		result = append(result, NormalizedVideoReferenceAsset{
			Role: role, RequestKey: requestKey, Asset: asset, SignedURL: signedURL,
		})
	}
	return result, nil
}

func buildVideoTaskPayload(
	modelName string,
	group string,
	mode string,
	prompt string,
	parameters map[string]any,
	references []NormalizedVideoReferenceAsset,
	specification VideoModelSpec,
) ([]byte, error) {
	payload := map[string]any{"model": modelName, "mode": mode, "prompt": prompt}
	if group != "" {
		payload["group"] = group
	}
	metadata := make(map[string]any, len(parameters)+len(references))
	parameterSpecs := make(map[string]VideoParameterSpec, len(specification.Parameters))
	for _, parameter := range specification.Parameters {
		parameterSpecs[parameter.Key] = parameter
	}
	for key, value := range parameters {
		parameter := parameterSpecs[key]
		requestKey := parameter.RequestKey
		if requestKey == "" {
			requestKey = key
		}
		payload[requestKey] = value
		metadata[requestKey] = value
	}
	for _, reference := range references {
		payload[reference.RequestKey] = reference.SignedURL
		metadata[reference.RequestKey] = reference.SignedURL
	}
	if len(references) > 0 {
		images := make([]string, 0, len(references))
		for _, reference := range references {
			images = append(images, reference.SignedURL)
		}
		payload["images"] = images
		if len(images) == 1 {
			payload["image"] = images[0]
		}
	}
	payload["metadata"] = metadata
	return common.Marshal(payload)
}

func sampleReferenceInputs(snapshots []VideoSampleReferenceSnapshot) []VideoStudioReferenceAssetInput {
	result := make([]VideoStudioReferenceAssetInput, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, VideoStudioReferenceAssetInput{
			AssetID: snapshot.AssetID, Role: snapshot.Role, requestKey: snapshot.RequestKey,
		})
	}
	return result
}
