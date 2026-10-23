package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletFundingSummaryContractIsExactAndScoped(t *testing.T) {
	db := walletCallbackDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIInternalBalanceAdjustment{}))
	require.NoError(t, db.Create(&model.User{Id: 61, Username: "funding-summary-user", AffCode: "funding-summary-user"}).Error)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	previousQuotaUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	settings := operation_setting.GetGeneralSetting()
	previousDisplay := settings.QuotaDisplayType
	settings.QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		common.QuotaPerUnit = previousQuotaUnit
		settings.QuotaDisplayType = previousDisplay
	})
	now := common.GetTimestamp()
	order := model.TopUp{UserId: 61, TradeNo: "exact-payment", Status: common.TopUpStatusSuccess, WalletLifecycleVersion: 1, PaymentProvider: model.PaymentProviderStripe, ProviderAccountRef: "private-merchant", ProviderEnvironment: "prod", Currency: "USD", CurrencyExponent: 2}
	require.NoError(t, db.Create(&order).Error)
	require.NoError(t, db.Create(&model.WalletPaymentReceipt{ReceiptKey: "private-receipt", TopUpID: &order.Id, Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment, TransactionID: "private-transaction", AmountMinor: 9007199254740993, Currency: "USD", CurrencyExponent: 2, PaidAt: now, EvidenceRef: "private-evidence", VerificationStatus: "verified", ProcessingStatus: "credited", PricingVerified: true}).Error)
	walletDelta := int64(9000000)
	require.NoError(t, db.Create(&model.KKAIInternalBalanceAdjustment{OperationID: "invitation-summary", UserID: 61, Delta: 999999, WalletDelta: &walletDelta, SourceEpoch: common.CreditEpochLegacy, Reason: model.KKAIBalanceAdjustmentReasonCredit}).Error)

	for _, tenantID := range []int64{0, 6} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", 61)
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID})
		c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/summary?user_id=99&tenant_id=99", nil)
		GetWalletFundingSummary(c)
		assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		assert.NotContains(t, recorder.Body.String(), "private-")
		if tenantID != 0 {
			assert.Equal(t, http.StatusNotFound, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), "cash_payments")
			continue
		}
		assert.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool                     `json:"success"`
			Data    dto.WalletFundingSummary `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		assert.Equal(t, []dto.WalletMoney{{AmountMinor: "9007199254740993", Currency: "USD", Exponent: 2}}, response.Data.CashPayments)
		assert.EqualValues(t, 1, response.Data.ConfirmedPaymentCount)
		assert.Zero(t, response.Data.UnknownPaymentCount)
		assert.Equal(t, "9000000", response.Data.RebateCreditedQuota)
		assert.Equal(t, dto.WalletCreditDisplay{Amount: "18", Unit: "USD"}, response.Data.RebateCreditDisplay)
		assert.False(t, response.Data.RefundAdjusted)
	}
	// The existing history controller forwards the new all-time scope instead
	// of silently falling back to its default 30-day legacy query.
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 61)
	common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{})
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/self?scope=all&status_group=completed", nil)
	GetUserTopUps(c)
	assert.Equal(t, http.StatusOK, recorder.Code)
	var history struct {
		Success bool `json:"success"`
		Data    struct {
			QueryScope string            `json:"query_scope"`
			Total      int64             `json:"total"`
			Items      []dto.WalletOrder `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &history))
	require.True(t, history.Success)
	assert.Equal(t, "all", history.Data.QueryScope)
	assert.EqualValues(t, 1, history.Data.Total)
	require.Len(t, history.Data.Items, 1)
	require.NotNil(t, history.Data.Items[0].ReceivedPayment)
	assert.Equal(t, "9007199254740993", history.Data.Items[0].ReceivedPayment.AmountMinor)
}
