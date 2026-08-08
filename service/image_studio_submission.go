package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"gorm.io/gorm"
)

var (
	ErrInvalidImageStudioSubmission = errors.New("invalid image studio submission")
	ErrImageStudioQuoteMismatch     = errors.New("image studio quote does not match the normalized request")
	ErrImageStudioQuoteExpired      = errors.New("image studio quote has expired")
	ErrImageStudioCapacityExceeded  = errors.New("image studio is at submission capacity")
)

const (
	ImageStudioModeGeneration = "generation"
	ImageStudioModeEdit       = "edit"
	ImageStudioEditModel      = "gpt-image-2"
)

type ImageStudioReferenceMetadata struct {
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type ImageStudioSubmissionRequest struct {
	TokenID    int                            `json:"token_id"`
	Model      string                         `json:"model"`
	Prompt     string                         `json:"prompt"`
	Parameters map[string]any                 `json:"parameters"`
	SampleID   *int64                         `json:"sample_id,omitempty"`
	QuoteToken string                         `json:"quote_token,omitempty"`
	Mode       string                         `json:"-"`
	Reference  *ImageStudioReferenceMetadata  `json:"reference,omitempty"`
	References []ImageStudioReferenceMetadata `json:"references,omitempty"`
}

func (request *ImageStudioSubmissionRequest) UnmarshalJSON(data []byte) error {
	type requestAlias ImageStudioSubmissionRequest
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, hasReference := fields["reference"]
	_, hasReferences := fields["references"]
	if hasReference && hasReferences {
		return ErrInvalidImageStudioSubmission
	}
	var decoded requestAlias
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*request = ImageStudioSubmissionRequest(decoded)
	return nil
}

type NormalizedImageStudioSubmission struct {
	UserID               int
	TokenID              int
	ProfileID            int64
	SpecificationVersion int
	Model                string
	Prompt               string
	Parameters           map[string]any
	SampleID             *int64
	RequestedCount       int
	QuoteToken           string
	RequestHash          string
	RelayRequest         *dto.ImageRequest
	Mode                 string
	References           []ImageStudioReferenceMetadata
}

func ValidateImageStudioSubmitRequest(request ImageStudioSubmissionRequest) error {
	token := strings.TrimSpace(request.QuoteToken)
	if request.TokenID <= 0 || token == "" || len(request.QuoteToken) > imageStudioQuoteTokenMaxLength ||
		len(token) > imageStudioQuoteTokenMaxLength {
		return ErrInvalidImageStudioSubmission
	}
	return nil
}

func NormalizeImageStudioSubmission(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	request ImageStudioSubmissionRequest,
) (*NormalizedImageStudioSubmission, error) {
	if db == nil || userID <= 0 || request.TokenID <= 0 {
		return nil, ErrInvalidImageStudioSubmission
	}
	request.Model = strings.TrimSpace(request.Model)
	request.Prompt = strings.TrimSpace(request.Prompt)
	request.QuoteToken = strings.TrimSpace(request.QuoteToken)
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode == "" {
		request.Mode = ImageStudioModeGeneration
	}
	if err := NormalizeImageStudioReferenceFields(&request); err != nil {
		return nil, err
	}
	var sample *model.KKAIImageSample
	if request.SampleID != nil {
		if *request.SampleID <= 0 {
			return nil, ErrInvalidImageStudioSubmission
		}
		var found model.KKAIImageSample
		if err := db.WithContext(ctx).First(
			&found, "id = ? AND status = ?", *request.SampleID, model.ImageSampleStatusPublished,
		).Error; err != nil {
			return nil, ErrImageSampleNotFound
		}
		sample = &found
		if request.Model == "" {
			var profile model.KKAIImageModelProfile
			if err := db.WithContext(ctx).Select("model").First(&profile, "id = ?", found.ModelProfileID).Error; err != nil {
				return nil, ErrImageModelProfileNotFound
			}
			request.Model = profile.Model
		}
		if request.Prompt == "" {
			request.Prompt = found.Prompt
		}
		if len(request.Parameters) == 0 {
			request.Parameters = map[string]any{}
			if err := common.UnmarshalJsonStr(found.Parameters, &request.Parameters); err != nil {
				return nil, fmt.Errorf("decode image sample parameters: %w", err)
			}
		}
	}
	if request.Prompt == "" || len(request.Prompt) > 8000 {
		return nil, ErrInvalidImageStudioSubmission
	}
	if request.Mode == ImageStudioModeEdit && request.Model != ImageStudioEditModel {
		return nil, ErrInvalidImageStudioSubmission
	}
	profile, specification, defaults, err := resolveImageModelProfile(ctx, db, request.Model)
	if err != nil {
		return nil, err
	}
	maxReferences := specification.MaxReferenceImages
	if maxReferences == 0 {
		maxReferences = 1
	}
	if err := normalizeImageStudioReferences(&request, maxReferences); err != nil {
		return nil, err
	}
	if sample != nil && (sample.ModelProfileID != profile.ID || sample.ModelVersion != profile.SpecificationVersion) {
		return nil, ErrInvalidImageStudioSubmission
	}
	merged := make(map[string]any, len(defaults)+len(request.Parameters))
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range request.Parameters {
		merged[key] = value
	}
	parameters, err := ValidateImageParameters(specification, merged, true)
	if err != nil {
		return nil, err
	}
	relayRequest, err := BuildImageRelayRequest(profile.Model, request.Prompt, specification, parameters)
	if err != nil {
		return nil, err
	}

	normalized := &NormalizedImageStudioSubmission{
		UserID: userID, TokenID: request.TokenID, ProfileID: profile.ID,
		SpecificationVersion: profile.SpecificationVersion, Model: profile.Model,
		Prompt: request.Prompt, Parameters: parameters, SampleID: request.SampleID,
		RequestedCount: int(*relayRequest.N), QuoteToken: request.QuoteToken,
		RelayRequest: relayRequest, Mode: request.Mode,
		References: cloneImageStudioReferences(request.References),
	}
	requestHash, err := imageStudioRequestHash(normalized, specification)
	if err != nil {
		return nil, err
	}
	normalized.RequestHash = requestHash
	return normalized, nil
}

func imageStudioRequestHash(normalized *NormalizedImageStudioSubmission, specification ImageModelSpec) (string, error) {
	if normalized == nil {
		return "", ErrInvalidImageStudioSubmission
	}
	type canonicalParameter struct {
		Key        string `json:"key"`
		RequestKey string `json:"request_key"`
		Value      any    `json:"value"`
	}
	parameters := make([]canonicalParameter, 0, len(normalized.Parameters))
	for _, parameter := range specification.Parameters {
		value, exists := normalized.Parameters[parameter.Key]
		if !exists {
			continue
		}
		parameters = append(parameters, canonicalParameter{
			Key: parameter.Key, RequestKey: parameter.RequestKey, Value: value,
		})
	}
	var encoded []byte
	var err error
	// Preserve single-reference quote and idempotency hashes across rolling deployments.
	if normalized.Mode == ImageStudioModeEdit && len(normalized.References) == 1 {
		canonical := struct {
			TokenID              int                           `json:"token_id"`
			ProfileID            int64                         `json:"profile_id"`
			SpecificationVersion int                           `json:"specification_version"`
			Model                string                        `json:"model"`
			Prompt               string                        `json:"prompt"`
			Parameters           []canonicalParameter          `json:"parameters"`
			SampleID             *int64                        `json:"sample_id,omitempty"`
			Mode                 string                        `json:"mode"`
			Reference            *ImageStudioReferenceMetadata `json:"reference"`
		}{
			TokenID: normalized.TokenID, ProfileID: normalized.ProfileID,
			SpecificationVersion: normalized.SpecificationVersion, Model: normalized.Model,
			Prompt: normalized.Prompt, Parameters: parameters, SampleID: normalized.SampleID,
			Mode: normalized.Mode, Reference: &normalized.References[0],
		}
		encoded, err = common.Marshal(canonical)
	} else if normalized.Mode == ImageStudioModeEdit {
		canonical := struct {
			TokenID              int                            `json:"token_id"`
			ProfileID            int64                          `json:"profile_id"`
			SpecificationVersion int                            `json:"specification_version"`
			Model                string                         `json:"model"`
			Prompt               string                         `json:"prompt"`
			Parameters           []canonicalParameter           `json:"parameters"`
			SampleID             *int64                         `json:"sample_id,omitempty"`
			Mode                 string                         `json:"mode"`
			References           []ImageStudioReferenceMetadata `json:"references"`
		}{
			TokenID: normalized.TokenID, ProfileID: normalized.ProfileID,
			SpecificationVersion: normalized.SpecificationVersion, Model: normalized.Model,
			Prompt: normalized.Prompt, Parameters: parameters, SampleID: normalized.SampleID,
			Mode: normalized.Mode, References: normalized.References,
		}
		encoded, err = common.Marshal(canonical)
	} else {
		// Keep generation quote and idempotency hashes byte-for-byte compatible with
		// the original Image Studio canonical request used before edit support.
		canonical := struct {
			TokenID              int                  `json:"token_id"`
			ProfileID            int64                `json:"profile_id"`
			SpecificationVersion int                  `json:"specification_version"`
			Model                string               `json:"model"`
			Prompt               string               `json:"prompt"`
			Parameters           []canonicalParameter `json:"parameters"`
			SampleID             *int64               `json:"sample_id,omitempty"`
		}{
			TokenID: normalized.TokenID, ProfileID: normalized.ProfileID,
			SpecificationVersion: normalized.SpecificationVersion, Model: normalized.Model,
			Prompt: normalized.Prompt, Parameters: parameters, SampleID: normalized.SampleID,
		}
		encoded, err = common.Marshal(canonical)
	}
	if err != nil {
		return "", fmt.Errorf("encode image studio request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func NormalizeImageStudioReferenceFields(request *ImageStudioSubmissionRequest) error {
	if request == nil {
		return ErrInvalidImageStudioSubmission
	}
	if request.Reference == nil {
		return nil
	}
	if request.References != nil {
		return ErrInvalidImageStudioSubmission
	}
	reference := *request.Reference
	request.Reference = nil
	request.References = []ImageStudioReferenceMetadata{reference}
	return nil
}

func normalizeImageStudioReferences(request *ImageStudioSubmissionRequest, maxReferences int) error {
	if request == nil {
		return ErrInvalidImageStudioSubmission
	}
	switch request.Mode {
	case ImageStudioModeGeneration:
		if len(request.References) != 0 {
			return ErrInvalidImageStudioSubmission
		}
	case ImageStudioModeEdit:
		if maxReferences <= 0 || maxReferences > MaxImageStudioReferenceImages ||
			len(request.References) == 0 || len(request.References) > maxReferences {
			return ErrInvalidImageStudioSubmission
		}
		settings := image_studio_setting.Get()
		var totalBytes int64
		for index := range request.References {
			reference := &request.References[index]
			reference.SHA256 = strings.ToLower(strings.TrimSpace(reference.SHA256))
			if !validImageStudioHash(reference.SHA256) || reference.SizeBytes <= 0 ||
				reference.SizeBytes > settings.MaxReferenceBytes ||
				reference.SizeBytes > settings.MaxReferenceTotalBytes-totalBytes {
				return ErrInvalidImageStudioSubmission
			}
			totalBytes += reference.SizeBytes
		}
	default:
		return ErrInvalidImageStudioSubmission
	}
	return nil
}

func cloneImageStudioReferences(references []ImageStudioReferenceMetadata) []ImageStudioReferenceMetadata {
	if len(references) == 0 {
		return nil
	}
	return append([]ImageStudioReferenceMetadata(nil), references...)
}

func validImageStudioHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
