package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

const videoStudioQuoteTTL = 5 * time.Minute

func VideoStudioIdempotencyFingerprint(request VideoStudioSubmissionRequest) (string, error) {
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

func NewVideoStudioQuote(normalized *NormalizedVideoStudioSubmission, quota int, otherRatios map[string]float64) VideoStudioQuote {
	return newVideoStudioQuoteAt(normalized, quota, otherRatios, time.Now())
}

func newVideoStudioQuoteAt(
	normalized *NormalizedVideoStudioSubmission,
	quota int,
	otherRatios map[string]float64,
	now time.Time,
) VideoStudioQuote {
	expiresAt := now.Add(videoStudioQuoteTTL).Unix()
	requestHash := ""
	if normalized != nil {
		requestHash = signVideoStudioQuote(normalized.UserID, normalized.RequestHash, expiresAt)
	}
	return VideoStudioQuote{
		Quota: quota, DisplayAmount: logger.FormatQuota(quota), RequestHash: requestHash,
		ExpiresAt: expiresAt, OtherRatios: otherRatios,
	}
}

func ValidateVideoStudioQuote(normalized *NormalizedVideoStudioSubmission, now time.Time) error {
	if normalized == nil || normalized.UserID <= 0 || !validVideoQuoteHash(normalized.RequestHash) ||
		!validVideoQuoteHash(normalized.QuoteHash) || normalized.QuoteExpiresAt <= 0 {
		return ErrVideoStudioQuoteMismatch
	}
	if normalized.QuoteExpiresAt <= now.Unix() {
		return ErrVideoStudioQuoteExpired
	}
	expected := signVideoStudioQuote(normalized.UserID, normalized.RequestHash, normalized.QuoteExpiresAt)
	if !hmac.Equal([]byte(expected), []byte(normalized.QuoteHash)) {
		return ErrVideoStudioQuoteMismatch
	}
	return nil
}

func signVideoStudioQuote(userID int, requestHash string, expiresAt int64) string {
	message := fmt.Sprintf("video-studio-quote:v1:%d:%s:%d", userID, requestHash, expiresAt)
	return common.GenerateHMAC(message)
}
