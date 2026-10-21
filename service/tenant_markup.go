package service

import (
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// Group price versions distinguish relative pricing from historical absolute
// multipliers without changing the schema. Older binaries cannot read this
// format and must not be selected for rollback after markup versions are saved.
const tenantMarkupPrefix = "markup:"

func parseTenantMarkupNumber(value string) (decimal.Decimal, error) {
	if value == "" || len(value) > 64 {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	number, err := decimal.NewFromString(value)
	if err != nil || number.Exponent() < -18 || number.Exponent() > 6 || number.IsNegative() || number.GreaterThan(decimal.NewFromInt(1000000)) {
		return decimal.Zero, model.ErrResellerInvalidPrice
	}
	return number, nil
}

func encodeTenantMarkupFactor(factor *big.Rat) (string, error) {
	if factor.Cmp(big.NewRat(1, 1)) < 0 || factor.Cmp(big.NewRat(10001, 1)) > 0 {
		return "", model.ErrResellerInvalidPrice
	}
	if factor.Cmp(big.NewRat(1, 1)) == 0 {
		return "", nil
	}
	stored := tenantMarkupPrefix + factor.RatString()
	if len(stored) > 64 {
		return "", model.ErrResellerInvalidPrice
	}
	return stored, nil
}

func tenantMarkupFromPercent(value string) (string, error) {
	percent, err := parseTenantMarkupNumber(value)
	if err != nil {
		return "", err
	}
	factor := new(big.Rat).Add(big.NewRat(1, 1), new(big.Rat).Quo(percent.Rat(), big.NewRat(100, 1)))
	return encodeTenantMarkupFactor(factor)
}

// resolveTenantGroupMarkup returns numeric prices to all existing customer and
// relay callers. Exact relative factors keep legacy conversions lossless, even
// when the displayed percentage has a repeating decimal expansion.
func resolveTenantGroupMarkup(stored string, platform decimal.Decimal) (decimal.Decimal, string, string, error) {
	if platform.IsNegative() || platform.GreaterThan(decimal.NewFromInt(1000000)) {
		return decimal.Zero, "", "", model.ErrResellerInvalidPrice
	}
	if stored == "" {
		if platform.Exponent() < -32 {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		return platform, "0", "markup", nil
	}
	if strings.HasPrefix(stored, tenantMarkupPrefix) {
		if len(stored) > 64 {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		text := strings.TrimPrefix(stored, tenantMarkupPrefix)
		for _, character := range text {
			if (character < '0' || character > '9') && character != '/' {
				return decimal.Zero, "", "", model.ErrResellerInvalidPrice
			}
		}
		factor, ok := new(big.Rat).SetString(text)
		if !ok || factor.RatString() != text {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		if _, err := encodeTenantMarkupFactor(factor); err != nil {
			return decimal.Zero, "", "", err
		}
		if platform.Exponent() < -32 {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		effective := new(big.Rat).Mul(platform.Rat(), factor)
		if effective.Cmp(big.NewRat(1000000, 1)) > 0 {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		precision := 20
		if platform.Exponent() < 0 {
			precision += int(-platform.Exponent())
		}
		multiplier, err := decimal.NewFromString(effective.FloatString(precision))
		if err != nil {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		multiplier, err = decimal.NewFromString(multiplier.String())
		if err != nil || multiplier.Exponent() < -32 || (platform.IsPositive() && factor.Cmp(big.NewRat(1, 1)) > 0 && !multiplier.GreaterThan(platform)) {
			return decimal.Zero, "", "", model.ErrResellerInvalidPrice
		}
		percent := new(big.Rat).Mul(new(big.Rat).Sub(factor, big.NewRat(1, 1)), big.NewRat(100, 1))
		percentage, _ := decimal.NewFromString(percent.FloatString(18))
		return multiplier, percentage.String(), "markup", nil
	}
	multiplier, err := parseTenantMarkupNumber(stored)
	if err != nil {
		return decimal.Zero, "", "", err
	}
	multiplier = decimal.Max(multiplier, platform)
	if platform.IsZero() {
		if multiplier.IsZero() {
			return multiplier, "0", "legacy", nil
		}
		return multiplier, "", "legacy", nil
	}
	percent := new(big.Rat).Mul(new(big.Rat).Sub(new(big.Rat).Quo(multiplier.Rat(), platform.Rat()), big.NewRat(1, 1)), big.NewRat(100, 1))
	percentage, _ := decimal.NewFromString(percent.FloatString(18))
	return multiplier, percentage.String(), "legacy", nil
}
