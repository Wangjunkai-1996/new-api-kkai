package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantBillingFreezesCashTariffAndSettlesSubFenUsage(t *testing.T) {
	user := setupAuthSessionTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Tenant{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}, &model.TenantConsumption{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	oldOpen := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"tenant-supply":"Supply"}`))
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen)) })
	oldRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text":1}`))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "tenant-supply", Models: "tenant-text", Group: "tenant-supply"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Model: "tenant-text", Group: "tenant-supply", Enabled: true}).Error)
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		model.InvalidatePricingCache()
	})
	oldPrice, oldUnit, oldGroups := operation_setting.Price, common.QuotaPerUnit, ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		operation_setting.Price, common.QuotaPerUnit = oldPrice, oldUnit
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
	})
	operation_setting.Price, common.QuotaPerUnit = 1, 10000
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"tenant-supply":1}`))
	require.NoError(t, db.Model(user).Updates(map[string]any{"tenant_id": 42, "quota": 10000}).Error)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantCatalogEntry{TenantID: 42, Model: "tenant-text", SalesGroup: "tenant-supply", Status: model.TenantCatalogStatusEnabled, IsDefault: true}).Error)
	price := model.TenantPriceVersion{TenantID: 42, Group: "tenant-supply", Version: 1, SalesMultiplier: "2"}
	require.NoError(t, db.Create(&price).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "tenant-sub-fen", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 10000}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	_, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: user.Id, TopUpID: 1, OperationID: "sub-fen-payment", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 10000})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, RequestId: "sub-fen-request", OriginModelName: "tenant-text", Request: &dto.GeneralOpenAIRequest{Model: "tenant-text"}, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, PrepareTenantPricing(c, info))
	// A concurrent platform reprice must not change the request already quoted.
	operation_setting.Price = 20
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"tenant-supply":10}`))
	require.NoError(t, PrepareTenantPricing(c, info))
	require.Nil(t, preConsumeTenantBilling(c, info, 1))
	var quote model.PricingQuote
	require.NoError(t, db.First(&quote, info.TenantQuoteID).Error)
	assert.Equal(t, "0.01", quote.SaleMinorPerQuota)
	assert.Equal(t, "0.005", quote.SupplierMinorPerQuota)
	assert.Equal(t, "2", quote.SalesMultiplier)
	assert.Equal(t, "0.00484", quote.ProfitMinorExact)
	require.NoError(t, info.Billing.Settle(1))
	require.NoError(t, info.Billing.Settle(1))
	info.Billing.Refund(c) // A later transport error cannot refund committed usage.
	require.NoError(t, db.First(user, user.Id).Error)
	assert.EqualValues(t, 9999, user.Quota)
	assert.EqualValues(t, 1, user.UsedQuota)
	var wallet model.ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.EqualValues(t, "-0.005", wallet.ExactProfitCarry)
	var entries []model.ResellerLedgerEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 1)
	assert.Equal(t, "0.005", entries[0].SupplierMinorExact)
}

func TestTenantBillingInheritsPlatformPriceAndCarriesRealPaymentFeeLoss(t *testing.T) {
	user := setupAuthSessionTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Tenant{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}, &model.TenantConsumption{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	oldOpen, oldGroups, oldModels, oldPrice, oldUnit := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), operation_setting.Price, common.QuotaPerUnit
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		operation_setting.Price, common.QuotaPerUnit = oldPrice, oldUnit
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"inherited-text":1}`))
	operation_setting.Price, common.QuotaPerUnit = 1, 100
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "inherited", Models: "inherited-text", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Model: "inherited-text", Group: "default", Enabled: true}).Error)
	model.InvalidatePricingCache()
	require.NoError(t, db.Model(user).Updates(map[string]any{"tenant_id": 42, "quota": 100}).Error)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "inherited-price", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	_, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: user.Id, TopUpID: 1, OperationID: "inherited-payment", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 100})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, RequestId: "inherited-request", OriginModelName: "inherited-text", Request: &dto.GeneralOpenAIRequest{Model: "inherited-text"}, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, PrepareTenantPricing(c, info))
	assert.Equal(t, "2", info.TenantSalesMultiplier)
	assert.Zero(t, info.TenantPriceVersionID)
	// No per-model price or catalogue rows are needed, and an in-flight quote
	// remains exact even after the platform changes its current group ratio.
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))
	require.NoError(t, PrepareTenantPricing(c, info))
	assert.Equal(t, "2", info.TenantSalesMultiplier)
	require.Nil(t, preConsumeTenantBilling(c, info, 100))
	require.NoError(t, info.Billing.Settle(100))
	require.NoError(t, info.Billing.Settle(100))
	var quote model.PricingQuote
	require.NoError(t, db.First(&quote, info.TenantQuoteID).Error)
	assert.Equal(t, "-1.6", quote.ProfitMinorExact)
	var wallet model.ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.EqualValues(t, "-2", wallet.ExactProfitCarry)
	var ledger []model.ResellerLedgerEntry
	require.NoError(t, db.Find(&ledger).Error)
	require.Len(t, ledger, 1)
	assert.Equal(t, "100", ledger[0].SupplierMinorExact)
	assert.Equal(t, "-2", ledger[0].ExactProfitMinor)
	assert.Zero(t, ledger[0].AmountMinor)
	require.NoError(t, db.First(user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.EqualValues(t, 100, user.UsedQuota)
}
