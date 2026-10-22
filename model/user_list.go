package model

// AdminUserListItem adds site ownership only to administrator list responses.
// User itself keeps tenant ownership out of ordinary account responses.
type AdminUserListItem struct {
	*User
	TenantID     int64  `json:"tenant_id"`
	TenantName   string `json:"tenant_name"`
	TenantDomain string `json:"tenant_domain"`
}

func GetAdminUserListItems(users []*User) ([]AdminUserListItem, error) {
	items := make([]AdminUserListItem, len(users))
	tenantIDs := make([]int64, 0)
	seen := make(map[int64]bool)
	for i, user := range users {
		items[i] = AdminUserListItem{User: user, TenantID: user.TenantID}
		if user.TenantID > PlatformTenantID && !seen[user.TenantID] {
			tenantIDs = append(tenantIDs, user.TenantID)
			seen[user.TenantID] = true
		}
	}
	if len(tenantIDs) == 0 {
		return items, nil
	}

	var tenants []Tenant
	if err := DB.Select("id", "display_name").Where("id IN ?", tenantIDs).Find(&tenants).Error; err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(tenants))
	for _, tenant := range tenants {
		names[tenant.ID] = tenant.DisplayName
	}
	var profiles []TenantProfile
	if err := DB.Select("tenant_id", "site_name").Where("tenant_id IN ?", tenantIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.SiteName != "" {
			names[profile.TenantID] = profile.SiteName
		}
	}
	var domains []TenantDomain
	if err := DB.Select("tenant_id", "hostname", "status").Where("tenant_id IN ?", tenantIDs).Order("id ASC").Find(&domains).Error; err != nil {
		return nil, err
	}
	selectedDomains := make(map[int64]TenantDomain, len(tenantIDs))
	for _, domain := range domains {
		selected, exists := selectedDomains[domain.TenantID]
		if !exists || selected.Status != TenantDomainStatusActive && domain.Status == TenantDomainStatusActive {
			selectedDomains[domain.TenantID] = domain
		}
	}
	for i := range items {
		items[i].TenantName = names[items[i].TenantID]
		items[i].TenantDomain = selectedDomains[items[i].TenantID].Hostname
	}
	return items, nil
}
