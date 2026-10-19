package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantSiteConfigRespectsRoutingFlagAndProvisioning(t *testing.T) {
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	platform, err := ResolveTenantSiteConfig(nil, model.TenantContext{})
	require.NoError(t, err)
	assert.False(t, platform.EnabledModules["reseller"])
	t.Setenv("TENANT_ROUTING_ENABLED", "true")
	platform, err = ResolveTenantSiteConfig(nil, model.TenantContext{})
	require.NoError(t, err)
	assert.True(t, platform.EnabledModules["reseller"])

	setupAuthSessionTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Tenant{}, &model.TenantProfile{}))
	require.NoError(t, model.DB.Create(&model.Tenant{ID: 42, Status: model.TenantStatusProvisioning, DisplayName: "Tenant setup"}).Error)
	config, err := ResolveTenantSiteConfig(model.DB, model.TenantContext{TenantID: 42, Hostname: "kimi.example.com"})
	require.NoError(t, err)
	assert.True(t, config.Maintenance)
	assert.Equal(t, model.TenantStatusProvisioning, config.Status)
	assert.Equal(t, "Tenant setup", config.SiteName)
	require.NoError(t, model.DB.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
	_, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{TenantID: 42})
	assert.ErrorIs(t, err, model.ErrTenantDisabled)
}
