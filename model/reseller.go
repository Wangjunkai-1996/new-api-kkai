package model

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ResellerApplicationStatusPending  = "pending"
	ResellerApplicationStatusApproved = "approved"
	ResellerApplicationStatusRejected = "rejected"

	TenantCatalogStatusEnabled  = "enabled"
	TenantCatalogStatusDisabled = "disabled"
)

var (
	ErrResellerInvalidApplication = errors.New("invalid reseller application")
	ErrResellerInvalidBrand       = errors.New("invalid reseller brand")
	ErrResellerInvalidCatalog     = errors.New("invalid reseller catalog")
	ErrResellerInvalidPrice       = errors.New("invalid reseller price")
)

// ResellerApplication records a request to operate a branded tenant. The
// application is intentionally separate from Tenant: a rejected or pending
// application must not create a routable tenant.
type ResellerApplication struct {
	ID              int64      `json:"id" gorm:"primaryKey"`
	ApplicantUserID int        `json:"applicant_user_id" gorm:"not null;index"`
	TenantID        int64      `json:"tenant_id" gorm:"not null;default:0;index"`
	BusinessName    string     `json:"business_name" gorm:"type:varchar(128);not null;default:''"`
	ContactName     string     `json:"contact_name" gorm:"type:varchar(64);not null;default:''"`
	ContactEmail    string     `json:"contact_email" gorm:"type:varchar(254);not null;default:''"`
	Notes           string     `json:"notes" gorm:"type:text;not null"`
	Status          string     `json:"status" gorm:"type:varchar(16);not null;index"`
	ReviewedBy      int        `json:"reviewed_by" gorm:"not null;default:0"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason string     `json:"rejection_reason" gorm:"type:text;not null"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (ResellerApplication) TableName() string { return "reseller_applications" }

// TenantProfile contains customer-visible branding. It is separate from the
// tenant routing record so changing copy/logo never changes Host resolution.
type TenantProfile struct {
	ID              int64     `json:"id" gorm:"primaryKey"`
	TenantID        int64     `json:"tenant_id" gorm:"not null;uniqueIndex"`
	SiteName        string    `json:"site_name" gorm:"type:varchar(128);not null;default:''"`
	LogoURL         string    `json:"logo_url" gorm:"type:varchar(2048);not null;default:''"`
	LogoData        []byte    `json:"-"`
	LogoMIME        string    `json:"-" gorm:"type:varchar(32)"`
	Contact         string    `json:"contact" gorm:"type:varchar(512);not null;default:''"`
	Announcement    string    `json:"announcement" gorm:"type:text;not null"`
	HomepageContent string    `json:"homepage_content" gorm:"type:text;not null"`
	UpdatedBy       int       `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (TenantProfile) TableName() string { return "tenant_profiles" }

// TenantCatalogEntry is a tenant's allow-list over platform-owned model
// groups. It does not copy or mutate Channel rows.
type TenantCatalogEntry struct {
	ID         int64     `json:"id" gorm:"primaryKey"`
	TenantID   int64     `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_tenant_catalog_model,priority:1"`
	SalesGroup string    `json:"sales_group" gorm:"column:sales_group;type:varchar(64);not null;uniqueIndex:idx_tenant_catalog_model,priority:2"`
	Model      string    `json:"model" gorm:"type:varchar(128);not null;uniqueIndex:idx_tenant_catalog_model,priority:3"`
	Status     string    `json:"status" gorm:"type:varchar(16);not null;index"`
	IsDefault  bool      `json:"is_default" gorm:"not null"`
	SortOrder  int       `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (TenantCatalogEntry) TableName() string { return "tenant_catalog_entries" }

// TenantPriceVersion is the tenant-owned sales configuration frozen by
// PricingQuote at request reservation. Supplier prices remain platform-controlled;
// owner APIs can only update SalesMultiplier.
type TenantPriceVersion struct {
	ID                 int64  `json:"id" gorm:"primaryKey"`
	TenantID           int64  `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_tenant_price_version,priority:1"`
	Version            int64  `json:"version" gorm:"not null;uniqueIndex:idx_tenant_price_version,priority:4"`
	Model              string `json:"model" gorm:"type:varchar(128);not null;uniqueIndex:idx_tenant_price_version,priority:2"`
	Group              string `json:"group" gorm:"column:group;type:varchar(64);not null;uniqueIndex:idx_tenant_price_version,priority:3"`
	SupplierPriceMinor int64  `json:"supplier_price_minor" gorm:"not null"`
	SalesMultiplier    string `json:"sales_multiplier" gorm:"type:varchar(64);not null"`
	EffectiveAt        int64  `json:"effective_at" gorm:"not null;index"`
	CreatedAt          int64  `json:"created_at" gorm:"not null"`
}

func (TenantPriceVersion) TableName() string { return "tenant_price_versions" }

func (application *ResellerApplication) BeforeCreate(_ *gorm.DB) error {
	if application == nil || application.ApplicantUserID <= 0 {
		return ErrResellerInvalidApplication
	}
	application.BusinessName = strings.TrimSpace(application.BusinessName)
	application.ContactName = strings.TrimSpace(application.ContactName)
	application.ContactEmail = strings.TrimSpace(application.ContactEmail)
	if application.BusinessName == "" || application.ContactName == "" || application.ContactEmail == "" {
		return ErrResellerInvalidApplication
	}
	if application.Status == "" {
		application.Status = ResellerApplicationStatusPending
	}
	if application.Status != ResellerApplicationStatusPending {
		return fmt.Errorf("%w: status %s", ErrResellerInvalidApplication, application.Status)
	}
	return nil
}

func ValidateTenantProfile(profile *TenantProfile) error {
	if profile == nil || profile.TenantID <= PlatformTenantID {
		return ErrResellerInvalidBrand
	}
	if strings.TrimSpace(profile.SiteName) == "" || strings.ContainsAny(profile.SiteName, "<>") || profile.LogoURL != "" || strings.ContainsAny(profile.Announcement, "<>") || strings.ContainsAny(profile.HomepageContent, "<>") || strings.ContainsAny(profile.Contact, "<>") || len(profile.SiteName) > 128 || len(profile.LogoURL) > 2048 || len(profile.Contact) > 512 || len(profile.Announcement) > 20000 || len(profile.HomepageContent) > 50000 {
		return ErrResellerInvalidBrand
	}
	return nil
}

// EscapeTenantText keeps profile content safe for clients that render a plain
// text/Markdown subset. Controllers should apply this before persistence;
// direct model callers can use it as well.
func EscapeTenantText(value string) string { return html.EscapeString(strings.TrimSpace(value)) }

func ValidateTenantCatalog(entries []TenantCatalogEntry) error {
	if len(entries) == 0 || len(entries) > 100 {
		return ErrResellerInvalidCatalog
	}
	defaults := 0
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.TenantID <= PlatformTenantID || strings.TrimSpace(entry.SalesGroup) == "" || strings.TrimSpace(entry.Model) == "" {
			return ErrResellerInvalidCatalog
		}
		status := entry.Status
		if status == "" {
			status = TenantCatalogStatusEnabled
		}
		if status != TenantCatalogStatusEnabled && status != TenantCatalogStatusDisabled {
			return ErrResellerInvalidCatalog
		}
		if entry.IsDefault {
			if status != TenantCatalogStatusEnabled {
				return ErrResellerInvalidCatalog
			}
			defaults++
		}
		key := entry.SalesGroup + "\x00" + entry.Model
		if _, exists := seen[key]; exists {
			return ErrResellerInvalidCatalog
		}
		seen[key] = struct{}{}
	}
	if defaults != 1 {
		return ErrResellerInvalidCatalog
	}
	return nil
}

func ValidateTenantPrice(price *TenantPriceVersion) error {
	if price == nil || price.TenantID <= PlatformTenantID || strings.TrimSpace(price.Group) == "" || strings.TrimSpace(price.Model) == "" {
		return ErrResellerInvalidPrice
	}
	multiplier := strings.TrimSpace(price.SalesMultiplier)
	if multiplier == "" || len(multiplier) > 32 || strings.HasPrefix(multiplier, "-") {
		return ErrResellerInvalidPrice
	}
	parsed, err := decimal.NewFromString(multiplier)
	if err != nil || !parsed.GreaterThan(decimal.Zero) {
		return ErrResellerInvalidPrice
	}
	if price.Version < 1 {
		return ErrResellerInvalidPrice
	}
	return nil
}
