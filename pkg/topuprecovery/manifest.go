package topuprecovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	hexSHA256Pattern             = regexp.MustCompile(`^[0-9a-f]{64}$`)
	gitSHA1Pattern               = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ErrInvalidManifest           = errors.New("invalid topup recovery manifest")
	ErrInvalidQuotaConfiguration = errors.New("invalid topup recovery quota configuration")
)

func SealManifest(manifest *Manifest) error {
	if manifest == nil {
		return ErrInvalidManifest
	}
	manifest.SHA256 = ""
	digest, err := hashValue(manifest)
	if err != nil {
		return err
	}
	manifest.SHA256 = digest
	return nil
}

func ValidateManifest(manifest *Manifest, expectedSHA256, sourceRevision string) error {
	if manifest == nil || manifest.SchemaVersion != SchemaVersion || manifest.ToolVersion != ToolVersion {
		return ErrInvalidManifest
	}
	if !gitSHA1Pattern.MatchString(sourceRevision) || manifest.SourceRevision != sourceRevision {
		return fmt.Errorf("%w: source revision mismatch", ErrInvalidManifest)
	}
	if manifest.ActiveFromID <= 0 || manifest.CutoffID < manifest.ActiveFromID {
		return fmt.Errorf("%w: invalid topup range", ErrInvalidManifest)
	}
	quotaPerUnit, err := strconv.ParseFloat(manifest.QuotaPerUnit, 64)
	if err != nil || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) || quotaPerUnit <= 0 {
		return fmt.Errorf("%w: invalid quota configuration", ErrInvalidManifest)
	}
	if !hexSHA256Pattern.MatchString(expectedSHA256) || manifest.SHA256 != expectedSHA256 {
		return fmt.Errorf("%w: expected SHA-256 mismatch", ErrInvalidManifest)
	}

	previousID := int64(0)
	for _, order := range manifest.Orders {
		if order.TopUpID < manifest.ActiveFromID || order.TopUpID > manifest.CutoffID ||
			order.TopUpID <= previousID || order.UserID <= 0 || order.InviterID <= 0 ||
			order.CreditedQuota <= 0 || order.CompletedAt <= 0 ||
			strings.TrimSpace(order.InviterGroup) == "" ||
			order.EventKey != fmt.Sprintf("newapi:topup:%d", order.TopUpID) {
			return fmt.Errorf("%w: invalid order identity", ErrInvalidManifest)
		}
		if !hexSHA256Pattern.MatchString(order.TradeNoSHA256) ||
			!hexSHA256Pattern.MatchString(order.SourceRowSHA256) ||
			!hexSHA256Pattern.MatchString(order.ProviderResponseSHA256) ||
			!hexSHA256Pattern.MatchString(order.EventPayloadSHA256) {
			return fmt.Errorf("%w: invalid order digest", ErrInvalidManifest)
		}
		previousID = order.TopUpID
	}

	unsigned := *manifest
	unsigned.SHA256 = ""
	actualSHA256, err := hashValue(&unsigned)
	if err != nil {
		return err
	}
	if actualSHA256 != manifest.SHA256 {
		return fmt.Errorf("%w: manifest content hash mismatch", ErrInvalidManifest)
	}
	return nil
}

func hashString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func hashValue(value any) (string, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sourceDigest(source topUpSource) (string, error) {
	return hashValue(sourceIdentity{
		ID:              source.ID,
		UserID:          source.UserID,
		Amount:          source.Amount,
		TradeNo:         source.TradeNo,
		PaymentProvider: source.PaymentProvider,
		CreateTime:      source.CreateTime,
		Status:          source.Status,
		InviterID:       source.InviterID,
		InviterGroup:    normalizeInviterGroup(source.InviterGroup),
	})
}

func providerDigest(order ProviderOrder) (string, error) {
	return hashValue(providerIdentity{
		Code:           order.Code,
		Status:         order.Status,
		TradeNo:        order.TradeNo,
		ServiceTradeNo: order.ServiceTradeNo,
		PaymentType:    order.PaymentType,
		EndTime:        order.EndTime,
		CompletedAt:    order.CompletedAt,
	})
}
