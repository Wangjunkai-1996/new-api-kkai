package service

// Frozen API 3 ingress is retained only for the adjacent rollout/rollback window.
// The new product uses API 4; both versions share task acceptance and settlement.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"gorm.io/gorm"
)

type VideoSampleReferenceSnapshotAPI3 struct {
	AssetID    int64  `json:"asset_id"`
	Role       string `json:"role"`
	RequestKey string `json:"request_key"`
}

func ValidateVideoStudioSubmitRequestAPI3(request VideoStudioSubmissionRequest) error {
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.TokenID <= 0 || request.MaxQuota == nil || *request.MaxQuota < 0 || request.QuoteHash == "" ||
		!validVideoQuoteHash(request.QuoteHash) || request.QuoteExpiresAt <= 0 {
		return ErrInvalidVideoStudioSubmission
	}
	return nil
}

func NormalizeVideoStudioSubmissionAPI3(
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

	profileID, specificationVersion, resolvedModel, specification, defaults, err := resolveVideoModelProfileAPI3(ctx, db, request.Model)
	if err != nil {
		return nil, err
	}
	if sample != nil && sample.ModelProfileID != profileID {
		return nil, ErrInvalidVideoStudioSubmission
	}
	if sample != nil && len(request.ReferenceAssets) == 0 {
		expectedReferences := expectedVideoReferenceInputsAPI3(specification, request.Mode)
		referenceSnapshots, err := decodeVideoSampleReferenceSnapshotsAPI3(sample.ReferenceAssetIDs, expectedReferences)
		if err != nil {
			return nil, fmt.Errorf("decode sample references: %w", err)
		}
		if !videoSampleReferenceSnapshotsMatchAPI3(referenceSnapshots, expectedReferences) {
			return nil, ErrInvalidVideoStudioSubmission
		}
		request.ReferenceAssets = sampleReferenceInputsAPI3(referenceSnapshots)
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
	references, err := normalizeVideoReferencesAPI3(ctx, db, store, userID, specification, request.Mode, request.ReferenceAssets)
	if err != nil {
		return nil, err
	}

	normalized := &NormalizedVideoStudioSubmission{
		LegacyAPI3: true, UserID: userID, TokenID: request.TokenID, ProfileID: profileID, SpecificationVersion: specificationVersion,
		Model: resolvedModel, Group: request.Group, Mode: request.Mode,
		Prompt: request.Prompt, Parameters: parameters, ReferenceAssets: references,
		SampleID: request.SampleID, MaxQuota: request.MaxQuota, QuoteHash: request.QuoteHash,
		QuoteExpiresAt: request.QuoteExpiresAt,
		specification:  specification,
	}
	if err := ApplyVideoStudioEffectiveGroupAPI3(normalized, request.Group); err != nil {
		return nil, err
	}
	return normalized, nil
}

func ApplyVideoStudioEffectiveGroupAPI3(normalized *NormalizedVideoStudioSubmission, group string) error {
	if normalized == nil || strings.TrimSpace(normalized.Model) == "" || normalized.SpecificationVersion <= 0 {
		return ErrInvalidVideoStudioSubmission
	}
	group = strings.TrimSpace(group)
	if len(group) > 64 {
		return ErrInvalidVideoStudioSubmission
	}
	taskPayload, err := buildVideoTaskPayloadAPI3(
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

func normalizeVideoReferencesAPI3(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	specification VideoModelSpec,
	mode string,
	inputs []VideoStudioReferenceAssetInput,
) ([]NormalizedVideoReferenceAsset, error) {
	requiredRoles := expectedVideoReferenceRolesAPI3(specification, mode)
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
		expectsVideo := role == model.VideoTaskAssetRoleReferenceVideo
		validMIME := strings.HasPrefix(asset.MIMEType, "image/")
		if expectsVideo {
			validMIME = strings.HasPrefix(asset.MIMEType, "video/")
		}
		if asset.DeletedAt != 0 || asset.Kind != model.VideoAssetKindReference ||
			asset.State != model.VideoAssetStateReady || !validMIME {
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
			Role: role, Position: len(result), RequestKey: requestKey, Asset: asset, SignedURL: signedURL,
		})
	}
	return result, nil
}

func buildVideoTaskPayloadAPI3(
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
	images := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Role != model.VideoTaskAssetRoleReferenceVideo {
			images = append(images, reference.SignedURL)
		}
	}
	if len(images) > 0 {
		payload["images"] = images
		if len(images) == 1 {
			payload["image"] = images[0]
		}
	}
	payload["metadata"] = metadata
	return common.Marshal(payload)
}

