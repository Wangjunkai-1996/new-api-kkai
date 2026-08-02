package service

import (
	"crypto/hmac"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

const imageStudioQuoteTTL = 5 * time.Minute

type ImageStudioQuote struct {
	Quota         int                `json:"quota"`
	DisplayAmount string             `json:"display_amount"`
	RequestHash   string             `json:"request_hash"`
	ExpiresAt     int64              `json:"expires_at"`
	OtherRatios   map[string]float64 `json:"other_ratios"`
}

func NewImageStudioQuote(
	normalized *NormalizedImageStudioSubmission,
	quota int,
	otherRatios map[string]float64,
) ImageStudioQuote {
	return newImageStudioQuoteAt(normalized, quota, otherRatios, time.Now())
}

func newImageStudioQuoteAt(
	normalized *NormalizedImageStudioSubmission,
	quota int,
	otherRatios map[string]float64,
	now time.Time,
) ImageStudioQuote {
	expiresAt := now.Add(imageStudioQuoteTTL).Unix()
	requestHash := ""
	if normalized != nil {
		requestHash = signImageStudioQuote(normalized.UserID, normalized.RequestHash, expiresAt)
	}
	return ImageStudioQuote{
		Quota: quota, DisplayAmount: logger.FormatQuota(quota), RequestHash: requestHash,
		ExpiresAt: expiresAt, OtherRatios: otherRatios,
	}
}

func ValidateImageStudioQuote(normalized *NormalizedImageStudioSubmission, now time.Time) error {
	if normalized == nil || normalized.UserID <= 0 || !validImageStudioHash(normalized.RequestHash) ||
		!validImageStudioHash(normalized.QuoteHash) || normalized.QuoteExpiresAt <= 0 {
		return ErrImageStudioQuoteMismatch
	}
	if normalized.QuoteExpiresAt <= now.Unix() {
		return ErrImageStudioQuoteExpired
	}
	expected := signImageStudioQuote(normalized.UserID, normalized.RequestHash, normalized.QuoteExpiresAt)
	if !hmac.Equal([]byte(expected), []byte(normalized.QuoteHash)) {
		return ErrImageStudioQuoteMismatch
	}
	return nil
}

func signImageStudioQuote(userID int, requestHash string, expiresAt int64) string {
	message := fmt.Sprintf("image-studio-quote:v1:%d:%s:%d", userID, strings.ToLower(requestHash), expiresAt)
	return common.GenerateHMAC(message)
}
