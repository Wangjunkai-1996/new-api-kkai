package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerEarningsEstimateUsesInheritedDefaultGroup(t *testing.T) {
	db := resellerAdminFixture(t)
	require.NoError(t, db.AutoMigrate(&model.TenantMembership{}, &model.TenantPriceVersion{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	oldOpen, oldGroups, oldModels, oldPrice := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), operation_setting.Price
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		operation_setting.Price = oldPrice
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"earnings-text":1}`))
	operation_setting.Price = 1
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "earnings", Group: "default", Models: "earnings-text"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Group: "default", Model: "earnings-text", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TenantMembership{TenantID: 42, UserID: 7, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	_, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: 8, TopUpID: 1, OperationID: "earnings-payment", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 100})
	require.NoError(t, err)
	model.InvalidatePricingCache()
	router := gin.New()
	router.GET("/earnings", func(c *gin.Context) {
		c.Set("id", 7)
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42})
	}, middleware.TenantAdminAuth(), GetResellerEarnings)
	for _, test := range []struct {
		multiplier string
		want       int64
	}{{"", -2}, {"2", 48}, {"0", 0}} {
		if test.multiplier != "" {
			if test.multiplier == "0" {
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0}`))
			}
			require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 42, Group: "default", Version: 1, SalesMultiplier: test.multiplier}).Error)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/earnings", nil))
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Estimate  int64 `json:"estimated_profit_minor"`
				Reference bool  `json:"estimate_is_reference"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		assert.Equal(t, test.want, result.Data.Estimate)
		assert.True(t, result.Data.Reference)
		// Keep each scenario independent while preserving all production records.
		require.NoError(t, db.Where("tenant_id = ?", 42).Delete(&model.TenantPriceVersion{}).Error)
	}
}
