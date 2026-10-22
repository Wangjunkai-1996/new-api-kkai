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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/types"
)

const (
	imageStudioQuoteTTL            = 5 * time.Minute
	imageStudioQuoteTokenVersion   = 1
	imageStudioQuoteTokenMaxLength = 8192
	imageStudioQuoteMACDomain      = "image-studio-quote-token:v1:"
)

type ImageStudioQuoteClaims struct {
	Version              int                      `json:"version"`
	UserID               int                      `json:"user_id"`
	RequestHash          string                   `json:"request_hash"`
	ExpiresAt            int64                    `json:"expires_at"`
	Quota                int                      `json:"quota"`
	OtherRatios          map[string]float64       `json:"other_ratios"`
	ImagePricingSnapshot *imagepricing.Snapshot   `json:"image_pricing_snapshot"`
	MembershipPricing    *types.MembershipPricing `json:"membership_pricing,omitempty"`
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
	membership ...*types.MembershipPricing,
) (ImageStudioQuote, error) {
	return newImageStudioQuoteAt(normalized, quota, otherRatios, pricingSnapshot, time.Now(), membership...)
}

func newImageStudioQuoteAt(
	normalized *NormalizedImageStudioSubmission,
	quota int,
	otherRatios map[string]float64,
	pricingSnapshot *imagepricing.Snapshot,
	now time.Time,
	membership ...*types.MembershipPricing,
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
	if len(membership) > 0 {
		claims.MembershipPricing = membership[0]
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
	if claims.MembershipPricing != nil {
		if _, err := imageStudioMembershipReservation(normalized, claims); err != nil {
			return err
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
	if err != nil || (claims.MembershipPricing == nil && expectedQuota != claims.Quota) ||
		(claims.MembershipPricing != nil && expectedQuota > claims.Quota) {
		return ErrImageStudioQuoteMismatch
	}
	return nil
}

// A signed quote carries a wallet pricing snapshot. Both funding quotes derive
// from those frozen facts; acceptance checks the account and pricing revisions
// and available funds, and never silently reprices a signed quote.
func imageStudioMembershipReservation(normalized *NormalizedImageStudioSubmission, claims ImageStudioQuoteClaims) (model.MembershipReservationInput, error) {
	input := model.MembershipReservationInput{}
	snapshot := claims.MembershipPricing
	if snapshot == nil || snapshot.Version != 1 || snapshot.GroupKey == "" || snapshot.ModelName != normalized.Model ||
		snapshot.Image == nil || claims.ImagePricingSnapshot == nil || *snapshot.Image != *claims.ImagePricingSnapshot ||
		snapshot.Tiered != nil || snapshot.Price.GroupRatioInfo.GroupRatio != snapshot.WalletRatio ||
		snapshot.Image.GroupRatio != snapshot.WalletRatio || snapshot.Discount <= 0 || snapshot.Discount > 1 ||
		math.IsNaN(snapshot.MaximumBeforeGroup) || math.IsInf(snapshot.MaximumBeforeGroup, 0) || snapshot.MaximumBeforeGroup < 0 ||
		math.IsNaN(snapshot.BaseRatio) || math.IsInf(snapshot.BaseRatio, 0) || snapshot.BaseRatio < 0 {
		return input, ErrImageStudioQuoteMismatch
	}
	input.UserID, input.TokenID, input.SkipToken = normalized.UserID, normalized.TokenID, true
	input.GroupKey, input.ExpectedAccountID, input.ExpectedRevision = snapshot.GroupKey, snapshot.AccountID, snapshot.AccountRevision
	input.ExpectedReleaseID = snapshot.ReleaseID
	memberRatio := snapshot.WalletRatio
	if snapshot.Discount < 1 {
		memberRatio = math.Min(memberRatio, snapshot.BaseRatio*snapshot.Discount)
	}
	for _, member := range []bool{false, true} {
		frozen := *snapshot
		frozen.Image = cloneImagePricingSnapshot(snapshot.Image)
		ratio := snapshot.WalletRatio
		if member {
			ratio = memberRatio
		}
		quota, err := common.QuotaFromFloatStrict(math.Ceil(snapshot.MaximumBeforeGroup * ratio))
		if err != nil || (!member && quota != claims.Quota) {
			return input, ErrImageStudioQuoteMismatch
		}
		frozen.Price.GroupRatioInfo.GroupRatio = ratio
		frozen.Price.QuotaToPreConsume, frozen.Price.Quota, frozen.Price.FreeModel = quota, quota, quota == 0
		frozen.Image.GroupRatio = ratio
		encoded, err := common.Marshal(frozen)
		if err != nil {
			return input, err
		}
		if member {
			input.SubscriptionQuota, input.SubscriptionSnapshot = int64(quota), string(encoded)
		} else {
			input.WalletQuota, input.WalletSnapshot = int64(quota), string(encoded)
		}
	}
	return input, nil
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