func sampleReferenceInputsAPI3(snapshots []VideoSampleReferenceSnapshotAPI3) []VideoStudioReferenceAssetInput {
	result := make([]VideoStudioReferenceAssetInput, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, VideoStudioReferenceAssetInput{
			AssetID: snapshot.AssetID, Role: snapshot.Role, requestKey: snapshot.RequestKey,
		})
	}
	return result
}

func VideoStudioIdempotencyFingerprintAPI3(request VideoStudioSubmissionRequest) (string, error) {
	type referenceFingerprint struct {
		AssetID int64  `json:"asset_id"`
		Role    string `json:"role"`
	}
	references := make([]referenceFingerprint, 0, len(request.ReferenceAssets))
	for _, reference := range request.ReferenceAssets {
		references = append(references, referenceFingerprint{
			AssetID: reference.AssetID,
			Role:    strings.TrimSpace(reference.Role),
		})
	}
	sort.Slice(references, func(left int, right int) bool {
		if references[left].Role == references[right].Role {
			return references[left].AssetID < references[right].AssetID
		}
		return references[left].Role < references[right].Role
	})
	parameters := request.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}
	canonical := struct {
		TokenID    int                    `json:"token_id"`
		Model      string                 `json:"model"`
		Group      string                 `json:"group"`
		Mode       string                 `json:"mode"`
		Prompt     string                 `json:"prompt"`
		Parameters map[string]any         `json:"parameters"`
		References []referenceFingerprint `json:"references"`
		SampleID   *int64                 `json:"sample_id,omitempty"`
	}{
		TokenID: request.TokenID, Model: strings.TrimSpace(request.Model), Group: strings.TrimSpace(request.Group),
		Mode: strings.TrimSpace(request.Mode), Prompt: strings.TrimSpace(request.Prompt),
		Parameters: parameters, References: references, SampleID: request.SampleID,
	}
	encoded, err := common.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode video studio idempotency fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateVideoModelSpecAPI3(spec VideoModelSpec, defaults map[string]any) error {
	if spec.ReferencePolicy != nil {
		return fmt.Errorf("%w: API3 does not support reference_policy", ErrInvalidVideoModelSpec)
	}
	if spec.Version <= 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidVideoModelSpec)
	}
	if len(spec.Modes) == 0 || len(spec.Modes) > 3 {
		return fmt.Errorf("%w: at least one supported mode is required", ErrInvalidVideoModelSpec)
	}
	modes := make(map[string]struct{}, len(spec.Modes))
	for _, mode := range spec.Modes {
		if !isVideoModeAPI3(mode) {
			return fmt.Errorf("%w: unsupported mode %q", ErrInvalidVideoModelSpec, mode)
		}
		if _, duplicate := modes[mode]; duplicate {
			return fmt.Errorf("%w: duplicate mode %q", ErrInvalidVideoModelSpec, mode)
		}
		modes[mode] = struct{}{}
	}

	if len(spec.Parameters) > 32 {
		return fmt.Errorf("%w: at most 32 parameters are allowed", ErrInvalidVideoModelSpec)
	}
	parameters := make(map[string]VideoParameterSpec, len(spec.Parameters))
	requestKeys := make(map[string]string, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		if err := validateVideoParameterSpec(parameter, modes); err != nil {
			return err
		}
		if _, duplicate := parameters[parameter.Key]; duplicate {
			return fmt.Errorf("%w: duplicate parameter %q", ErrInvalidVideoModelSpec, parameter.Key)
		}
		requestKey := parameter.RequestKey
		if requestKey == "" {
			requestKey = parameter.Key
		}
		if _, reserved := videoTaskEnvelopeKeys[requestKey]; reserved || requestKey == "image" || requestKey == "images" {
			return fmt.Errorf("%w: parameter %q uses reserved request_key %q", ErrInvalidVideoModelSpec, parameter.Key, requestKey)
		}
		if existing, duplicate := requestKeys[requestKey]; duplicate {
			return fmt.Errorf("%w: parameters %q and %q use the same request_key", ErrInvalidVideoModelSpec, existing, parameter.Key)
		}
		requestKeys[requestKey] = parameter.Key
		parameters[parameter.Key] = parameter
	}
	if err := validateVideoReferenceInputsAPI3(spec.ReferenceInputs, modes, requestKeys); err != nil {
		return err
	}
	for key := range defaults {
		if _, ok := parameters[key]; !ok {
			return fmt.Errorf("%w: default for unknown parameter %q", ErrInvalidVideoModelSpec, key)
		}
	}
	for _, mode := range spec.Modes {
		modeDefaults := filterVideoParametersForMode(spec, mode, defaults)
		if _, err := ValidateVideoParameters(spec, mode, modeDefaults, true); err != nil {
			return fmt.Errorf("%w: defaults for mode %s: %v", ErrInvalidVideoModelSpec, mode, err)
		}
	}
	return nil
}

