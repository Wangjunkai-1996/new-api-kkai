package service

import (
	"bytes"
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
)

const (
	imageStudioQuoteTTL            = 5 * time.Minute
	imageStudioQuoteTokenVersion   = 1
	imageStudioQuoteTokenMaxLength = 8192
	imageStudioQuoteMACDomain      = "image-studio-quote-token:v1:"
)

type ImageStudioQuoteClaims struct {
	Version              int                    `json:"version"`
	UserID               int                    `json:"user_id"`
	RequestHash          string                 `json:"request_hash"`
	ExpiresAt            int64                  `json:"expires_at"`
	Quota                int                    `json:"quota"`
	OtherRatios          map[string]float64     `json:"other_ratios"`
	ImagePricingSnapshot *imagepricing.Snapshot `json:"image_pricing_snapshot"`
}

type ImageStudioQuote struct {
	Quota         int                `json:"quota"`
	DisplayAmount string             `json:"display_amount"`
	ExpiresAt     int64              `json:"expires_at"`
	OtherRatios   map[string]float64 `json:"other_ratios"`
	QuoteToken    string             `json:"quote_token"`
}

func NewImageStudioQuote(
	normalized *NormalizedImageStudioSubmission,
	quota int,
	otherRatios map[string]float64,
	pricingSnapshot *imagepricing.Snapshot,
) (ImageStudioQuote, error) {
	return newImageStudioQuoteAt(normalized, quota, otherRatios, pricingSnapshot, time.Now())
}

func newImageStudioQuoteAt(
	normalized *NormalizedImageStudioSubmission,
	quota int,
	otherRatios map[string]float64,
	pricingSnapshot *imagepricing.Snapshot,
	now time.Time,
) (ImageStudioQuote, error) {
	if normalized == nil {
		return ImageStudioQuote{}, ErrImageStudioQuoteMismatch
	}
	claims := ImageStudioQuoteClaims{
		Version:              imageStudioQuoteTokenVersion,
		UserID:               normalized.UserID,
		RequestHash:          normalized.RequestHash,
		ExpiresAt:            now.Add(imageStudioQuoteTTL).Unix(),
		Quota:                quota,
		OtherRatios:          cloneImageStudioRatios(otherRatios),
		ImagePricingSnapshot: cloneImagePricingSnapshot(pricingSnapshot),
	}
	if err := validateImageStudioQuoteClaims(normalized, claims); err != nil {
		return ImageStudioQuote{}, err
	}
	payload, err := common.Marshal(claims)
	if err != nil {
		return ImageStudioQuote{}, err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	token := encodedPayload + "." + imageStudioQuoteMAC(encodedPayload)
	if len(token) > imageStudioQuoteTokenMaxLength {
		return ImageStudioQuote{}, ErrImageStudioQuoteMismatch
	}
	return ImageStudioQuote{
		Quota: quota, DisplayAmount: logger.FormatQuota(quota), ExpiresAt: claims.ExpiresAt,
		OtherRatios: cloneImageStudioRatios(claims.OtherRatios), QuoteToken: token,
	}, nil
}

func ValidateImageStudioQuote(
	normalized *NormalizedImageStudioSubmission,
	now time.Time,
) (ImageStudioQuoteClaims, error) {
	if normalized == nil || normalized.UserID <= 0 || !validImageStudioHash(normalized.RequestHash) {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	if len(normalized.QuoteToken) > imageStudioQuoteTokenMaxLength {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	token := strings.TrimSpace(normalized.QuoteToken)
	if token == "" || len(token) > imageStudioQuoteTokenMaxLength {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	encodedPayload, providedMAC, found := strings.Cut(token, ".")
	if !found || encodedPayload == "" || !validImageStudioHash(providedMAC) || strings.Contains(providedMAC, ".") {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	expectedMAC, err := hex.DecodeString(imageStudioQuoteMAC(encodedPayload))
	if err != nil {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	providedMACBytes, err := hex.DecodeString(providedMAC)
	if err != nil || !hmac.Equal(expectedMAC, providedMACBytes) {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encodedPayload)
	if err != nil {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	var claims ImageStudioQuoteClaims
	if err := common.Unmarshal(payload, &claims); err != nil {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	canonical, err := common.Marshal(claims)
	if err != nil || !bytes.Equal(canonical, payload) {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteMismatch
	}
	if err := validateImageStudioQuoteClaims(normalized, claims); err != nil {
		return ImageStudioQuoteClaims{}, err
	}
	if claims.ExpiresAt <= now.Unix() {
		return ImageStudioQuoteClaims{}, ErrImageStudioQuoteExpired
	}
	return claims, nil
}

func validateImageStudioQuoteClaims(
	normalized *NormalizedImageStudioSubmission,
	claims ImageStudioQuoteClaims,
) error {
	if normalized == nil || normalized.UserID <= 0 || normalized.RequestedCount <= 0 ||
		claims.Version != imageStudioQuoteTokenVersion || claims.UserID <= 0 || claims.UserID != normalized.UserID ||
		claims.RequestHash != normalized.RequestHash || !validImageStudioHash(claims.RequestHash) ||
		claims.ExpiresAt <= 0 || claims.Quota < 0 {
		return ErrImageStudioQuoteMismatch
	}
	for name, ratio := range claims.OtherRatios {
		if strings.TrimSpace(name) == "" || ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return ErrImageStudioQuoteMismatch
		}
	}
	if claims.ImagePricingSnapshot == nil {
		countRatio, hasCountRatio := claims.OtherRatios["n"]
		if (hasCountRatio && countRatio != float64(normalized.RequestedCount)) ||
			(normalized.RequestedCount > 1 && !hasCountRatio) {
			return ErrImageStudioQuoteMismatch
		}
		return nil
	}
	snapshot := claims.ImagePricingSnapshot
	if normalized.RelayRequest == nil || snapshot.Model != normalized.Model || !validImageStudioHash(snapshot.PolicyHash) ||
		imagepricing.ValidateOutbound(snapshot, normalized.RelayRequest.Size, normalized.RequestedCount) != nil ||
		len(claims.OtherRatios) != 1 || claims.OtherRatios["n"] != float64(snapshot.RequestedCount) {
		return ErrImageStudioQuoteMismatch
	}
	expectedQuota, err := imagepricing.CalculateQuotaStrict(snapshot, snapshot.RequestedCount)
	if err != nil || expectedQuota != claims.Quota {
		return ErrImageStudioQuoteMismatch
	}
	return nil
}

func imageStudioQuoteMAC(encodedPayload string) string {
	return common.GenerateHMAC(imageStudioQuoteMACDomain + encodedPayload)
}

func cloneImageStudioRatios(ratios map[string]float64) map[string]float64 {
	if len(ratios) == 0 {
		return nil
	}
	cloned := make(map[string]float64, len(ratios))
	for name, ratio := range ratios {
		cloned[name] = ratio
	}
	return cloned
}

func cloneImagePricingSnapshot(snapshot *imagepricing.Snapshot) *imagepricing.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}
