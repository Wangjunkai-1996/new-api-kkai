package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeResellerDomainPrefix(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		valid bool
	}{
		{"", "", true},
		{" Kimi-AI ", "kimi-ai", true},
		{"1", "1", true},
		{strings.Repeat("a", 63), strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), "", false},
		{"-kimi", "", false},
		{"kimi-", "", false},
		{"kimi.ai", "", false},
		{"kimi.kkai.win", "", false},
		{"https://kimi", "", false},
		{"kimi/path", "", false},
		{"kimi:443", "", false},
		{"kimi_ai", "", false},
		{"奇米", "", false},
		{"www", "", false},
		{"API", "", false},
		{"api-origin", "", false},
		{"dt-api", "", false},
		{"cpa", "", false},
		{"monitor", "", false},
		{"admin", "", false},
		{"mail", "", false},
		{"status", "", false},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := NormalizeResellerDomainPrefix(test.input)
			if !test.valid {
				assert.ErrorIs(t, err, ErrTenantInvalidHost)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestNormalizeResellerDomain(t *testing.T) {
	host, err := NormalizeResellerDomain(" Kimi-AI.KKAI.WIN ", "kkai.win")
	require.NoError(t, err)
	assert.Equal(t, "kimi-ai.kkai.win", host)
	for _, host := range []string{"kkai.win", "api.kkai.win", "kimi.foo.kkai.win", "kimi.kkai.win.evil.com", "kimi.kkai.win:443", "https://kimi.kkai.win", "kimi_ai.kkai.win"} {
		_, err := NormalizeResellerDomain(host, "kkai.win")
		assert.ErrorIs(t, err, ErrTenantInvalidHost, host)
	}
}

func TestResellerApplicationSchema14Compatibility(t *testing.T) {
	tenantIdentityTestDB(t)
	require.NoError(t, DB.AutoMigrate(&ResellerApplication{}, &TenantProfile{}, &ResellerWallet{}))
	assert.False(t, DB.Migrator().HasColumn(&ResellerApplication{}, "DesiredDomainPrefix"))
	assert.False(t, ResellerApplicationPrefixEnabled(DB))
	owner := User{Username: "legacy-owner", AffCode: "legacy"}
	require.NoError(t, DB.Create(&owner).Error)
	application := ResellerApplication{ApplicantUserID: owner.Id, BusinessName: "Legacy site", ContactEmail: "owner@example.com"}
	require.NoError(t, CreateResellerApplication(&application))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		approved, err := ReviewResellerApplicationTx(tx, application.ID, 99, true, "")
		if err == nil {
			assert.Positive(t, approved.TenantID)
			assert.Empty(t, approved.DesiredDomainPrefix)
		}
		return err
	}))
	rejected := ResellerApplication{ApplicantUserID: owner.Id, BusinessName: "New site", ContactEmail: "owner@example.com", DesiredDomainPrefix: "kimi"}
	require.ErrorIs(t, CreateResellerApplication(&rejected), ErrResellerDomainPrefixUnavailable)
	var count int64
	require.NoError(t, DB.Model(&ResellerApplication{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestResellerApplicationPrefixPersistenceAndReview(t *testing.T) {
	tenantIdentityTestDB(t)
	require.NoError(t, DB.AutoMigrate(&ResellerApplication{}, &TenantProfile{}, &ResellerWallet{}))
	// Expansion alone does not enable submissions until the migration is recorded.
	require.NoError(t, DB.Exec("ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix varchar(63)").Error)
	assert.False(t, ResellerApplicationPrefixEnabled(DB))
	owner := User{Username: "prefix-owner", AffCode: "prefix"}
	require.NoError(t, DB.Create(&owner).Error)
	application := ResellerApplication{ApplicantUserID: owner.Id, BusinessName: " KimiAI ", ContactEmail: " OWNER@example.com ", DesiredDomainPrefix: " Kimi-AI ", Notes: "Optional note"}
	require.ErrorIs(t, CreateResellerApplication(&application), ErrResellerDomainPrefixUnavailable)
	require.NoError(t, DB.Exec("CREATE TABLE kkai_schema_migrations (version bigint PRIMARY KEY)").Error)
	require.NoError(t, DB.Table("kkai_schema_migrations").Create(map[string]any{"version": 15}).Error)
	assert.True(t, ResellerApplicationPrefixEnabled(DB))
	require.NoError(t, CreateResellerApplication(&application))
	var loaded ResellerApplication
	require.NoError(t, DB.First(&loaded, application.ID).Error)
	assert.Equal(t, "kimi-ai", loaded.DesiredDomainPrefix)
	assert.Equal(t, "KimiAI", loaded.BusinessName)
	assert.Equal(t, "owner@example.com", loaded.ContactEmail)
	assert.Empty(t, loaded.ContactName)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		approved, err := ReviewResellerApplicationTx(tx, application.ID, 99, true, "")
		if err == nil {
			assert.Equal(t, "kimi-ai", approved.DesiredDomainPrefix)
		}
		return err
	}))
	require.NoError(t, DB.First(&loaded, application.ID).Error)
	assert.Equal(t, "kimi-ai", loaded.DesiredDomainPrefix)
	assert.Equal(t, ResellerApplicationStatusApproved, loaded.Status)
	var profile TenantProfile
	require.NoError(t, DB.Where("tenant_id = ?", loaded.TenantID).First(&profile).Error)
	assert.Equal(t, "KimiAI", profile.SiteName)
	assert.Equal(t, "owner@example.com", profile.Contact)
}

func TestResellerApplicationRejectsInvalidDetails(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*ResellerApplication)
	}{
		{"empty name", func(application *ResellerApplication) { application.BusinessName = " " }},
		{"long name", func(application *ResellerApplication) { application.BusinessName = strings.Repeat("a", 129) }},
		{"long contact", func(application *ResellerApplication) { application.ContactName = strings.Repeat("a", 65) }},
		{"invalid email", func(application *ResellerApplication) { application.ContactEmail = "not-an-email" }},
		{"display name email", func(application *ResellerApplication) { application.ContactEmail = "Owner <owner@example.com>" }},
		{"long notes", func(application *ResellerApplication) { application.Notes = strings.Repeat("a", 4097) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := ResellerApplication{ApplicantUserID: 1, BusinessName: "Site", ContactEmail: "owner@example.com"}
			test.apply(&application)
			assert.ErrorIs(t, application.BeforeCreate(nil), ErrResellerInvalidApplication)
		})
	}
}