func isVideoModeAPI3(mode string) bool {
	switch mode {
	case VideoModeTextToVideo, VideoModeImageToVideo, VideoModeFirstLastFrame:
		return true
	default:
		return false
	}
}

func validateVideoReferenceInputsAPI3(inputs []VideoReferenceInputSpec, modes map[string]struct{}, requestKeys map[string]string) error {
	roles := make(map[string]VideoReferenceInputSpec, len(inputs))
	referenceRequestKeys := make(map[string]string, len(inputs))
	for _, input := range inputs {
		switch input.Role {
		case model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo,
			model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame:
		default:
			return fmt.Errorf("%w: unsupported reference role %q", ErrInvalidVideoModelSpec, input.Role)
		}
		if _, duplicate := roles[input.Role]; duplicate {
			return fmt.Errorf("%w: duplicate reference role %q", ErrInvalidVideoModelSpec, input.Role)
		}
		if !videoParameterKeyPattern.MatchString(input.RequestKey) {
			return fmt.Errorf("%w: reference role %q has invalid request_key", ErrInvalidVideoModelSpec, input.Role)
		}
		if _, reserved := videoTaskEnvelopeKeys[input.RequestKey]; reserved || input.RequestKey == "images" {
			return fmt.Errorf("%w: reference role %q uses reserved request_key %q", ErrInvalidVideoModelSpec, input.Role, input.RequestKey)
		}
		if parameter, duplicate := requestKeys[input.RequestKey]; duplicate {
			return fmt.Errorf("%w: reference role %q conflicts with parameter %q", ErrInvalidVideoModelSpec, input.Role, parameter)
		}
		if role, duplicate := referenceRequestKeys[input.RequestKey]; duplicate {
			return fmt.Errorf("%w: reference roles %q and %q use the same request_key", ErrInvalidVideoModelSpec, role, input.Role)
		}
		referenceRequestKeys[input.RequestKey] = input.Role
		roles[input.Role] = input
	}
	if _, supported := modes[VideoModeImageToVideo]; supported {
		requiredReferences := 0
		for _, role := range []string{model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo} {
			if input, ok := roles[role]; ok && input.Required {
				requiredReferences++
			}
		}
		if requiredReferences != 1 {
			return fmt.Errorf("%w: image_to_video requires exactly one image or video reference mapping", ErrInvalidVideoModelSpec)
		}
	}
	if _, supported := modes[VideoModeFirstLastFrame]; supported {
		firstFrame, hasFirstFrame := roles[model.VideoTaskAssetRoleFirstFrame]
		if !hasFirstFrame || !firstFrame.Required {
			return fmt.Errorf("%w: first_last_frame requires a required first_frame mapping", ErrInvalidVideoModelSpec)
		}
		lastFrame, hasLastFrame := roles[model.VideoTaskAssetRoleLastFrame]
		if !hasLastFrame || !lastFrame.Required {
			return fmt.Errorf("%w: first_last_frame requires a required last_frame mapping", ErrInvalidVideoModelSpec)
		}
	}
	return nil
}

func newVideoSampleReferenceSnapshotsAPI3(
	assetIDs []int64,
	inputs []VideoReferenceInputSpec,
) ([]VideoSampleReferenceSnapshotAPI3, error) {
	if len(assetIDs) != len(inputs) {
		return nil, ErrInvalidVideoSample
	}
	snapshots := make([]VideoSampleReferenceSnapshotAPI3, 0, len(assetIDs))
	for index, assetID := range assetIDs {
		if assetID <= 0 {
			return nil, ErrInvalidVideoSample
		}
		snapshots = append(snapshots, VideoSampleReferenceSnapshotAPI3{
			AssetID: assetID, Role: inputs[index].Role, RequestKey: inputs[index].RequestKey,
		})
	}
	return snapshots, nil
}

