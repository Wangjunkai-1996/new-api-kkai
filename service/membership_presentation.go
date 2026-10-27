package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type MembershipGroupPrice struct {
	GroupKey    string  `json:"group_key"`
	DisplayName string  `json:"display_name"`
	Description string  `json:"description"`
	BaseRatio   float64 `json:"base_ratio"`
	WalletRatio float64 `json:"wallet_ratio"`
	Published   bool    `json:"published"`
	Enabled     bool    `json:"enabled"`
}

type MembershipTierQuote struct {
	Coefficient     float64 `json:"coefficient"`
	MembershipRatio float64 `json:"membership_ratio"`
	Savings         float64 `json:"savings"`
}

// MembershipGroupQuote applies one published catalog's tier discounts to current group prices.
type MembershipGroupQuote struct {
	MembershipGroupPrice
	MembershipTierQuote
}

// MembershipCatalogGroupQuote quotes the latest terms for each available tier.
type MembershipCatalogGroupQuote struct {
	MembershipGroupPrice
	TierQuotes map[string]MembershipTierQuote `json:"tier_quotes"`
}

func membershipAccessibleGroupPrices(userID int, catalog *model.MembershipCatalog) ([]MembershipGroupPrice, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return nil, err
	}
	return membershipAccessibleGroupPricesForUser(user, catalog)
}

func membershipAccessibleGroupPricesForUser(user *model.User, catalog *model.MembershipCatalog) ([]MembershipGroupPrice, error) {
	if user == nil || catalog == nil {
		return nil, model.ErrMembershipInvalidInput
	}
	allowed := GetUserUsableGroups(user.Group)
	groups := make(map[string]model.MembershipGroup, len(catalog.Groups))
	for _, group := range catalog.Groups {
		groups[group.Key] = group
	}
	rows := make([]MembershipGroupPrice, 0, len(allowed))
	for key, description := range allowed {
		group, registered := groups[key]
		if IsAutoGroup(key) || !ratio_setting.ContainsGroupRatio(key) || (registered && !group.Enabled) {
			continue
		}
		base := ratio_setting.GetGroupRatio(key)
		userRatio := base
		if special, ok := ratio_setting.GetGroupGroupRatio(user.Group, key); ok {
			userRatio = special
		}
		rows = append(rows, MembershipGroupPrice{
			GroupKey: key, DisplayName: setting.GetGroupDisplayNameWithFallback(key, description),
			Description: description, BaseRatio: base, WalletRatio: userRatio,
			Published: registered && group.Published, Enabled: !registered || group.Enabled,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].GroupKey < rows[j].GroupKey })
	return rows, nil
}

func membershipTierQuote(wallet, ratio, coefficient float64) MembershipTierQuote {
	savings := 0.0
	if wallet > 0 {
		savings = (wallet - ratio) / wallet
	}
	return MembershipTierQuote{Coefficient: coefficient, MembershipRatio: ratio, Savings: savings}
}

func GetMembershipGroupQuotes(userID int, catalog *model.MembershipCatalog) ([]MembershipGroupQuote, error) {
	prices, err := membershipAccessibleGroupPrices(userID, catalog)
	if err != nil {
		return nil, err
	}
	tierID := ""
	if model.MembershipEnabled() {
		self, err := model.GetMembershipSelf(userID)
		if err != nil {
			return nil, err
		}
		if self.Account != nil && self.Account.ActiveAt(self.ServerTime) {
			tierID = self.Account.TierID
		}
	}
	rows := make([]MembershipGroupQuote, 0, len(prices))
	for _, price := range prices {
		coefficient := 1.0
		if tierID != "" {
			coefficient, err = model.MembershipDiscount(catalog, tierID, price.GroupKey)
			if err != nil {
				return nil, err
			}
		}
		ratio := model.ResolveMembershipRatio(price.BaseRatio, price.WalletRatio, coefficient)
		rows = append(rows, MembershipGroupQuote{MembershipGroupPrice: price,
			MembershipTierQuote: membershipTierQuote(price.WalletRatio, ratio, coefficient)})
	}
	return rows, nil
}

func GetMembershipCatalogGroupQuotes(userID int, catalog *model.MembershipCatalog) ([]MembershipCatalogGroupQuote, error) {
	prices, err := membershipAccessibleGroupPrices(userID, catalog)
	if err != nil {
		return nil, err
	}
	return membershipCatalogGroupQuotes(prices, catalog), nil
}

// GetMembershipCatalogGroupQuotesForUser reuses a user loaded by the caller.
// Catalog endpoints already need the user for group filtering, so this avoids
// a second identical user query while preserving the ID-based API above.
func GetMembershipCatalogGroupQuotesForUser(user *model.User, catalog *model.MembershipCatalog) ([]MembershipCatalogGroupQuote, error) {
	prices, err := membershipAccessibleGroupPricesForUser(user, catalog)
	if err != nil {
		return nil, err
	}
	return membershipCatalogGroupQuotes(prices, catalog), nil
}

func membershipCatalogGroupQuotes(prices []MembershipGroupPrice, catalog *model.MembershipCatalog) []MembershipCatalogGroupQuote {
	rows := make([]MembershipCatalogGroupQuote, 0, len(prices))
	for _, price := range prices {
		quotes := make(map[string]MembershipTierQuote)
		for _, tier := range catalog.Tiers {
			if !tier.Enabled {
				continue
			}
			coefficient := 1.0
			if value, ok := tier.Discounts[price.GroupKey]; ok && price.Published {
				coefficient = value
			}
			ratio := model.ResolveMembershipRatio(price.BaseRatio, price.WalletRatio, coefficient)
			quotes[tier.ID] = membershipTierQuote(price.WalletRatio, ratio, coefficient)
		}
		rows = append(rows, MembershipCatalogGroupQuote{MembershipGroupPrice: price, TierQuotes: quotes})
	}
	return rows
}
