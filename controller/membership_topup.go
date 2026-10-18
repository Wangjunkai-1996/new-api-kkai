package controller

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// Only new quotations use this policy. Existing TopUp rows retain their stored
// Amount/Money and their existing provider-specific settlement semantics.
func topUpPromotionFactors(group string, amount int64) (float64, float64) {
	if model.MembershipTopupDiscountsDisabled() {
		return 1, 1
	}
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if configured, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && configured > 0 {
		discount = configured
	}
	return ratio, discount
}

func creemProductWithoutBonus(product CreemProduct) (CreemProduct, error) {
	if !model.MembershipTopupDiscountsDisabled() {
		return product, nil
	}
	if product.Price <= 0 || math.IsNaN(product.Price) || math.IsInf(product.Price, 0) {
		return product, errors.New("invalid Creem product price")
	}
	amount := decimal.NewFromFloat(product.Price)
	switch strings.ToUpper(product.Currency) {
	case "USD":
	case "CNY":
		if operation_setting.USDExchangeRate <= 0 || math.IsNaN(operation_setting.USDExchangeRate) || math.IsInf(operation_setting.USDExchangeRate, 0) {
			return product, errors.New("invalid currency exchange rate")
		}
		amount = amount.Div(decimal.NewFromFloat(operation_setting.USDExchangeRate))
	default:
		return product, errors.New("Creem top-up currency requires an explicit exchange rate")
	}
	quota, err := validateCreditedQuota(amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		return product, err
	}
	// Remove only bonus credits, preserving products whose ordinary price already
	// includes a markup; do not create a new promotion while removing another.
	if product.Quota > quota {
		product.Quota = quota
	}
	return product, nil
}