func decodeVideoSampleReferenceSnapshotsAPI3(
	encoded string,
	fallback []VideoReferenceInputSpec,
) ([]VideoSampleReferenceSnapshotAPI3, error) {
	var snapshots []VideoSampleReferenceSnapshotAPI3
	if err := common.UnmarshalJsonStr(encoded, &snapshots); err == nil {
		if err := validateVideoSampleReferenceSnapshotsAPI3(snapshots); err != nil {
			return nil, err
		}
		return snapshots, nil
	}
	var assetIDs []int64
	if err := common.UnmarshalJsonStr(encoded, &assetIDs); err != nil || len(assetIDs) != len(fallback) {
		return nil, ErrInvalidVideoSample
	}
	return newVideoSampleReferenceSnapshotsAPI3(assetIDs, fallback)
}

func validateVideoSampleReferenceSnapshotsAPI3(snapshots []VideoSampleReferenceSnapshotAPI3) error {
	seen := make(map[int64]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.AssetID <= 0 || !videoParameterKeyPattern.MatchString(snapshot.RequestKey) {
			return ErrInvalidVideoSample
		}
		switch snapshot.Role {
		case model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo,
			model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame:
		default:
			return ErrInvalidVideoSample
		}
		if _, duplicate := seen[snapshot.AssetID]; duplicate {
			return ErrInvalidVideoSample
		}
		seen[snapshot.AssetID] = struct{}{}
	}
	return nil
}

func videoSampleReferenceSnapshotsMatchAPI3(
	snapshots []VideoSampleReferenceSnapshotAPI3,
	inputs []VideoReferenceInputSpec,
) bool {
	if len(snapshots) != len(inputs) {
		return false
	}
	inputsByRole := make(map[string]VideoReferenceInputSpec, len(inputs))
	for _, input := range inputs {
		inputsByRole[input.Role] = input
	}
	for _, snapshot := range snapshots {
		input, exists := inputsByRole[snapshot.Role]
		if !exists || snapshot.RequestKey != input.RequestKey {
			return false
		}
	}
	return true
}

func expectedVideoReferenceRolesAPI3(specification VideoModelSpec, mode string) []string {
	inputs := expectedVideoReferenceInputsAPI3(specification, mode)
	roles := make([]string, 0, len(inputs))
	for _, input := range inputs {
		roles = append(roles, input.Role)
	}
	return roles
}

func expectedVideoReferenceInputsAPI3(specification VideoModelSpec, mode string) []VideoReferenceInputSpec {
	if mode == VideoModeTextToVideo {
		return nil
	}
	if mode == VideoModeImageToVideo {
		inputs := make([]VideoReferenceInputSpec, 0, 1)
		for _, input := range specification.ReferenceInputs {
			if input.Required && (input.Role == model.VideoTaskAssetRoleReference || input.Role == model.VideoTaskAssetRoleReferenceVideo) {
				inputs = append(inputs, input)
			}
		}
		return inputs
	}
	roles := []string{model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame}
	inputs := make([]VideoReferenceInputSpec, 0, len(roles))
	for _, role := range roles {
		for _, input := range specification.ReferenceInputs {
			if input.Role == role && input.Required {
				inputs = append(inputs, input)
				break
			}
		}
	}
	return inputs
}

func resolveVideoModelProfileAPI3(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
) (int64, int, string, VideoModelSpec, map[string]any, error) {
	modelName = strings.TrimSpace(modelName)
	if db == nil || !videoModelNamePattern.MatchString(modelName) {
		return 0, 0, "", VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
	}
	var profile model.KKAIVideoModelProfile
	if err := db.WithContext(ctx).Where("model = ? AND enabled = ?", modelName, true).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, "", VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
		}
		return 0, 0, "", VideoModelSpec{}, nil, err
	}
	specification, defaults, err := decodeVideoModelProfileAPI3(profile)
	if err != nil {
		return 0, 0, "", VideoModelSpec{}, nil, err
	}
	return profile.ID, specification.Version, profile.Model, specification, defaults, nil
}
