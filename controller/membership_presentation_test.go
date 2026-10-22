package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipPresentationAPIsExposePricesOnlyForAccessibleGroups(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, fixtureOrder := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
	previousGroups, previousUsable := ratio_setting.GroupRatio2JSONString(), setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2,"hidden":0.5}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"gpt":"GPT description"}`))
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
		Tiers:  []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, PriceMinor: 9900, Quota: 1000, Currency: "CNY", Enabled: true, Discounts: map[string]float64{"gpt": 0.95, "hidden": 0.5}}},
		Groups: []model.MembershipGroup{{Key: "gpt", Enabled: true, Published: true}, {Key: "hidden", Enabled: true, Published: true}},
	}, fixtureOrder.ReleaseID, 1)
	require.NoError(t, err)
	account := model.MembershipAccount{UserID: 7, TierID: "gold", Rank: 1, ReleaseID: catalog.ReleaseID, AmountTotal: 1000, AmountUsed: 100, AmountReserved: 20, StartedAt: common.GetTimestamp() - 60, ExpiresAt: common.GetTimestamp() + 3600, Status: model.MembershipActive}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&model.MembershipUserState{UserID: 7, CurrentAccountID: account.ID}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 7) })
	router.GET("/catalog", GetMembershipCatalog)
	router.GET("/self", GetMembershipSelf)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/catalog", nil))
	var saleResponse struct {
		Success bool `json:"success"`
		Data    struct {
			model.MembershipCatalog
			GroupQuotes []service.MembershipCatalogGroupQuote `json:"group_quotes"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saleResponse))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, saleResponse.Success)
	require.Len(t, saleResponse.Data.GroupQuotes, 1)
	assert.Equal(t, "gpt", saleResponse.Data.GroupQuotes[0].GroupKey)
	assert.Equal(t, 0.2, saleResponse.Data.GroupQuotes[0].BaseRatio)
	assert.InDelta(t, 0.19, saleResponse.Data.GroupQuotes[0].TierQuotes["gold"].MembershipRatio, 1e-9)
	require.Len(t, saleResponse.Data.Tiers, 1)
	assert.Equal(t, map[string]float64{"gpt": 0.95}, saleResponse.Data.Tiers[0].Discounts)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/self", nil))
	var selfResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Account        *model.MembershipAccount       `json:"account"`
			Tier           *model.MembershipTier          `json:"tier"`
			Discounts      []service.MembershipGroupQuote `json:"discounts"`
			AvailableQuota int64                          `json:"available_quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &selfResponse))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, selfResponse.Success)
	require.NotNil(t, selfResponse.Data.Tier)
	assert.Equal(t, map[string]float64{"gpt": 0.95}, selfResponse.Data.Tier.Discounts, "member terms must not leak inaccessible groups")
	require.Len(t, selfResponse.Data.Discounts, 1)
	assert.Equal(t, "gpt", selfResponse.Data.Discounts[0].GroupKey)
	assert.Equal(t, "GPT description", selfResponse.Data.Discounts[0].Description)
	assert.Equal(t, 0.2, selfResponse.Data.Discounts[0].BaseRatio)
	assert.InDelta(t, 0.19, selfResponse.Data.Discounts[0].MembershipRatio, 1e-9)
	assert.InDelta(t, 0.05, selfResponse.Data.Discounts[0].Savings, 1e-9)

	for _, coefficient := range []float64{0.5, 0.9} {
		catalog.Tiers[0].Discounts["gpt"] = coefficient
		catalog.Tiers[0].Name = "Updated Gold"
		catalog.Tiers[0].PriceMinor = 19900
		catalog.Tiers[0].Quota = 2000
		catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
		require.NoError(t, err)

		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/catalog", nil))
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saleResponse))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.True(t, saleResponse.Success)
		assert.Equal(t, catalog.ReleaseID, saleResponse.Data.ReleaseID)
		require.Len(t, saleResponse.Data.GroupQuotes, 1)
		assert.Equal(t, "gpt", saleResponse.Data.GroupQuotes[0].GroupKey)
		assert.InDelta(t, 0.2*coefficient, saleResponse.Data.GroupQuotes[0].TierQuotes["gold"].MembershipRatio, 1e-9)
		require.Len(t, saleResponse.Data.Tiers, 1)
		assert.Equal(t, map[string]float64{"gpt": coefficient}, saleResponse.Data.Tiers[0].Discounts)
		assert.EqualValues(t, 19900, saleResponse.Data.Tiers[0].PriceMinor)
		assert.EqualValues(t, 2000, saleResponse.Data.Tiers[0].Quota)

		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/self", nil))
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &selfResponse))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.True(t, selfResponse.Success)
		require.NotNil(t, selfResponse.Data.Tier)
		assert.Equal(t, map[string]float64{"gpt": coefficient}, selfResponse.Data.Tier.Discounts, "existing members receive the latest discounts without inaccessible group terms")
		assert.Equal(t, "Gold", selfResponse.Data.Tier.Name)
		assert.EqualValues(t, 9900, selfResponse.Data.Tier.PriceMinor, "publication must not rewrite the bought price")
		assert.EqualValues(t, 1000, selfResponse.Data.Tier.Quota, "publication must not rewrite the bought quota")
		require.NotNil(t, selfResponse.Data.Account)
		assert.Equal(t, account, *selfResponse.Data.Account, "publication must preserve balances, purchase release and expiry")
		assert.EqualValues(t, 880, selfResponse.Data.AvailableQuota)
		require.Len(t, selfResponse.Data.Discounts, 1)
		assert.Equal(t, "gpt", selfResponse.Data.Discounts[0].GroupKey)
		assert.InDelta(t, 0.2*coefficient, selfResponse.Data.Discounts[0].MembershipRatio, 1e-9)
		assert.InDelta(t, 1-coefficient, selfResponse.Data.Discounts[0].Savings, 1e-9)
	}
}
