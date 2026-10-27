package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipSalesPreserveTopUpPromotions(t *testing.T) {
	db, _ := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
	previousRatio := common.TopupGroupRatio2JSONString()
	previousDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	previousDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(previousRatio))
		operation_setting.GetPaymentSetting().AmountDiscount = previousDiscount
		operation_setting.GetGeneralSetting().QuotaDisplayType = previousDisplay
	})
	for target, value := range map[*float64]float64{
		&common.QuotaPerUnit: 500000, &operation_setting.Price: 1,
		&setting.StripeUnitPrice: 1, &setting.WaffoUnitPrice: 1, &setting.WaffoPancakeUnitPrice: 1,
	} {
		previous := *target
		*target = value
		t.Cleanup(func() { *target = previous })
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"loyal":0.8}`))
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.9}
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	for _, sales := range []bool{true, false} {
		require.NoError(t, db.Model(&model.MembershipCatalogState{}).Where("id = ?", 1).Updates(map[string]any{"sales_enabled": sales, "topup_discounts_disabled": true}).Error)
		assert.InDelta(t, 72, getPayMoney(100, "loyal"), 1e-9)
		assert.InDelta(t, 72, getStripePayMoney(100, "loyal"), 1e-9)
		assert.InDelta(t, 72, getWaffoPayMoney(100, "loyal"), 1e-9)
		assert.InDelta(t, 72, getWaffoPancakePayMoney(100, "loyal"), 1e-9)
		assert.Equal(t, 80.0, GetChargedAmount(100, model.User{Group: "loyal"}))
		assert.EqualValues(t, 40000000, getStripeCreditedQuota(100, "loyal").IntPart())
		assert.Equal(t, 100.0, getPayMoneyDecimal(100, "loyal", true).InexactFloat64(), "tenant pricing remains independent")
	}
}

func TestMembershipLegacyFlagPreservesTopUpInfoAndCreemBonus(t *testing.T) {
	db := walletCallbackDB(t)
	enableWalletLegacyPayments(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("c", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	t.Setenv("WALLET_CREEM_ACCOUNT_REF", "test-merchant")
	previousSecret := setting.CreemWebhookSecret
	previousQuota := common.QuotaPerUnit
	previousDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		setting.CreemWebhookSecret = previousSecret
		common.QuotaPerUnit = previousQuota
		operation_setting.GetPaymentSetting().AmountDiscount = previousDiscount
	})
	common.QuotaPerUnit = 500000
	setting.CreemWebhookSecret = "test-webhook-secret"
	setting.CreemProducts = `[{"productId":"prod_bonus","price":10,"currency":"USD","quota":6000000}]`
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.98, 1000: 0.9}
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "wallet-promotions", Group: "default"}).Error)
	require.NoError(t, db.Save(&model.MembershipCatalogState{ID: 1, SalesEnabled: true, TopupDiscountsDisabled: true}).Error)

	response := invokeWalletCompatibility(t, GetTopUpInfo, "")
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Discount               map[int]float64 `json:"discount"`
			CreemProducts          string          `json:"creem_products"`
			TopupDiscountsDisabled bool            `json:"topup_discounts_disabled"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, operation_setting.GetPaymentSetting().AmountDiscount, result.Data.Discount)
	assert.Equal(t, setting.CreemProducts, result.Data.CreemProducts)
	assert.False(t, result.Data.TopupDiscountsDisabled)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/wallet/quote", nil)
	ctx.Set("id", 7)
	quote, _, err := walletQuote(ctx, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderCreem, PaymentMethod: model.PaymentMethodCreem, ProductID: "prod_bonus"})
	require.NoError(t, err)
	assert.EqualValues(t, 6000000, quote.CreditedQuota)
	assert.EqualValues(t, 1000, quote.ExpectedMinor)
	assert.Equal(t, "USD", quote.Currency)

	tenantResponse := httptest.NewRecorder()
	tenantCtx, _ := gin.CreateTestContext(tenantResponse)
	tenantCtx.Request = httptest.NewRequest(http.MethodGet, "/topup/info", nil)
	common.SetContextKey(tenantCtx, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42})
	GetTopUpInfo(tenantCtx)
	require.Equal(t, http.StatusOK, tenantResponse.Code)
	result.Data.Discount = nil
	require.NoError(t, common.Unmarshal(tenantResponse.Body.Bytes(), &result))
	assert.Empty(t, result.Data.Discount)
	assert.True(t, result.Data.TopupDiscountsDisabled)
}
