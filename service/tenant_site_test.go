package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantSiteConfigRespectsRoutingFlagAndProvisioning(t *testing.T) {
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	t.Setenv("TENANT_ROOT_DOMAIN", "EXAMPLE.COM")
	platform, err := ResolveTenantSiteConfig(nil, model.TenantContext{})
	require.NoError(t, err)
	assert.False(t, platform.EnabledModules["reseller"])
	assert.Equal(t, "example.com", platform.ResellerRootDomain)
	assert.False(t, platform.ResellerDomainPrefixEnabled)
	t.Setenv("TENANT_ROUTING_ENABLED", "true")
	platform, err = ResolveTenantSiteConfig(nil, model.TenantContext{})
	require.NoError(t, err)
	assert.True(t, platform.EnabledModules["reseller"])
	assert.False(t, platform.ResellerDomainPrefixEnabled)

	setupAuthSessionTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Tenant{}, &model.TenantProfile{}))
	require.NoError(t, model.DB.Create(&model.Tenant{ID: 42, Status: model.TenantStatusProvisioning, DisplayName: "Tenant setup"}).Error)
	config, err := ResolveTenantSiteConfig(model.DB, model.TenantContext{TenantID: 42, Hostname: "kimi.example.com"})
	require.NoError(t, err)
	assert.True(t, config.Maintenance)
	assert.Equal(t, model.TenantStatusProvisioning, config.Status)
	assert.Equal(t, "Tenant setup", config.SiteName)
	assert.Empty(t, config.ResellerRootDomain)
	assert.False(t, config.ResellerDomainPrefixEnabled)
	require.NoError(t, model.DB.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
	_, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{TenantID: 42})
	assert.ErrorIs(t, err, model.ErrTenantDisabled)
}

func TestTenantSiteConfigAdvertisesPrefixOnlyAfterSchemaUpgrade(t *testing.T) {
	t.Setenv("TENANT_ROUTING_ENABLED", "true")
	t.Setenv("TENANT_ROOT_DOMAIN", "EXAMPLE.COM")
	setupAuthSessionTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ResellerApplication{}, &kkaimigrate.AppliedMigration{}))
	config, err := ResolveTenantSiteConfig(model.DB, model.TenantContext{})
	require.NoError(t, err)
	assert.False(t, config.ResellerDomainPrefixEnabled)
	require.NoError(t, model.DB.Exec("ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix varchar(63)").Error)
	config, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{})
	require.NoError(t, err)
	assert.False(t, config.ResellerDomainPrefixEnabled, "an unverified column must not enable the form")
	require.NoError(t, model.DB.Create(&kkaimigrate.AppliedMigration{Version: 15, Name: "application-prefix"}).Error)
	config, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{})
	require.NoError(t, err)
	assert.True(t, config.ResellerDomainPrefixEnabled)
	assert.Equal(t, "example.com", config.ResellerRootDomain)
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	config, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{})
	require.NoError(t, err)
	assert.False(t, config.ResellerDomainPrefixEnabled)
	t.Setenv("TENANT_ROUTING_ENABLED", "true")
	t.Setenv("TENANT_ROOT_DOMAIN", "https://example.com")
	config, err = ResolveTenantSiteConfig(model.DB, model.TenantContext{})
	require.NoError(t, err)
	assert.Empty(t, config.ResellerRootDomain)
	assert.False(t, config.ResellerDomainPrefixEnabled)
}
