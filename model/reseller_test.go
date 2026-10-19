package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTenantCatalogRequiresOneEnabledDefault(t *testing.T) {
	entries := []TenantCatalogEntry{
		{TenantID: 42, SalesGroup: "default", Model: "gpt-4o", Status: TenantCatalogStatusEnabled, IsDefault: true},
		{TenantID: 42, SalesGroup: "default", Model: "claude-3", Status: TenantCatalogStatusEnabled},
	}
	require.NoError(t, ValidateTenantCatalog(entries))
	entries[0].IsDefault = false
	assert.ErrorIs(t, ValidateTenantCatalog(entries), ErrResellerInvalidCatalog)
	entries[0].IsDefault = true
	entries[0].Status = TenantCatalogStatusDisabled
	assert.ErrorIs(t, ValidateTenantCatalog(entries), ErrResellerInvalidCatalog)
}

func TestValidateTenantProfileRejectsUnsafeContentAndLogoURL(t *testing.T) {
	profile := &TenantProfile{TenantID: 42, SiteName: "KimiAI", Announcement: "<script>alert(1)</script>"}
	assert.ErrorIs(t, ValidateTenantProfile(profile), ErrResellerInvalidBrand)
	profile.Announcement = "safe announcement"
	profile.LogoURL = "https://cdn.example/logo.png"
	assert.ErrorIs(t, ValidateTenantProfile(profile), ErrResellerInvalidBrand)
	assert.NoError(t, ValidateTenantProfile(&TenantProfile{TenantID: 42, SiteName: "KimiAI", Announcement: EscapeTenantText("safe < text")}))
}

func TestValidateTenantPriceUsesPositiveDecimalMultiplier(t *testing.T) {
	price := &TenantPriceVersion{TenantID: 42, Group: "default", Model: "gpt-4o", SalesMultiplier: "1.6", Version: 1}
	require.NoError(t, ValidateTenantPrice(price))
	price.SalesMultiplier = "0"
	assert.ErrorIs(t, ValidateTenantPrice(price), ErrResellerInvalidPrice)
	price.SalesMultiplier = "1..6"
	assert.ErrorIs(t, ValidateTenantPrice(price), ErrResellerInvalidPrice)
}
