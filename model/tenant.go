package model

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"gorm.io/gorm"
)

// PlatformTenantID is the reserved tenant used by the existing KKAI site.
// It is intentionally represented by zero so existing rows remain compatible
// while tenant ownership is introduced incrementally.
const PlatformTenantID int64 = 0

const (
	TenantStatusActive       = "active"
	TenantStatusProvisioning = "provisioning"
	TenantStatusDisabled     = "disabled"

	TenantDomainStatusActive   = "active"
	TenantDomainStatusDisabled = "disabled"

	TenantMembershipRoleOwner    = "owner"
	TenantMembershipRoleCustomer = "customer"

	TenantMembershipStatusActive   = "active"
	TenantMembershipStatusDisabled = "disabled"
)

var (
	ErrTenantInvalidHost        = errors.New("tenant host is invalid")
	ErrTenantUnknownHost        = errors.New("tenant host is unknown")
	ErrTenantDisabled           = errors.New("tenant is disabled")
	ErrTenantStoreUnavailable   = errors.New("tenant store is unavailable")
	ErrTenantMembershipMissing  = errors.New("tenant membership is missing")
	ErrTenantMembershipConflict = errors.New("tenant membership role conflicts with existing membership")
)

// Tenant is the logical owner of a branded site and its customer data. The
// platform tenant (ID 0) is implicit and does not need a database row.
type Tenant struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Status      string    `json:"status" gorm:"type:varchar(16);not null;index"`
	DisplayName string    `json:"display_name" gorm:"type:varchar(128);not null;default:''"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Tenant) TableName() string { return "tenants" }

// TenantDomain maps an explicitly provisioned hostname to a tenant. Hostname
// is stored in canonical ASCII form and is globally unique across tenants.
type TenantDomain struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	TenantID  int64     `json:"tenant_id" gorm:"not null;index"`
	Hostname  string    `json:"hostname" gorm:"type:varchar(253);not null;uniqueIndex"`
	Prefix    string    `json:"prefix" gorm:"type:varchar(63);not null;default:''"`
	Status    string    `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TenantDomain) TableName() string { return "tenant_domains" }

