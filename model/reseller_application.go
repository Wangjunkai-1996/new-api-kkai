package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrResellerDomainPrefixUnavailable = errors.New("reseller domain prefix storage is not ready")

// NormalizeResellerDomainPrefix accepts an optional DNS label, not a URL or
// hostname. Reserved labels belong to the platform and cannot be requested.
func NormalizeResellerDomainPrefix(raw string) (string, error) {
	prefix := strings.ToLower(strings.TrimSpace(raw))
	if prefix == "" {
		return "", nil
	}
	if len(prefix) > 63 || prefix[0] == '-' || prefix[len(prefix)-1] == '-' {
		return "", ErrTenantInvalidHost
	}
	for _, char := range prefix {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return "", ErrTenantInvalidHost
		}
	}
	switch prefix {
	case "www", "api", "api-origin", "dt-api", "cpa", "monitor", "admin", "mail", "status":
		return "", ErrTenantInvalidHost
	}
	return prefix, nil
}

// NormalizeResellerDomain restricts provisioning to one allowed label beneath
// the configured root. It intentionally rejects ports as provisioning input.
func NormalizeResellerDomain(raw, rootDomain string) (string, error) {
	if strings.ContainsAny(raw, ":") || strings.ContainsAny(rootDomain, ":") {
		return "", ErrTenantInvalidHost
	}
	root, err := NormalizeTenantHost(rootDomain)
	if err != nil {
		return "", err
	}
	host, err := NormalizeTenantHost(raw)
	if err != nil || !strings.HasSuffix(host, "."+root) {
		return "", ErrTenantInvalidHost
	}
	prefix, err := NormalizeResellerDomainPrefix(strings.TrimSuffix(host, "."+root))
	if err != nil || prefix == "" {
		return "", ErrTenantInvalidHost
	}
	return host, nil
}

// ResellerApplicationPrefixEnabled stays false during rolling expansion until
// the explicit schema migration has both added the column and recorded success.
func ResellerApplicationPrefixEnabled(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	var count int64
	if err := db.Session(&gorm.Session{NewDB: true}).Table("kkai_schema_migrations").Where("version = ?", 15).Count(&count).Error; err != nil || count != 1 {
		return false
	}
	var rows []struct{ DesiredDomainPrefix string }
	return db.Session(&gorm.Session{NewDB: true}).Table("reseller_applications").Select("desired_domain_prefix").Where("1 = 0").Find(&rows).Error == nil
}

func CreateResellerApplication(application *ResellerApplication) error {
	if DB == nil || application == nil {
		return ErrResellerInvalidApplication
	}
	return DB.Create(application).Error
}

// Both new submissions and reviews remain writable on schema 14. A requested
// prefix is never silently omitted when its storage has not become available.
func (application *ResellerApplication) BeforeSave(tx *gorm.DB) error {
	if application == nil || tx == nil {
		return ErrResellerInvalidApplication
	}
	prefix, err := NormalizeResellerDomainPrefix(application.DesiredDomainPrefix)
	if err != nil {
		return err
	}
	application.DesiredDomainPrefix = prefix
	if !ResellerApplicationPrefixEnabled(tx) {
		if prefix != "" {
			return ErrResellerDomainPrefixUnavailable
		}
		tx.Statement.Omits = append(tx.Statement.Omits, "DesiredDomainPrefix")
	}
	return nil
}
