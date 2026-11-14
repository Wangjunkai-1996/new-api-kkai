package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/shopspring/decimal"

	"gorm.io/gorm"
)

var ErrInvalidVideoStudioSubmission = errors.New("invalid video studio submission")
var ErrVideoStudioQuoteMismatch = errors.New("video studio quote does not match the normalized request")
var ErrVideoStudioQuoteExpired = errors.New("video studio quote has expired")

type VideoStudioValidationIssue struct {
	Field    string `json:"field"`
	Rule     string `json:"rule"`
	Role     string `json:"role,omitempty"`
	AssetID  int64  `json:"asset_id,omitempty"`
	Position *int   `json:"position,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Limit    any    `json:"limit,omitempty"`
}

type VideoStudioValidationError struct {
	Code       string                       `json:"code"`
	Message    string                       `json:"message"`
	StatusCode int                          `json:"-"`
	Issues     []VideoStudioValidationIssue `json:"issues,omitempty"`
}

func (e *VideoStudioValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func videoStudioValidationError(code string, status int, message string) error {
	return &VideoStudioValidationError{Code: code, StatusCode: status, Message: message}
}

type VideoStudioReferenceAssetInput struct {
	requestKey string
	AssetID    int64  `json:"asset_id"`
	Role       string `json:"role"`
	Position   *int   `json:"position"`
}

type VideoStudioSubmissionRequest struct {
	LegacyAPI3           bool                             `json:"-"`
	TokenID              int                              `json:"token_id"`
	Model                string                           `json:"model"`
	SpecificationVersion int                              `json:"specification_version"`
	Group                string                           `json:"group,omitempty"`
	Mode                 string                           `json:"mode"`
	Prompt               string                           `json:"prompt"`
	Parameters           map[string]any                   `json:"parameters"`
	ReferenceAssets      []VideoStudioReferenceAssetInput `json:"reference_assets,omitempty"`
	SampleID             *int64                           `json:"sample_id,omitempty"`
	MaxQuota             *int                             `json:"max_quota,omitempty"`
	QuoteHash            string                           `json:"quote_hash,omitempty"`
	QuoteExpiresAt       int64                            `json:"quote_expires_at,omitempty"`
}

type NormalizedVideoStudioSubmission struct {
	LegacyAPI3           bool
	UserID               int
	TokenID              int
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
	Position   int
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
	if request.LegacyAPI3 {
		return ValidateVideoStudioSubmitRequestAPI3(request)
	}
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.TokenID <= 0 || request.SpecificationVersion <= 0 || request.MaxQuota == nil || *request.MaxQuota < 0 || request.QuoteHash == "" ||
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
	if request.LegacyAPI3 {
		return NormalizeVideoStudioSubmissionAPI3(ctx, db, store, userID, request)
	}
	if db == nil || store == nil || userID <= 0 {
		return nil, ErrInvalidVideoStudioSubmission
	}
	request.Model = strings.TrimSpace(request.Model)
	request.Group = strings.TrimSpace(request.Group)
	request.Mode = strings.TrimSpace(request.Mode)
	request.Prompt = strings.TrimSpace(request.Prompt)
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.SpecificationVersion <= 0 || len(request.Group) > 64 || (request.MaxQuota != nil && *request.MaxQuota < 0) || !validVideoQuoteHash(request.QuoteHash) {
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
	}
	if request.Prompt == "" || len(request.Prompt) > 8000 {
		return nil, ErrInvalidVideoStudioSubmission
	}

	profileID, specificationVersion, resolvedModel, specification, defaults, err := resolveVideoModelProfile(ctx, db, request.Model)
	if err != nil {
		return nil, err
	}
	if sample != nil && sample.ModelProfileID != profileID {
		return nil, ErrInvalidVideoStudioSubmission
	}
	if request.SpecificationVersion != specificationVersion {
		return nil, videoStudioValidationError("video_capabilities_stale", 409, "the selected model capabilities have changed; refresh and retry")
	}
	modeDefaults := filterVideoParametersForMode(specification, request.Mode, defaults)
	mergedParameters := make(map[string]any, len(modeDefaults)+len(request.Parameters))
	for key, value := range modeDefaults {
		mergedParameters[key] = value
	}
	for key, value := range request.Parameters {
		mergedParameters[key] = value
	}
	parameters, err := ValidateVideoParameters(specification, request.Mode, mergedParameters, true)
	if err != nil {
		return nil, err
	}
	references, err := normalizeVideoReferences(ctx, db, store, userID, specification, request.Mode, parameters, request.ReferenceAssets)
	if err != nil {
		return nil, err
	}

	normalized := &NormalizedVideoStudioSubmission{
		UserID: userID, TokenID: request.TokenID, ProfileID: profileID, SpecificationVersion: specificationVersion,
		Model: resolvedModel, Group: request.Group, Mode: request.Mode,
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
	if normalized != nil && normalized.LegacyAPI3 {
		return ApplyVideoStudioEffectiveGroupAPI3(normalized, group)
	}
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
		AssetID  int64  `json:"asset_id"`
		Role     string `json:"role"`
		Position int    `json:"position"`
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
			AssetID: reference.Asset.ID, Role: reference.Role, Position: reference.Position,
		})
	}
	canonical := struct {
		TokenID              int                  `json:"token_id"`
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
		TokenID: normalized.TokenID, ProfileID: normalized.ProfileID, SpecificationVersion: normalized.SpecificationVersion,
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
	ctx context.Context, db *gorm.DB, store VideoAssetStore, userID int,
	specification VideoModelSpec, mode string, parameters map[string]any,
	inputs []VideoStudioReferenceAssetInput,
) ([]NormalizedVideoReferenceAsset, error) {
	if _, err := validateVideoReferenceCounts(specification, mode, inputs); err != nil {
		return nil, err
	}
	assets := make(map[int64]model.KKAIVideoAsset, len(inputs))
	for _, input := range inputs {
		asset, err := GetAuthorizedVideoAsset(ctx, db, userID, false, input.AssetID)
		if errors.Is(err, ErrVideoAssetAccessDenied) {
			return nil, err
		}
		if err != nil {
			return nil, videoStudioValidationError("video_reference_unavailable", 409, "a reference asset is unavailable")
		}
		assets[input.AssetID] = *asset
	}
	references, err := validateVideoReferenceAssets(specification, mode, parameters, inputs, assets)
	if err != nil {
		return nil, err
	}
	settings := video_studio_setting.Get()
	for index := range references {
		asset := references[index].Asset
		signedURL, err := store.PresignDownload(ctx, asset.ObjectKey, asset.OriginalFilename, false, time.Duration(settings.SignedURLSeconds)*time.Second)
		if err != nil {
			return nil, err
		}
		references[index].SignedURL = signedURL
	}
	return references, nil
}

// validateVideoReferenceAssets is shared by submission and sample authoring so
// a saved sample cannot bypass the model's media or ordering contract.
func validateVideoReferenceAssets(spec VideoModelSpec, mode string, parameters map[string]any, inputs []VideoStudioReferenceAssetInput, assets map[int64]model.KKAIVideoAsset) ([]NormalizedVideoReferenceAsset, error) {
	counts, err := validateVideoReferenceCounts(spec, mode, inputs)
	if err != nil {
		return nil, err
	}
	if mode == VideoModeTextToVideo {
		return []NormalizedVideoReferenceAsset{}, nil
	}
	policy := spec.ReferencePolicy
	positions := map[string]map[int]bool{}
	seenAssets := map[int64]bool{}
	references := make([]NormalizedVideoReferenceAsset, 0, len(inputs))
	durations := map[string]decimal.Decimal{}
	for _, input := range inputs {
		if input.AssetID <= 0 || input.Position == nil || *input.Position < 0 {
			return nil, videoStudioValidationError("video_request_contract_invalid", 400, "asset_id, role and position are required")
		}
		role := input.Role
		advanced := mode == VideoModeFirstFrameToVideo || mode == VideoModeFirstLastFrame
		if seenAssets[input.AssetID] && !advanced {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "an asset cannot be repeated")
		}
		seenAssets[input.AssetID] = true
		if positions[role] == nil {
			positions[role] = map[int]bool{}
		}
		if positions[role][*input.Position] {
			return nil, videoStudioValidationError("video_request_contract_invalid", 400, "reference positions must be unique")
		}
		positions[role][*input.Position] = true
		mediaPolicy, kind := policy.Image, "image"
		if role == model.VideoTaskAssetRoleReferenceVideo {
			mediaPolicy, kind = policy.Video, "video"
		}
		if role == model.VideoTaskAssetRoleReferenceAudio {
			mediaPolicy, kind = policy.Audio, "audio"
		}
		asset, exists := assets[input.AssetID]
		if !exists || asset.State != model.VideoAssetStateReady || asset.Kind != model.VideoAssetKindReference || asset.DeletedAt != 0 {
			return nil, videoStudioValidationError("video_reference_unavailable", 409, "a reference asset is unavailable")
		}
		if !strings.HasPrefix(asset.MIMEType, kind+"/") || !videoReferenceMIMEAllowed(asset.MIMEType, mediaPolicy.AllowedMIMETypes) {
			return nil, videoReferenceLimitError(role, &input, "allowed_mime_types", asset.MIMEType, mediaPolicy.AllowedMIMETypes)
		}
		if asset.SizeBytes <= 0 || asset.SizeBytes > mediaPolicy.MaxSizeBytes {
			return nil, videoReferenceLimitError(role, &input, "max_size_bytes", asset.SizeBytes, mediaPolicy.MaxSizeBytes)
		}
		inspection, err := DecodeVideoMediaInspection(asset)
		if err != nil || inspection.InspectionVersion != VideoMediaInspectionVersion {
			return nil, videoStudioValidationError("video_reference_unavailable", 409, "reference media inspection is required")
		}
		if kind == "image" {
			if asset.Width < mediaPolicy.MinWidth || asset.Width > mediaPolicy.MaxWidth {
				return nil, videoReferenceLimitError(role, &input, "width", asset.Width, []int{mediaPolicy.MinWidth, mediaPolicy.MaxWidth})
			}
			if asset.Height < mediaPolicy.MinHeight || asset.Height > mediaPolicy.MaxHeight {
				return nil, videoReferenceLimitError(role, &input, "height", asset.Height, []int{mediaPolicy.MinHeight, mediaPolicy.MaxHeight})
			}
			ratio := float64(asset.Width) / float64(asset.Height)
			if ratio <= mediaPolicy.MinAspectRatioExclusive || ratio >= mediaPolicy.MaxAspectRatioExclusive {
				return nil, videoReferenceLimitError(role, &input, "aspect_ratio_exclusive", ratio, []float64{mediaPolicy.MinAspectRatioExclusive, mediaPolicy.MaxAspectRatioExclusive})
			}
		} else {
			if !isFinite(asset.DurationSeconds) || asset.DurationSeconds <= 0 || asset.DurationSeconds < mediaPolicy.MinDurationSeconds || asset.DurationSeconds > mediaPolicy.MaxDurationSeconds {
				return nil, videoReferenceLimitError(role, &input, "duration_seconds", asset.DurationSeconds, []float64{mediaPolicy.MinDurationSeconds, mediaPolicy.MaxDurationSeconds})
			}
			durations[role] = durations[role].Add(decimal.NewFromFloat(asset.DurationSeconds))
			if durations[role].GreaterThan(decimal.NewFromFloat(mediaPolicy.MaxTotalDurationSeconds)) {
				totalDuration, _ := durations[role].Float64()
				return nil, videoReferenceLimitError(role, &input, "max_total_duration_seconds", totalDuration, mediaPolicy.MaxTotalDurationSeconds)
			}
			if kind == "video" {
				if inspection.FrameRateNum <= 0 || inspection.FrameRateDen <= 0 {
					return nil, videoStudioValidationError("video_reference_unavailable", 409, "video frame rate inspection is required")
				}
				fps := float64(inspection.FrameRateNum) / float64(inspection.FrameRateDen)
				if fps < mediaPolicy.MinFrameRate || fps > mediaPolicy.MaxFrameRate {
					return nil, videoReferenceLimitError(role, &input, "frame_rate", fps, []float64{mediaPolicy.MinFrameRate, mediaPolicy.MaxFrameRate})
				}
			}
		}
		references = append(references, NormalizedVideoReferenceAsset{Role: role, Position: *input.Position, RequestKey: role, Asset: asset})
	}
	for role, rolePositions := range positions {
		for position := range rolePositions {
			if position >= counts[role] {
				return nil, videoStudioValidationError("video_request_contract_invalid", 400, "reference positions must be contiguous from zero")
			}
		}
	}
	switch mode {
	case VideoModeReferenceToVideo:
		if counts[model.VideoTaskAssetRoleReferenceImage]+counts[model.VideoTaskAssetRoleReferenceVideo] == 0 || (policy.RequiresVideo && counts[model.VideoTaskAssetRoleReferenceVideo] == 0) {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "the model requires a visual or video reference")
		}
		if counts[model.VideoTaskAssetRoleReferenceAudio] > 0 && policy.AudioRequiresGenerateAudio {
			if generateAudio, ok := parameters["generate_audio"].(bool); !ok || !generateAudio {
				return nil, videoStudioValidationError("video_audio_output_required", 400, "audio references require generated audio")
			}
		}
	case VideoModeFirstFrameToVideo:
		if counts[model.VideoTaskAssetRoleFirstFrame] != 1 {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "one first frame is required")
		}
	case VideoModeFirstLastFrame:
		if counts[model.VideoTaskAssetRoleFirstFrame] != 1 || counts[model.VideoTaskAssetRoleLastFrame] != 1 {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "one first frame and one last frame are required")
		}
	}
	sort.SliceStable(references, func(i, j int) bool {
		if references[i].Role != references[j].Role {
			return referenceRoleOrder(references[i].Role) < referenceRoleOrder(references[j].Role)
		}
		return references[i].Position < references[j].Position
	})
	return references, nil
}

func videoReferenceLimitError(role string, input *VideoStudioReferenceAssetInput, rule string, actual, limit any) error {
	issue := VideoStudioValidationIssue{Field: "reference_assets", Rule: rule, Role: role, Actual: actual, Limit: limit}
	if input != nil {
		issue.AssetID, issue.Position = input.AssetID, input.Position
	}
	return &VideoStudioValidationError{Code: "video_reference_limit_exceeded", StatusCode: 400, Message: "a reference exceeds the model limits", Issues: []VideoStudioValidationIssue{issue}}
}

// Reject unsupported roles and per-media count overflows before asset lookups.
func validateVideoReferenceCounts(spec VideoModelSpec, mode string, inputs []VideoStudioReferenceAssetInput) (map[string]int, error) {
	if !containsVideoMode(spec.Modes, mode) || !isVideoMode(mode) {
		return nil, videoStudioValidationError("video_request_contract_invalid", 400, "unsupported video mode")
	}
	policy := spec.ReferencePolicy
	if mode == VideoModeTextToVideo {
		if len(inputs) != 0 || (policy != nil && policy.RequiresVideo) {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "this mode requires a different reference selection")
		}
		return nil, nil
	}
	if policy == nil {
		return nil, videoStudioValidationError("video_model_capability_unavailable", 503, "reference capabilities are not available")
	}
	if len(inputs) == 0 {
		return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "reference assets are required")
	}
	counts := map[string]int{}
	imageCount := 0
	advanced := mode == VideoModeFirstFrameToVideo || mode == VideoModeFirstLastFrame
	for _, input := range inputs {
		role := input.Role
		if advanced {
			if role != model.VideoTaskAssetRoleFirstFrame && !(mode == VideoModeFirstLastFrame && role == model.VideoTaskAssetRoleLastFrame) {
				return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "frame controls cannot be mixed with ordinary references")
			}
		} else if role != model.VideoTaskAssetRoleReferenceImage && role != model.VideoTaskAssetRoleReferenceVideo && role != model.VideoTaskAssetRoleReferenceAudio {
			return nil, videoStudioValidationError("video_request_contract_invalid", 400, "unsupported reference role")
		}
		counts[role]++
		mediaPolicy, count := policy.Image, counts[role]
		switch role {
		case model.VideoTaskAssetRoleReferenceVideo:
			mediaPolicy = policy.Video
		case model.VideoTaskAssetRoleReferenceAudio:
			mediaPolicy = policy.Audio
		default:
			imageCount++
			count = imageCount
		}
		if mediaPolicy == nil {
			return nil, videoStudioValidationError("video_reference_combination_invalid", 400, "this reference type is not supported")
		}
		if count > mediaPolicy.MaxCount {
			return nil, videoReferenceLimitError(role, nil, "max_count", count, mediaPolicy.MaxCount)
		}
		if advanced && counts[role] > 1 {
			return nil, videoReferenceLimitError(role, nil, "max_count", counts[role], 1)
		}
	}
	return counts, nil
}

func videoReferenceMIMEAllowed(mimeType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), mimeType) {
			return true
		}
	}
	return false
}

func referenceRoleOrder(role string) int {
	switch role {
	case model.VideoTaskAssetRoleReferenceImage, model.VideoTaskAssetRoleFirstFrame:
		return 0
	case model.VideoTaskAssetRoleReferenceVideo, model.VideoTaskAssetRoleLastFrame:
		return 1
	case model.VideoTaskAssetRoleReferenceAudio:
		return 2
	}
	return 9
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
	referenceViews := make([]map[string]any, 0, len(references))
	for _, reference := range references {
		referenceViews = append(referenceViews, map[string]any{"role": reference.Role, "position": reference.Position, "url": reference.SignedURL})
	}
	if len(referenceViews) > 0 {
		metadata["reference_assets"] = referenceViews
	}
	payload["metadata"] = metadata
	return common.Marshal(payload)
}
