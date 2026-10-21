package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func tenantMarkupFixture(t *testing.T) *TenantPricingService {
	t.Helper()
	setupAuthSessionTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.Tenant{}, &model.TenantPriceVersion{}))
	oldOpen, oldGroups, oldModels, oldPrice := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), operation_setting.Price
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		operation_setting.Price = oldPrice
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2.5}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"markup-text":1}`))
	operation_setting.Price = 1
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "markup", Models: "markup-text", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Model: "markup-text", Group: "default", Enabled: true}).Error)
	model.InvalidatePricingCache()
	return NewTenantPricingService(db)
}

func TestTenantMarkupTracksPlatformAndFreezesAcceptedRequests(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	percent := "20"
	groups, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "3", groups[0].SalesMultiplier)
	assert.Equal(t, "20", groups[0].MarkupPercent)
	assert.Equal(t, "markup", groups[0].PricingMode)
	assert.False(t, groups[0].Inherited)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	accepted := &relaycommon.RelayInfo{OriginModelName: "markup-text", Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, PrepareTenantPricing(c, accepted))
	for _, step := range []struct{ platform, sale string }{{"3", "3.6"}, {"1.5", "1.8"}} {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":`+step.platform+`}`))
		price, err := pricing.ResolveCurrentPrice(ctx, 42, "markup-text", "default")
		require.NoError(t, err)
		assert.Equal(t, step.sale, price.SalesMultiplier)
		visible, err := pricing.ResolveCustomerPrices(ctx, 42)
		require.NoError(t, err)
		require.Len(t, visible, 1)
		wanted, err := strconv.ParseFloat(step.sale, 64)
		require.NoError(t, err)
		assert.Equal(t, wanted, visible[0].SalesRatio)
		require.NoError(t, PrepareTenantPricing(c, accepted))
		assert.Equal(t, "3", accepted.TenantSalesMultiplier)
	}
	percent = "40"
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent, ExpectedVersion: 1}})
	require.NoError(t, err)
	var historical model.TenantPriceVersion
	require.NoError(t, model.DB.First(&historical, groups[0].ID).Error)
	assert.Equal(t, "markup:6/5", historical.SalesMultiplier)
	require.NoError(t, PrepareTenantPricing(c, accepted))
	assert.Equal(t, "3", accepted.TenantSalesMultiplier)
}

func TestTenantMarkupConvertsLegacyExactlyAndKeepsHistory(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5}`))
	legacy := model.TenantPriceVersion{TenantID: 42, Group: "default", Version: 1, SalesMultiplier: "2"}
	require.NoError(t, model.DB.Create(&legacy).Error)
	before, err := pricing.ListGroupPrices(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "legacy", before[0].PricingMode)
	assert.Equal(t, "33.333333333333333333", before[0].MarkupPercent)
	converted, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: "1.5", ExpectedVersion: 1}})
	require.NoError(t, err)
	assert.Equal(t, "2", converted[0].SalesMultiplier, "conversion must not round-trip through the displayed repeating percentage")
	assert.Equal(t, "markup", converted[0].PricingMode)
	var versions []model.TenantPriceVersion
	require.NoError(t, model.DB.Order("version").Find(&versions).Error)
	require.Len(t, versions, 2)
	assert.Equal(t, "2", versions[0].SalesMultiplier)
	assert.Equal(t, "markup:4/3", versions[1].SalesMultiplier)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))
	price, err := pricing.ResolveCurrentPrice(ctx, 42, "markup-text", "default")
	require.NoError(t, err)
	assert.Equal(t, "4", price.SalesMultiplier)
	zero := "0"
	reset, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &zero, ExpectedVersion: 2}})
	require.NoError(t, err)
	assert.True(t, reset[0].Inherited)
	assert.Equal(t, "0", reset[0].MarkupPercent)
	assert.Equal(t, "3", reset[0].SalesMultiplier)
}

func TestTenantMarkupZeroPlatformAndLegacyPositivePrice(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0}`))
	percent := "20"
	groups, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}})
	require.NoError(t, err)
	assert.Equal(t, "0", groups[0].SalesMultiplier)
	assert.Equal(t, "20", groups[0].MarkupPercent)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2.5}`))
	price, err := pricing.ResolveCurrentPrice(ctx, 42, "markup-text", "default")
	require.NoError(t, err)
	assert.Equal(t, "3", price.SalesMultiplier)
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", Multiplier: "3", ExpectedVersion: 1}})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0}`))
	groups, err = pricing.ListGroupPrices(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "3", groups[0].SalesMultiplier)
	assert.Empty(t, groups[0].MarkupPercent)
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: "0", ExpectedVersion: 2}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	var count int64
	require.NoError(t, model.DB.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func TestTenantMarkupInvalidUpdatesLeavePricesUntouched(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	for _, value := range []string{"", "-1", "NaN", "Infinity", "1000001", "1e-19", "1e7", strings.Repeat("1", 65)} {
		t.Run(value, func(t *testing.T) {
			_, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &value}})
			assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
		})
	}
	percent := "20"
	_, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent, Multiplier: "3"}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}, {Group: "default", MarkupPercent: &percent}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1000000}`))
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	var count int64
	require.NoError(t, model.DB.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count)

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5}`))
	require.NoError(t, model.DB.Create(&model.TenantPriceVersion{TenantID: 42, Group: "default", Version: 1, SalesMultiplier: "2"}).Error)
	for _, update := range []TenantPriceUpdate{
		{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: "2", ExpectedVersion: 1},
		{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: "1.5", ExpectedVersion: 0},
	} {
		_, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{update})
		assert.ErrorIs(t, err, ErrTenantPriceVersionConflict)
	}
	for _, expected := range []string{"", "NaN", "-1"} {
		_, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: expected, ExpectedVersion: 1}})
		assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	}
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0.000000000000000001}`))
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", ConvertLegacy: true, ExpectedPlatformMultiplier: "0.000000000000000001", ExpectedVersion: 1}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice, "an unrepresentable legacy markup must not be silently changed")
	require.NoError(t, model.DB.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestTenantMarkupPreservesVerySmallPlatformPrices(t *testing.T) {
	for _, test := range []struct{ platform, expected string }{
		{"1e-18", "0.0000000000000000012"},
		{"1e-19", "0.00000000000000000012"},
	} {
		t.Run(test.platform, func(t *testing.T) {
			pricing := tenantMarkupFixture(t)
			require.NoError(t, model.DB.Create(&model.TenantPriceVersion{TenantID: 42, Group: "default", Version: 1, SalesMultiplier: "2"}).Error)
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":`+test.platform+`}`))
			percent := "20"
			groups, err := pricing.SaveGroupPrices(context.Background(), 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent, ExpectedVersion: 1}})
			require.NoError(t, err)
			assert.Equal(t, test.expected, groups[0].SalesMultiplier)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
			info := &relaycommon.RelayInfo{OriginModelName: "markup-text", Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
			require.NoError(t, PrepareTenantPricing(c, info), "supported fractional markup must reach request pricing")
			assert.Equal(t, test.expected, info.TenantSalesMultiplier)
		})
	}
}

