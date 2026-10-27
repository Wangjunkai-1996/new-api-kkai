package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Only new quotations use this policy. Existing TopUp rows retain their stored
// Amount/Money and their existing provider-specific settlement semantics.
func topUpPromotionFactors(group string, amount int64) (float64, float64) {
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
