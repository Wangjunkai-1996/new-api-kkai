package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipCatalogQuotesUseActualWalletPricesAndLatestTerms(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	previousUsable, previousSpecial := setting.UserUsableGroups2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	previousAuto := setting.AutoGroupProfiles2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousSpecial))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousAuto))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2,"special":0.2,"full":0.5,"free":0,"unpublished":0.5,"disabled":0.5,"hidden":0.5,"auto":1,"auto2":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"default":{"special":0.16,"full":0.7}}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"gpt":"GPT description","special":"Existing discount","full":"No extra discount","free":"Free group","unpublished":"Unpublished","disabled":"Disabled","removed":"Removed","auto":"Auto","auto2":"Auto 2"}`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["gpt"]}`))
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.SalesEnabled = false
	catalog.Groups = []model.MembershipGroup{
		{Key: "gpt", Enabled: true, Published: true},
		{Key: "special", Enabled: true, Published: true},
		{Key: "full", Enabled: true, Published: true},
		{Key: "free", Enabled: true, Published: true},
		{Key: "unpublished", Enabled: true},
		{Key: "disabled", Published: true},
		{Key: "hidden", Enabled: true, Published: true},
	}
	catalog.Tiers[0].Discounts = map[string]float64{"gpt": 0.25, "special": 0.95, "full": 1, "free": 0.5, "unpublished": 0.1, "disabled": 0.5, "hidden": 0.5}
	diamond := catalog.Tiers[0]
	diamond.ID, diamond.Rank = "diamond", 2
	diamond.Discounts = map[string]float64{"gpt": 0.2, "special": 0.9, "full": 0.8, "free": 0.3, "unpublished": 0.1, "disabled": 0.5, "hidden": 0.5}
	retired := diamond
	retired.ID, retired.Rank, retired.Enabled = "retired", 3, false
	catalog.Tiers = append(catalog.Tiers, diamond, retired)
	catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)

	quotes, err := GetMembershipCatalogGroupQuotes(user.Id, catalog)
	require.NoError(t, err)
	require.Len(t, quotes, 5, "exclude hidden, disabled, removed and virtual routing groups")
	byGroup := make(map[string]MembershipCatalogGroupQuote, len(quotes))
	for _, quote := range quotes {
		byGroup[quote.GroupKey] = quote
		require.Len(t, quote.TierQuotes, 2, "quote each enabled tier without advertising disabled tiers")
		assert.NotContains(t, quote.TierQuotes, "retired")
	}
	assert.Equal(t, "GPT description", byGroup["gpt"].Description)
	assert.Equal(t, 0.2, byGroup["gpt"].BaseRatio)
	assert.Equal(t, 0.2, byGroup["gpt"].WalletRatio)
	assert.InDelta(t, 0.04, byGroup["gpt"].TierQuotes["diamond"].MembershipRatio, 1e-9)
	assert.InDelta(t, 0.8, byGroup["gpt"].TierQuotes["diamond"].Savings, 1e-9)
	for _, tc := range []struct {
		group                       string
		coefficient, ratio, savings float64
	}{
		{"gpt", 0.25, 0.05, 0.75},
		{"special", 0.95, 0.16, 0},
		{"full", 1, 0.7, 0},
		{"free", 0.5, 0, 0},
		{"unpublished", 1, 0.5, 0},
	} {
		t.Run(tc.group, func(t *testing.T) {
			row, exists := byGroup[tc.group]
			require.True(t, exists)
			quote := row.TierQuotes["gold"]
			assert.Equal(t, tc.coefficient, quote.Coefficient)
			assert.InDelta(t, tc.ratio, quote.MembershipRatio, 1e-9)
			assert.InDelta(t, tc.savings, quote.Savings, 1e-9)
		})
	}

	locked, err := GetMembershipGroupQuotes(user.Id, catalog)
	require.NoError(t, err)
	require.Len(t, locked, 5)
	for _, quote := range locked {
		if quote.GroupKey == "gpt" {
			assert.Equal(t, 0.5, quote.Coefficient, "sale changes must not rewrite a bought entitlement")
			assert.Equal(t, 0.1, quote.MembershipRatio)
			assert.Equal(t, 0.5, quote.Savings, "closing sales must not deactivate a current member")
		}
	}
}

func TestMembershipGroupQuotesStopDiscountAtExpiryOrExhaustion(t *testing.T) {
	for _, state := range []string{"expired", "exhausted"} {
		t.Run(state, func(t *testing.T) {
			_, user := setupMembershipServiceTest(t)
			previousUsable := setting.UserUsableGroups2JSONString()
			t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable)) })
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"gpt":"GPT"}`))
			self, err := model.GetMembershipSelf(user.Id)
			require.NoError(t, err)
			require.NotNil(t, self.Account)
			if state == "expired" {
				require.NoError(t, model.DB.Model(self.Account).Update("expires_at", common.GetTimestamp()-1).Error)
			} else {
				require.NoError(t, model.DB.Model(self.Account).Update("amount_used", self.Account.AmountTotal).Error)
			}
			catalog, err := model.GetMembershipCatalog()
			require.NoError(t, err)
			quotes, err := GetMembershipGroupQuotes(user.Id, catalog)
			require.NoError(t, err)
			require.Len(t, quotes, 1)
			assert.Equal(t, 1.0, quotes[0].Coefficient)
			assert.Equal(t, quotes[0].WalletRatio, quotes[0].MembershipRatio)
			assert.Zero(t, quotes[0].Savings)
		})
	}
}
