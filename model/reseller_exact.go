package model

import (
	"math"
	"math/big"
	"strings"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// ResellerExactAggregate holds a sum of independently priced fractions. Its
// denominator can grow across price versions even when every tariff is small.
type ResellerExactAggregate string

func (ResellerExactAggregate) GormDataType() string { return "string" }

func (ResellerExactAggregate) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "longtext"
	}
	return "text"
}

// ResellerQuotaTariff keeps division exact until a cash amount is settled.
// Existing string columns accept decimals and reduced fractions; no quote is
// recomputed from a later price setting.
func ResellerQuotaTariff(priceCNY, quotaPerUnit, supplyRatio, saleRatio decimal.Decimal) (string, string, error) {
	for _, value := range []decimal.Decimal{priceCNY, quotaPerUnit, supplyRatio, saleRatio} {
		if value.Exponent() < -32 || value.Exponent() > 18 || value.Abs().GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return "", "", ErrResellerFinanceInvalidInput
		}
	}
	if !priceCNY.IsPositive() || !quotaPerUnit.IsPositive() || supplyRatio.IsNegative() || saleRatio.IsNegative() || (saleRatio.IsZero() && !supplyRatio.IsZero()) {
		return "", "", ErrResellerFinanceInvalidInput
	}
	sale := new(big.Rat).Quo(priceCNY.Mul(decimal.NewFromInt(100)).Rat(), quotaPerUnit.Rat())
	supply := new(big.Rat)
	if !saleRatio.IsZero() {
		supply.Quo(new(big.Rat).Mul(sale, supplyRatio.Rat()), saleRatio.Rat())
	}
	saleText, err := resellerExactString(sale, 64)
	if err != nil {
		return "", "", err
	}
	supplyText, err := resellerExactString(supply, 64)
	return saleText, supplyText, err
}

// ResellerQuotedCharge rounds only the final request estimate up to CNY fen.
func ResellerQuotedCharge(unit string, quota int64) (int64, error) {
	rate, err := parseResellerExact(unit)
	if err != nil || rate.Sign() < 0 || quota <= 0 || quota > math.MaxInt32 {
		return 0, ErrResellerFinanceInvalidInput
	}
	return resellerExactCeilInt64(new(big.Rat).Mul(rate, big.NewRat(quota, 1)))
}

// Legacy decimals retain their original bounds. New fractions are canonical,
// bounded before integer allocation, and never pass through floating point.
func parseResellerExact(value string) (*big.Rat, error) {
	if len(value) > 128 {
		return nil, ErrResellerFinanceInvalidInput
	}
	return parseResellerAggregate(ResellerExactAggregate(value))
}

// Only persisted, internally accumulated money uses this unbounded text path.
// External tariffs and individual ledger inputs retain their length bounds.
func parseResellerAggregate(value ResellerExactAggregate) (*big.Rat, error) {
	text := string(value)
	if !strings.Contains(text, "/") {
		d, err := parseResellerDecimal(text)
		if err != nil {
			return nil, err
		}
		return d.Rat(), nil
	}
	ratio, ok := new(big.Rat).SetString(text)
	if !ok || ratio.RatString() != text || new(big.Rat).Abs(ratio).Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	return ratio, nil
}

func resellerAggregateString(value *big.Rat) (ResellerExactAggregate, error) {
	text, err := resellerExactString(value, 0)
	return ResellerExactAggregate(text), err
}

func resellerExactString(value *big.Rat, limit int) (string, error) {
	if value == nil || new(big.Rat).Abs(value).Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
		return "", ErrResellerFinanceInvalidInput
	}
	// Preserve decimal API values whenever the reduced denominator contains only
	// factors 2 and 5. Otherwise p/q preserves every fraction without rounding.
	denominator := new(big.Int).Set(value.Denom())
	twos, fives := int32(0), int32(0)
	for _, factor := range []int64{2, 5} {
		for new(big.Int).Mod(denominator, big.NewInt(factor)).Sign() == 0 {
			denominator.Quo(denominator, big.NewInt(factor))
			if factor == 2 {
				twos++
			} else {
				fives++
			}
		}
	}
	text := value.RatString()
	if denominator.Cmp(big.NewInt(1)) == 0 && max(twos, fives) <= 32 {
		scale := max(twos, fives)
		coefficient := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
		coefficient.Mul(coefficient, value.Num()).Quo(coefficient, value.Denom())
		text = decimal.NewFromBigInt(coefficient, -scale).String()
	}
	if limit > 0 && len(text) > limit {
		return "", ErrResellerFinanceInvalidInput
	}
	return text, nil
}

func resellerExactCeilInt64(value *big.Rat) (int64, error) {
	if value == nil || value.Sign() < 0 {
		return 0, ErrResellerFinanceInvalidInput
	}
	integer, remainder := new(big.Int), new(big.Int)
	integer.QuoRem(value.Num(), value.Denom(), remainder)
	if remainder.Sign() != 0 {
		integer.Add(integer, big.NewInt(1))
	}
	if !integer.IsInt64() {
		return 0, ErrResellerFinanceInvalidInput
	}
	return integer.Int64(), nil
}
