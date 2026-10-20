package service

import (
	"context"
)

type TenantCustomerPrice struct {
	Model      string
	Group      string
	SalesRatio float64
	VersionID  int64
}

// ResolveCustomerPrices uses the same inherited catalogue and group prices as
// request authorization; missing tenant rows never hide platform defaults.
func (s *TenantPricingService) ResolveCustomerPrices(ctx context.Context, tenantID int64) ([]TenantCustomerPrice, error) {
	entries, err := s.ResolveCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	groups, err := s.ListGroupPrices(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byGroup := make(map[string]TenantGroupPrice, len(groups))
	for _, group := range groups {
		byGroup[group.Group] = group
	}
	result := make([]TenantCustomerPrice, 0, len(entries))
	for _, entry := range entries {
		group, ok := byGroup[entry.SalesGroup]
		if !ok {
			continue
		}
		ratio, err := EffectiveTenantMultiplier(group.SalesMultiplier, entry.SalesGroup)
		if err != nil {
			return nil, err
		}
		value, _ := ratio.Float64()
		result = append(result, TenantCustomerPrice{Model: entry.Model, Group: entry.SalesGroup, SalesRatio: value, VersionID: group.ID})
	}
	return result, nil
}