// TenantMembership grants a user a role inside a tenant. Platform users keep
// their existing account and may gain an owner membership without changing
// their platform tenant ID.
type TenantMembership struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	UserID    int       `json:"user_id" gorm:"not null;index;uniqueIndex:idx_tenant_memberships_identity,priority:1"`
	TenantID  int64     `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_tenant_memberships_identity,priority:2"`
	Role      string    `json:"role" gorm:"type:varchar(16);not null"`
	Status    string    `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TenantMembership) TableName() string { return "tenant_memberships" }

// BeforeCreate keeps an account from being both the owner and a customer of
// the same tenant. The same user may still own one tenant and be a customer
// of another tenant, which is useful for platform operators and testing.
func (membership *TenantMembership) BeforeCreate(tx *gorm.DB) error {
	if membership == nil || membership.UserID <= 0 || membership.TenantID < PlatformTenantID {
		return ErrTenantMembershipMissing
	}
	if membership.Role != TenantMembershipRoleOwner && membership.Role != TenantMembershipRoleCustomer {
		return fmt.Errorf("invalid tenant membership role %q", membership.Role)
	}
	if membership.Status != TenantMembershipStatusActive {
		return nil
	}
	var count int64
	if err := tx.Model(&TenantMembership{}).
		Where("user_id = ? AND tenant_id = ? AND status = ? AND role <> ?", membership.UserID, membership.TenantID, TenantMembershipStatusActive, membership.Role).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrTenantMembershipConflict
	}
	return nil
}

// TenantContext is the request-scoped result of resolving a canonical Host.
// Host resolution is deliberately independent from authentication: a Host
// selects the site, while the session/token selects the account.
type TenantContext struct {
	TenantID int64
	Hostname string
	Status   string
}

// TenantScope is the only tenant filter business handlers should carry. It is
// created from the request TenantContext rather than from a client parameter.
type TenantScope struct {
	TenantID int64
}

func (scope TenantScope) Apply(query *gorm.DB) *gorm.DB {
	if query == nil {
		return nil
	}
	return query.Where("tenant_id = ?", scope.TenantID)
}

// snapshotUserTenant records primary customer ownership on persisted activity
// and identity rows. Deleted users retain their ownership for audit history.
func snapshotUserTenant(db *gorm.DB, userID int, tenantID *int64) error {
	if db == nil || userID <= 0 || tenantID == nil || *tenantID != PlatformTenantID {
		return nil
	}
	var user User
	err := db.Unscoped().Select("tenant_id").First(&user, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil // Historical system records may outlive their account.
	}
	if err != nil {
		if !db.Migrator().HasTable(&User{}) {
			return nil // Identity tables may not exist in isolated activity stores.
		}
		return err
	}
	*tenantID = user.TenantID
	return nil
}

func (scope TenantScope) Membership(db *gorm.DB, userID int, roles ...string) (*TenantMembership, error) {
	if db == nil || userID <= 0 {
		return nil, ErrTenantMembershipMissing
	}
	query := db.Where("tenant_id = ? AND user_id = ? AND status = ?", scope.TenantID, userID, TenantMembershipStatusActive)
	if len(roles) > 0 {
		query = query.Where("role IN ?", roles)
	}
	var membership TenantMembership
	if err := query.First(&membership).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantMembershipMissing
		}
		return nil, err
	}
	return &membership, nil
}

// NormalizeTenantHost strips an optional port, canonicalizes case and IDNA,
// and rejects URL-like or malformed values. It never trusts X-Forwarded-Host.
func NormalizeTenantHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "/?#@") {
		return "", ErrTenantInvalidHost
	}
	host := raw
	if splitHost, port, err := net.SplitHostPort(raw); err == nil {
		number, portErr := strconv.Atoi(port)
		if portErr != nil || number < 1 || number > 65535 || strings.ContainsAny(port, "+-") {
			return "", ErrTenantInvalidHost
		}
		host = splitHost
	} else if strings.HasPrefix(raw, "[") || strings.Contains(raw, ":") {
		// A bracketed IPv6 literal without a port is accepted by net.ParseIP;
		// all other unparsed colon forms are ambiguous and rejected.
		host = strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "[")
		if net.ParseIP(host) == nil {
			return "", ErrTenantInvalidHost
		}
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || len(host) > 253 {
		return "", ErrTenantInvalidHost
	}
	if net.ParseIP(host) != nil {
		return "", ErrTenantInvalidHost
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" || len(ascii) > 253 {
		return "", ErrTenantInvalidHost
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", ErrTenantInvalidHost
		}
	}
	return ascii, nil
}

// ResolveTenantByHost resolves an explicitly configured platform host or an
// active tenant domain. No database error or unknown host falls back to the
// platform tenant.
func ResolveTenantByHost(db *gorm.DB, rawHost string, platformHosts ...string) (TenantContext, error) {
	host, err := NormalizeTenantHost(rawHost)
	if err != nil {
		return TenantContext{}, err
	}
	for _, platformHost := range platformHosts {
		canonical, canonicalErr := NormalizeTenantHost(platformHost)
		if canonicalErr == nil && canonical == host {
			return TenantContext{TenantID: PlatformTenantID, Hostname: host}, nil
		}
	}
	if db == nil {
		return TenantContext{}, ErrTenantStoreUnavailable
	}
	var domain TenantDomain
	if err := db.Where("hostname = ? AND status = ?", host, TenantDomainStatusActive).First(&domain).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TenantContext{}, ErrTenantUnknownHost
		}
		return TenantContext{}, err
	}
	if domain.TenantID == PlatformTenantID {
		return TenantContext{TenantID: PlatformTenantID, Hostname: host}, nil
	}
	var tenant Tenant
	if err := db.Where("id = ?", domain.TenantID).First(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TenantContext{}, ErrTenantUnknownHost
		}
		return TenantContext{}, err
	}
	if tenant.Status != TenantStatusActive && tenant.Status != TenantStatusProvisioning {
		return TenantContext{}, ErrTenantDisabled
	}
	return TenantContext{TenantID: tenant.ID, Hostname: host, Status: tenant.Status}, nil
}

func (membership *TenantMembership) ActiveOwner() bool {
	return membership != nil && membership.Status == TenantMembershipStatusActive && membership.Role == TenantMembershipRoleOwner
}

func ValidateTenantMembership(membership *TenantMembership, tenantID int64, userID int, role string) error {
	if membership == nil || membership.TenantID != tenantID || membership.UserID != userID || membership.Status != TenantMembershipStatusActive {
		return ErrTenantMembershipMissing
	}
	if role != "" && membership.Role != role {
		return fmt.Errorf("%w: role %s", ErrTenantMembershipMissing, role)
	}
	return nil
}
