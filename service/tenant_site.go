package service

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

const MaxTenantLogoBytes = 2 << 20

type TenantSiteConfig struct {
	TenantID                    int64           `json:"tenant_id"`
	Hostname                    string          `json:"hostname"`
	Status                      string          `json:"status"`
	SiteName                    string          `json:"site_name"`
	Logo                        string          `json:"logo"`
	Contact                     string          `json:"contact"`
	Announcement                string          `json:"announcement"`
	HomepageContent             string          `json:"homepage_content"`
	EnabledModules              map[string]bool `json:"enabled_modules"`
	Maintenance                 bool            `json:"maintenance"`
	ResellerRootDomain          string          `json:"reseller_root_domain"`
	ResellerDomainPrefixEnabled bool            `json:"reseller_domain_prefix_enabled"`
}

func ResolveTenantSiteConfig(db *gorm.DB, tenant model.TenantContext) (*TenantSiteConfig, error) {
	config := &TenantSiteConfig{TenantID: tenant.TenantID, Hostname: tenant.Hostname, Status: model.TenantStatusActive,
		EnabledModules: map[string]bool{"text": true, "pricing": true, "wallet": true, "reseller": true}}
	if tenant.TenantID == model.PlatformTenantID {
		config.EnabledModules["reseller"] = strings.EqualFold(strings.TrimSpace(os.Getenv("TENANT_ROUTING_ENABLED")), "true")
		config.SiteName, config.Logo = common.SystemName, common.Logo
		config.ResellerRootDomain, _ = model.NormalizeTenantHost(os.Getenv("TENANT_ROOT_DOMAIN"))
		config.ResellerDomainPrefixEnabled = config.EnabledModules["reseller"] && config.ResellerRootDomain != "" && model.ResellerApplicationPrefixEnabled(db)
		return config, nil
	}
	if db == nil {
		return nil, model.ErrTenantStoreUnavailable
	}
	var row model.Tenant
	if err := db.First(&row, tenant.TenantID).Error; err != nil {
		return nil, err
	}
	if row.Status != model.TenantStatusActive && row.Status != model.TenantStatusProvisioning {
		return nil, model.ErrTenantDisabled
	}
	config.Status = row.Status
	config.Maintenance = row.Status == model.TenantStatusProvisioning
	var profile model.TenantProfile
	err := db.Where("tenant_id = ?", tenant.TenantID).Omit("logo_data").First(&profile).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	config.SiteName = row.DisplayName
	if profile.SiteName != "" {
		config.SiteName = profile.SiteName
	}
	config.Logo, config.Contact, config.Announcement, config.HomepageContent = profile.LogoURL, profile.Contact, profile.Announcement, profile.HomepageContent
	return config, nil
}

func SaveTenantLogo(db *gorm.DB, tenantID int64, ownerID int, data []byte) (string, error) {
	if db == nil || tenantID <= 0 || ownerID <= 0 || len(data) == 0 || len(data) > MaxTenantLogoBytes {
		return "", model.ErrResellerInvalidBrand
	}
	mime := http.DetectContentType(data)
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return "", model.ErrResellerInvalidBrand
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 || !strings.Contains(mime, format) {
		return "", model.ErrResellerInvalidBrand
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return "", model.ErrResellerInvalidBrand
	}
	hash := sha256.Sum256(data)
	logoURL := fmt.Sprintf("/api/site-logo?v=%x", hash[:12])
	err = db.Transaction(func(tx *gorm.DB) error {
		var profile model.TenantProfile
		if err := tx.Where("tenant_id = ?", tenantID).First(&profile).Error; err != nil {
			return err
		}
		return tx.Model(&profile).Updates(map[string]any{"logo_data": data, "logo_mime": mime, "logo_url": logoURL, "updated_by": ownerID}).Error
	})
	return logoURL, err
}
