package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipLaunchRemovesOnlyNewTopUpPromotions(t *testing.T) {
	db, _ := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
	previousRatio := common.TopupGroupRatio2JSONString()
	previousDiscount, previousQuota := operation_setting.GetPaymentSetting().AmountDiscount, common.QuotaPerUnit
	t.Cleanup(func() {
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(previousRatio))
		operation_setting.GetPaymentSetting().AmountDiscount = previousDiscount
		common.QuotaPerUnit = previousQuota
	})
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"loyal":0.8}`))
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.9}
	common.QuotaPerUnit = 500000
	ratio, discount := topUpPromotionFactors("loyal", 100)
	assert.Equal(t, 0.8, ratio)
	assert.Equal(t, 0.9, discount)
	product := CreemProduct{Price: 10, Currency: "USD", Quota: 6000000}
	unchanged, err := creemProductWithoutBonus(product)
	require.NoError(t, err)
	assert.EqualValues(t, 6000000, unchanged.Quota)
	require.NoError(t, db.Model(&model.MembershipCatalogState{}).Where("id = ?", 1).Update("topup_discounts_disabled", true).Error)
	ratio, discount = topUpPromotionFactors("loyal", 100)
	assert.Equal(t, 1.0, ratio)
	assert.Equal(t, 1.0, discount)
	updated, err := creemProductWithoutBonus(product)
	require.NoError(t, err)
	assert.EqualValues(t, 5000000, updated.Quota)
	assert.EqualValues(t, 6000000, product.Quota)
	assert.Equal(t, 100.0, GetChargedAmount(100, model.User{Group: "loyal"}))
	assert.EqualValues(t, 50000000, getStripeCreditedQuota(100, "loyal").IntPart())
	// Pausing sales does not restore top-up promotions.
	require.NoError(t, db.Model(&model.MembershipCatalogState{}).Where("id = ?", 1).Update("sales_enabled", false).Error)
	ratio, discount = topUpPromotionFactors("loyal", 100)
	assert.Equal(t, 1.0, ratio)
	assert.Equal(t, 1.0, discount)
}
