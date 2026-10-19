package service

import (
	"context"

	"github.com/QuantumNous/new-api/model"
)

type TenantCustomerPrice struct {
	Model      string
	Group      string
	SalesRatio float64
	VersionID  int64
}

// ResolveCustomerPrices joins the enabled catalogue with its latest valid
// retail versions in two bounded queries. Supply costs never leave this API.
func (s *TenantPricingService) ResolveCustomerPrices(ctx context.Context, tenantID int64) ([]TenantCustomerPrice, error) {
	entries, err := s.ResolveCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]TenantCustomerPrice, 0, len(entries))
	if len(entries) == 0 {
		return result, nil
	}
	defaults := 0
	group := ""
	models := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDefault {
			defaults++
			group = entry.SalesGroup
		}
		models = append(models, entry.Model)
	}
	if defaults != 1 || group == "" || IsAutoGroup(group) {
		return nil, model.ErrResellerInvalidCatalog
	}
	// The grouped MAX keeps historical versions out of memory; use a map
	// condition for the reserved group column on all supported databases.
	latest := s.db.WithContext(ctx).Model(&model.TenantPriceVersion{}).Select("MAX(id)").Where(map[string]any{"tenant_id": tenantID, "group": group}).Where("model IN ?", models).Group("model")
	var versions []model.TenantPriceVersion
	if err := s.db.WithContext(ctx).Where("id IN (?)", latest).Find(&versions).Error; err != nil {
		return nil, err
	}
	byModel := make(map[string]model.TenantPriceVersion, len(versions))
	for _, version := range versions {
		byModel[version.Model] = version
	}
	supported, err := tenantTextSupplyModels(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		version, ok := byModel[entry.Model]
		if !ok || entry.SalesGroup != group || !supported[group+"\x00"+entry.Model] {
			continue
		}
		ratio, err := EffectiveTenantMultiplier(version.SalesMultiplier, group)
		if err != nil {
			continue
		}
		value, _ := ratio.Float64()
		result = append(result, TenantCustomerPrice{Model: entry.Model, Group: group, SalesRatio: value, VersionID: version.ID})
	}
	return result, nil
}
