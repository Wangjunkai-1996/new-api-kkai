package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// MembershipGroupQuote is the same server-resolved price used by request billing.
// The catalogue advertises new terms; these rows describe the user's entitlement.
type MembershipGroupQuote struct {
	GroupKey        string  `json:"group_key"`
	DisplayName     string  `json:"display_name"`
	Coefficient     float64 `json:"coefficient"`
	WalletRatio     float64 `json:"wallet_ratio"`
	MembershipRatio float64 `json:"membership_ratio"`
	Published       bool    `json:"published"`
	Enabled         bool    `json:"enabled"`
}

func GetMembershipGroupQuotes(userID int, catalog *model.MembershipCatalog) ([]MembershipGroupQuote, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return nil, err
	}
	allowed := GetUserUsableGroups(user.Group)
	groups := make(map[string]model.MembershipGroup, len(catalog.Groups))
	for _, group := range catalog.Groups {
		groups[group.Key] = group
	}
	rows := make([]MembershipGroupQuote, 0, len(allowed))
	for key, description := range allowed {
		group, registered := groups[key]
		if key == "auto" || !ratio_setting.ContainsGroupRatio(key) || (registered && !group.Enabled) {
			continue
		}
		base := ratio_setting.GetGroupRatio(key)
		userRatio := base
		if special, ok := ratio_setting.GetGroupGroupRatio(user.Group, key); ok {
			userRatio = special
		}
		quote, err := model.GetMembershipQuote(userID, key, base, userRatio)
		if err != nil {
			return nil, err
		}
		rows = append(rows, MembershipGroupQuote{
			GroupKey: key, DisplayName: setting.GetGroupDisplayNameWithFallback(key, description),
			Coefficient: quote.Discount, WalletRatio: quote.WalletRatio, MembershipRatio: quote.MembershipRatio,
			Published: registered && group.Published, Enabled: !registered || group.Enabled,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].GroupKey < rows[j].GroupKey })
	return rows, nil
}