func TestTenantMarkupPreservesPercentagePrecision(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	percent := "0.000000000000000001"
	groups, err := pricing.SaveGroupPrices(context.Background(), 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}})
	require.NoError(t, err)
	assert.Equal(t, "2.500000000000000000025", groups[0].SalesMultiplier)
}

func TestTenantMarkupCanResetAfterPlatformIncreaseExceedsPriceLimit(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":500000}`))
	percent := "100"
	groups, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent}})
	require.NoError(t, err)
	assert.Equal(t, "1000000", groups[0].SalesMultiplier)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":600000}`))
	_, err = pricing.ListGroupPrices(ctx, 42)
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	percent = "0"
	groups, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent, ExpectedVersion: 1}})
	require.NoError(t, err, "an invalid current retail price must not prevent a valid correction")
	assert.Equal(t, "600000", groups[0].SalesMultiplier)
	assert.True(t, groups[0].Inherited)
	_, err = pricing.ListGroupPrices(ctx, 42)
	assert.NoError(t, err)
}

func TestTenantMarkupCorrectionRollsBackIfAnotherGroupIsInvalid(t *testing.T) {
	pricing := tenantMarkupFixture(t)
	ctx := context.Background()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":500000,"premium":500000}`))
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("group", "default,premium").Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: 1, Model: "markup-text", Group: "premium", Enabled: true}).Error)
	model.InvalidatePricingCache()
	percent := "100"
	_, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{
		{Group: "default", MarkupPercent: &percent},
		{Group: "premium", MarkupPercent: &percent},
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":600000,"premium":600000}`))
	percent = "0"
	_, err = pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{{Group: "default", MarkupPercent: &percent, ExpectedVersion: 1}})
	assert.ErrorIs(t, err, model.ErrResellerInvalidPrice)
	var count int64
	require.NoError(t, model.DB.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "an error response cannot commit a partial correction")
	groups, err := pricing.SaveGroupPrices(ctx, 42, []TenantPriceUpdate{
		{Group: "default", MarkupPercent: &percent, ExpectedVersion: 1},
		{Group: "premium", MarkupPercent: &percent, ExpectedVersion: 1},
	})
	require.NoError(t, err)
	require.Len(t, groups, 2)
	for _, group := range groups {
		assert.Equal(t, "600000", group.SalesMultiplier)
		assert.EqualValues(t, 2, group.Version)
	}
}
