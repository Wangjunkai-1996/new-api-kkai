package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
)

var (
	ErrInvalidImageStudioSubmission = errors.New("invalid image studio submission")
	ErrImageStudioQuoteMismatch     = errors.New("image studio quote does not match the normalized request")
	ErrImageStudioQuoteExpired      = errors.New("image studio quote has expired")
	ErrImageStudioCapacityExceeded  = errors.New("image studio is at submission capacity")
)

type ImageStudioSubmissionRequest struct {
	TokenID        int            `json:"token_id"`
	Model          string         `json:"model"`
	Prompt         string         `json:"prompt"`
	Parameters     map[string]any `json:"parameters"`
	SampleID       *int64         `json:"sample_id,omitempty"`
	MaxQuota       *int           `json:"max_quota,omitempty"`
	QuoteHash      string         `json:"quote_hash,omitempty"`
	QuoteExpiresAt int64          `json:"quote_expires_at,omitempty"`
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
	MaxQuota             *int
	QuoteHash            string
	QuoteExpiresAt       int64
	RequestHash          string
	RelayRequest         *dto.ImageRequest
}

func ValidateImageStudioSubmitRequest(request ImageStudioSubmissionRequest) error {
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.TokenID <= 0 || request.MaxQuota == nil || *request.MaxQuota < 0 || request.QuoteExpiresAt <= 0 ||
		!validImageStudioHash(request.QuoteHash) {
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
	request.QuoteHash = strings.ToLower(strings.TrimSpace(request.QuoteHash))
	if request.MaxQuota != nil && *request.MaxQuota < 0 {
		return nil, ErrInvalidImageStudioSubmission
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
	profile, specification, defaults, err := resolveImageModelProfile(ctx, db, request.Model)
	if err != nil {
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
		RequestedCount: int(*relayRequest.N), MaxQuota: request.MaxQuota,
		QuoteHash: request.QuoteHash, QuoteExpiresAt: request.QuoteExpiresAt,
		RelayRequest: relayRequest,
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
	encoded, err := common.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode image studio request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validImageStudioHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
