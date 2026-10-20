package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrResellerDomainAlreadyAssigned = errors.New("a domain is already assigned to this tenant")
	ErrResellerActivationIncomplete  = errors.New("active tenants require exactly one active domain")
)

type ResellerActivation struct {
	DomainReady bool `json:"domain_ready"`
	Ready       bool `json:"ready"`
}

// GetResellerActivations uses the same prerequisites for administration and activation.
// Callers that change tenant status must hold LockTenantTx for that tenant.
func GetResellerActivations(db *gorm.DB, tenantIDs []int64) (map[int64]ResellerActivation, error) {
	states := make(map[int64]ResellerActivation, len(tenantIDs))
	for _, id := range tenantIDs {
		states[id] = ResellerActivation{}
	}
	if len(tenantIDs) == 0 {
		return states, nil
	}
	if db == nil {
		return nil, ErrTenantStoreUnavailable
	}
	var domains []TenantDomain
	if err := db.Where("tenant_id IN ? AND status = ?", tenantIDs, TenantDomainStatusActive).Find(&domains).Error; err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(tenantIDs))
	for _, domain := range domains {
		counts[domain.TenantID]++
	}
	for id, state := range states {
		state.DomainReady = counts[id] == 1
		state.Ready = state.DomainReady
		states[id] = state
	}
	return states, nil
}

// LockTenantTx serializes all owner configuration changes for one tenant.
func LockTenantTx(tx *gorm.DB, tenantID int64) (*Tenant, error) {
	var tenant Tenant
	if tx == nil || tenantID <= 0 {
		return nil, ErrTenantMembershipMissing
	}
	err := lockForUpdate(tx).First(&tenant, tenantID).Error
	return &tenant, err
}

func ReviewResellerApplicationTx(tx *gorm.DB, applicationID int64, reviewerID int, approve bool, reason string) (*ResellerApplication, error) {
	if tx == nil || applicationID <= 0 || reviewerID <= 0 {
		return nil, ErrResellerInvalidApplication
	}
	var application ResellerApplication
	if err := lockForUpdate(tx).First(&application, applicationID).Error; err != nil {
		return nil, err
	}
	target := ResellerApplicationStatusRejected
	if approve {
		target = ResellerApplicationStatusApproved
	}
	if application.Status == target {
		return &application, nil
	}
	if application.Status != ResellerApplicationStatusPending {
		return nil, ErrResellerInvalidApplication
	}
	var owner User
	if err := lockForUpdate(tx).First(&owner, application.ApplicantUserID).Error; err != nil {
		return nil, err
	}
	if owner.TenantID != 0 {
		return nil, ErrTenantMembershipConflict
	}
	now := time.Now()
	if approve {
		var owned int64
		if err := tx.Model(&TenantMembership{}).Where("user_id = ? AND role = ?", owner.Id, TenantMembershipRoleOwner).Count(&owned).Error; err != nil {
			return nil, err
		}
		if owned > 0 {
			return nil, ErrTenantMembershipConflict
		}
		tenant := Tenant{DisplayName: application.BusinessName, Status: TenantStatusProvisioning}
		if err := tx.Create(&tenant).Error; err != nil {
			return nil, err
		}
		application.TenantID = tenant.ID
		membership := TenantMembership{TenantID: tenant.ID, UserID: owner.Id, Role: TenantMembershipRoleOwner, Status: TenantMembershipStatusActive}
		if err := tx.Create(&membership).Error; err != nil {
			return nil, err
		}
		if err := tx.Create(&TenantProfile{TenantID: tenant.ID, SiteName: application.BusinessName, Contact: application.ContactEmail}).Error; err != nil {
			return nil, err
		}
		if err := tx.Create(&ResellerWallet{TenantID: tenant.ID, ExactProfitCarry: "0", Version: 1, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}).Error; err != nil {
			return nil, err
		}
	}
	application.Status, application.ReviewedBy, application.ReviewedAt = target, reviewerID, &now
	application.RejectionReason = strings.TrimSpace(reason)
	if err := tx.Save(&application).Error; err != nil {
		return nil, err
	}
	return &application, nil
}

func ProvisionTenantDomainTx(tx *gorm.DB, tenantID int64, hostname string) (*TenantDomain, error) {
	if _, err := LockTenantTx(tx, tenantID); err != nil {
		return nil, err
	}
	canonical, err := NormalizeTenantHost(hostname)
	if err != nil {
		return nil, err
	}
	var assigned []TenantDomain
	// The tenant lock serializes provisioning; a locking read also sees the latest
	// committed domains when the caller already has a repeatable-read snapshot.
	if err := lockForUpdate(tx).Where("tenant_id = ?", tenantID).Find(&assigned).Error; err != nil {
		return nil, err
	}
	for _, existing := range assigned {
		if existing.Hostname != canonical {
			continue
		}
		if existing.Status == TenantDomainStatusActive {
			return &existing, nil
		}
		for _, other := range assigned {
			if other.ID != existing.ID && other.Status == TenantDomainStatusActive {
				return nil, ErrResellerDomainAlreadyAssigned
			}
		}
		existing.Status = TenantDomainStatusActive
		return &existing, tx.Save(&existing).Error
	}
	if len(assigned) > 0 {
		return nil, ErrResellerDomainAlreadyAssigned
	}
	var domain TenantDomain
	err = tx.Where("hostname = ?", canonical).First(&domain).Error
	if err == nil {
		if domain.TenantID != tenantID {
			return nil, ErrTenantMembershipConflict
		}
		domain.Status = TenantDomainStatusActive
		return &domain, tx.Save(&domain).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	domain = TenantDomain{TenantID: tenantID, Hostname: canonical, Prefix: strings.Split(canonical, ".")[0], Status: TenantDomainStatusActive}
	return &domain, tx.Create(&domain).Error
}
