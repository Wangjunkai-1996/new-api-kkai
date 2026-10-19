package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantCatalogExcludesUnsupportedProviderEndpoints(t *testing.T) {
	setupAuthSessionTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.Tenant{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}))
	oldRatios, oldGroups, oldPrice := ratio_setting.ModelRatio2JSONString(), ratio_setting.GroupRatio2JSONString(), operation_setting.Price
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"plain-model":1,"opaque-media":1,"opaque-video":1,"embedding-model":1,"sdk-only":1,"socket-only":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	operation_setting.Price = 1
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		operation_setting.Price = oldPrice
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.Create(&model.Tenant{ID: 7, Status: model.TenantStatusActive}).Error)
	for i, name := range []string{"plain-model", "opaque-media", "opaque-video", "embedding-model", "sdk-only", "socket-only"} {
		channelType := constant.ChannelTypeOpenAI
		switch name {
		case "opaque-video":
			channelType = constant.ChannelTypeSora
		case "sdk-only":
			channelType = constant.ChannelTypeAws
		case "socket-only":
			channelType = constant.ChannelTypeXunfei
		}
		require.NoError(t, db.Create(&model.Channel{Id: i + 1, Type: channelType, Status: common.ChannelStatusEnabled, Name: name, Models: name, Group: "default"}).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: i + 1, Model: name, Group: "default", Enabled: true}).Error)
		require.NoError(t, db.Create(&model.TenantCatalogEntry{TenantID: 7, Model: name, SalesGroup: "default", Status: model.TenantCatalogStatusEnabled, IsDefault: i == 0}).Error)
		require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 7, Model: name, Group: "default", Version: 1, SalesMultiplier: "2"}).Error)
	}
	// The same model may have both transports. Only the safe group/model
	// combination is supply, while unsafe siblings do not hide the safe one.
	require.NoError(t, db.Create(&model.Channel{Id: 10, Type: constant.ChannelTypeAws, Status: common.ChannelStatusEnabled, Name: "mixed-sdk", Models: "plain-model", Group: "default,sdk-group"}).Error)
	require.NoError(t, db.Create(&[]model.Ability{{ChannelId: 10, Model: "plain-model", Group: "default", Enabled: true}, {ChannelId: 10, Model: "plain-model", Group: "sdk-group", Enabled: true}}).Error)
	// The opaque name is insufficient to identify media; operator metadata is
	// authoritative even when the channel also advertises OpenAI compatibility.
	require.NoError(t, db.Create(&model.Model{ModelName: "opaque-media", Status: 1, Endpoints: `{"image-generation":"/v1/images/generations"}`}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "embedding-model", Status: 1, Endpoints: `{"embeddings":"/v1/embeddings"}`}).Error)
	supply, err := ListTenantSupply(db)
	require.NoError(t, err)
	names := make([]string, 0, len(supply))
	for _, entry := range supply {
		names = append(names, entry.Model)
		assert.Equal(t, "default", entry.SalesGroup)
	}
	assert.ElementsMatch(t, []string{"plain-model", "embedding-model"}, names)
	pricing := NewTenantPricingService(db)
	assert.ErrorIs(t, pricing.SaveCatalog(context.Background(), 7, []model.TenantCatalogEntry{{TenantID: 7, Model: "opaque-media", SalesGroup: "default", Status: model.TenantCatalogStatusEnabled, IsDefault: true}}), model.ErrResellerInvalidCatalog)
	_, err = pricing.ResolveCurrentPrice(context.Background(), 7, "opaque-media", "default")
	assert.ErrorIs(t, err, model.ErrResellerInvalidCatalog)
	visible, err := pricing.ResolveCustomerPrices(context.Background(), 7)
	require.NoError(t, err)
	names = names[:0]
	for _, entry := range visible {
		names = append(names, entry.Model)
	}
	assert.ElementsMatch(t, []string{"plain-model", "embedding-model"}, names)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(7))
	quoted := &relaycommon.RelayInfo{OriginModelName: "plain-model", Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, PrepareTenantPricing(c, quoted))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))
	effective, err := EffectiveTenantMultiplier("2", "default")
	require.NoError(t, err)
	assert.True(t, effective.Mul(decimal.RequireFromString("0.984")).GreaterThanOrEqual(decimal.NewFromInt(3)))
	assert.True(t, effective.Sub(decimal.New(1, -18)).Mul(decimal.RequireFromString("0.984")).LessThan(decimal.NewFromInt(3)), "the supply floor rounds upwards to the nearest 18th decimal")
	current, err := pricing.ResolveCurrentPrice(context.Background(), 7, "plain-model", "default")
	require.NoError(t, err)
	assert.Equal(t, effective.String(), current.SalesMultiplier)
	visible, err = pricing.ResolveCustomerPrices(context.Background(), 7)
	require.NoError(t, err)
	for _, price := range visible {
		ratio, _ := effective.Float64()
		assert.Equal(t, ratio, price.SalesRatio)
	}
	var stored model.TenantPriceVersion
	require.NoError(t, db.First(&stored, current.ID).Error)
	assert.Equal(t, "2", stored.SalesMultiplier, "live supply changes cannot rewrite historical price versions")
	assert.ErrorIs(t, pricing.ValidateMinimumPrice("2", "default"), model.ErrResellerPriceBelowCost)
	updated := &relaycommon.RelayInfo{OriginModelName: "plain-model", Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, PrepareTenantPricing(c, updated))
	assert.Equal(t, effective.String(), updated.TenantSalesMultiplier)
	require.NoError(t, PrepareTenantPricing(c, quoted))
	assert.Equal(t, "2", quoted.TenantSalesMultiplier)
	require.NoError(t, db.Create(&model.Model{ModelName: "plain-model", Status: 1, Endpoints: `{"image-generation":"/v1/images/generations"}`}).Error)
	model.InvalidatePricingCache()
	assert.ErrorIs(t, PrepareTenantPricing(c, &relaycommon.RelayInfo{OriginModelName: "plain-model", Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}), model.ErrResellerInvalidCatalog)
	assert.NoError(t, PrepareTenantPricing(c, quoted), "retiring a model cannot rewrite an accepted pricing quote")
}
