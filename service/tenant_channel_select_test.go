package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantChannelSelectionRejectsUnverifiedTransports(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.Model{}, &model.Vendor{}))
	const group, modelName = "tenant-supply", "shared-model"
	oldOpen, oldGroups, oldModels := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"tenant-supply":"Public supply"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"tenant-supply":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"shared-model":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.Create(&model.Tenant{ID: 7, Status: model.TenantStatusActive}).Error)
	sdk := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeAws, 100)
	seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeXunfei, 90)
	safe := seedImageStudioSelectionChannel(t, db, group, modelName, constant.ChannelTypeOpenAI, 10)
	model.InvalidatePricingCache()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache_%t", cached), func(t *testing.T) {
			common.MemoryCacheEnabled = cached
			if cached {
				require.NoError(t, model.SyncChannelCacheOnce())
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			constraints := GetChannelConstraints(c)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(7))
			retry := &RetryParam{Ctx: c, TokenGroup: group, ModelName: modelName, RequestPath: c.Request.URL.Path}
			selected, _, err := SelectChannelForRequest(c, modelName, retry)
			require.Nil(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, safe.Id, selected.Id, "unsafe higher-priority siblings cannot receive tenant traffic")
			selected, _, randomErr := CacheGetRandomSatisfiedChannel(retry)
			require.NoError(t, randomErr)
			require.NotNil(t, selected)
			assert.Equal(t, safe.Id, selected.Id, "direct retry selection retains the transport filter")
			constraints.AddPin(dto.ChannelPin{ChannelId: sdk.Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken})
			selected, _, err = SelectChannelForRequest(c, modelName, retry)
			assert.Nil(t, selected)
			require.NotNil(t, err)
			assert.Equal(t, dto.FilterAllowedChannelTypes, err.FilterKind)
		})
	}
}

func TestTenantChannelSelectionUsesRequestedPublicGroup(t *testing.T) {
	db := newImageStudioChannelSelectionTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.Model{}, &model.Vendor{}))
	oldOpen, oldGroups, oldModels, oldProfiles := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), setting.AutoGroupProfiles2JsonString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium","auto":"Auto","auto2":"Auto 2"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":2,"private":0.5,"auto":1,"auto2":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"shared-model":1,"premium-only":1}`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["default","premium"]}`))
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(oldProfiles))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.Create(&model.Tenant{ID: 7, Status: model.TenantStatusActive}).Error)
	channels := map[string]int{}
	for _, group := range []string{"default", "premium", "private", "auto", "auto2"} {
		channel := seedImageStudioSelectionChannel(t, db, group, "shared-model", constant.ChannelTypeOpenAI, 10)
		channels[group] = channel.Id
	}
	premiumOnly := seedImageStudioSelectionChannel(t, db, "premium", "premium-only", constant.ChannelTypeOpenAI, 10)
	model.InvalidatePricingCache()
	for _, tc := range []struct {
		name, model, requested, selected string
		channelID                        int
	}{
		{name: "inherited default", model: "shared-model", selected: "default", channelID: channels["default"]},
		{name: "explicit default", model: "shared-model", requested: "default", selected: "default", channelID: channels["default"]},
		{name: "explicit premium", model: "shared-model", requested: "premium", selected: "premium", channelID: channels["premium"]},
		{name: "premium exclusive", model: "premium-only", requested: "premium", selected: "premium", channelID: premiumOnly.Id},
		{name: "no silent group change", model: "premium-only"},
		{name: "private group", model: "shared-model", requested: "private"},
		{name: "unknown group", model: "shared-model", requested: "missing"},
		{name: "auto group", model: "shared-model", requested: "auto"},
		{name: "auto profile", model: "shared-model", requested: "auto2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(7))
			retry := &RetryParam{Ctx: c, TokenGroup: tc.requested, ModelName: tc.model, RequestPath: c.Request.URL.Path}
			channel, _, err := SelectChannelForRequest(c, tc.model, retry)
			if tc.channelID == 0 {
				assert.Nil(t, channel)
				require.NotNil(t, err)
				assert.Equal(t, http.StatusForbidden, err.StatusCode)
				assert.Equal(t, tc.requested, retry.TokenGroup, "a rejected group must not fall back to another group")
				return
			}
			require.Nil(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, tc.channelID, channel.Id)
			assert.Equal(t, tc.selected, retry.TokenGroup)
			assert.Equal(t, tc.selected, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		})
	}
}
