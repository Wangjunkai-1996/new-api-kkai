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
	db, _ := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
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
	}, 0, 1)
	require.NoError(t, err)
	account := model.MembershipAccount{UserID: 7, TierID: "gold", Rank: 1, ReleaseID: catalog.ReleaseID, AmountTotal: 1000, ExpiresAt: common.GetTimestamp() + 3600, Status: model.MembershipActive}
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
			Tier      *model.MembershipTier          `json:"tier"`
			Discounts []service.MembershipGroupQuote `json:"discounts"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &selfResponse))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, selfResponse.Success)
	require.NotNil(t, selfResponse.Data.Tier)
	assert.Equal(t, map[string]float64{"gpt": 0.95}, selfResponse.Data.Tier.Discounts, "historical snapshot must not leak inaccessible group terms")
	require.Len(t, selfResponse.Data.Discounts, 1)
	assert.Equal(t, "gpt", selfResponse.Data.Discounts[0].GroupKey)
	assert.Equal(t, "GPT description", selfResponse.Data.Discounts[0].Description)
	assert.Equal(t, 0.2, selfResponse.Data.Discounts[0].BaseRatio)
	assert.InDelta(t, 0.19, selfResponse.Data.Discounts[0].MembershipRatio, 1e-9)
	assert.InDelta(t, 0.05, selfResponse.Data.Discounts[0].Savings, 1e-9)
}
