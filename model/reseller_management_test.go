package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resellerApplicationReviewFixture(t *testing.T) (*gorm.DB, []ResellerApplication) {
	t.Helper()
	tenantIdentityTestDB(t)
	db := DB
	require.NoError(t, db.AutoMigrate(&TenantProfile{}, &ResellerApplication{}, &ResellerWallet{}))
	owner := User{Username: "reseller-owner", AffCode: "owner"}
	require.NoError(t, db.Create(&owner).Error)
	applications := []ResellerApplication{
		{ApplicantUserID: owner.Id, BusinessName: "First brand", ContactName: "Owner", ContactEmail: "owner@example.com"},
		{ApplicantUserID: owner.Id, BusinessName: "Second brand", ContactName: "Owner", ContactEmail: "owner@example.com"},
	}
	for index := range applications {
		require.NoError(t, db.Create(&applications[index]).Error)
	}
	return db, applications
}

func TestResellerApplicationReviewPreservesOneTenantPerOwner(t *testing.T) {
	db, applications := resellerApplicationReviewFixture(t)
	var approved *ResellerApplication
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		approved, err = ReviewResellerApplicationTx(tx, applications[0].ID, 99, true, "")
		return err
	}))
	require.Positive(t, approved.TenantID)
	// A repeated review returns the original station rather than creating one.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		replayed, err := ReviewResellerApplicationTx(tx, applications[0].ID, 99, true, "")
		if err == nil {
			assert.Equal(t, approved.TenantID, replayed.TenantID)
		}
		return err
	}))
	// Disabling an existing station's membership must not permit a second one.
	for _, status := range []string{TenantMembershipStatusActive, TenantMembershipStatusDisabled} {
		require.NoError(t, db.Model(&TenantMembership{}).Where("tenant_id = ?", approved.TenantID).Update("status", status).Error)
		err := db.Transaction(func(tx *gorm.DB) error {
			_, err := ReviewResellerApplicationTx(tx, applications[1].ID, 99, true, "")
			return err
		})
		require.ErrorIs(t, err, ErrTenantMembershipConflict)
	}
	require.NoError(t, db.First(&applications[1], applications[1].ID).Error)
	assert.Equal(t, ResellerApplicationStatusPending, applications[1].Status)
	assert.Zero(t, applications[1].TenantID)
	for _, row := range []any{&Tenant{}, &TenantMembership{}, &TenantProfile{}, &ResellerWallet{}} {
		var count int64
		require.NoError(t, db.Model(row).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
	// The duplicate pending application can still be rejected normally.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReviewResellerApplicationTx(tx, applications[1].ID, 99, false, "one station per owner")
		return err
	}))
}

func TestResellerApplicationReviewConcurrentApprovalsCannotCreateSecondTenant(t *testing.T) {
	db, applications := resellerApplicationReviewFixture(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite serializes writers; one connection makes concurrent transaction
	// callers deterministic without depending on lock timing or sleeps.
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan error, len(applications))
	for _, application := range applications {
		go func(id int64) {
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error {
				_, err := ReviewResellerApplicationTx(tx, id, 99, true, "")
				return err
			})
		}(application.ID)
	}
	close(start)
	a, b := <-results, <-results
	assert.True(t, (a == nil && errors.Is(b, ErrTenantMembershipConflict)) || (b == nil && errors.Is(a, ErrTenantMembershipConflict)), "approval results: %v, %v", a, b)
	for _, row := range []any{&Tenant{}, &TenantMembership{}, &TenantProfile{}, &ResellerWallet{}} {
		var count int64
		require.NoError(t, db.Model(row).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
	var approved int64
	require.NoError(t, db.Model(&ResellerApplication{}).Where("status = ?", ResellerApplicationStatusApproved).Count(&approved).Error)
	assert.EqualValues(t, 1, approved)
}

func TestResellerDomainProvisioningPreservesOneDomainPerTenant(t *testing.T) {
	tenantIdentityTestDB(t)
	db := DB
	require.NoError(t, db.AutoMigrate(&TenantDomain{}))
	require.NoError(t, db.Create(&[]Tenant{{ID: 42, Status: TenantStatusProvisioning}, {ID: 43, Status: TenantStatusProvisioning}}).Error)
	var assigned *TenantDomain
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		assigned, err = ProvisionTenantDomainTx(tx, 42, " KIMI.EXAMPLE.COM ")
		return err
	}))
	assert.Equal(t, "kimi.example.com", assigned.Hostname)
	for _, status := range []string{TenantDomainStatusActive, TenantDomainStatusDisabled} {
		require.NoError(t, db.Model(assigned).Update("status", status).Error)
		err := db.Transaction(func(tx *gorm.DB) error {
			_, err := ProvisionTenantDomainTx(tx, 42, "other.example.com")
			return err
		})
		assert.ErrorIs(t, err, ErrResellerDomainAlreadyAssigned)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			replayed, err := ProvisionTenantDomainTx(tx, 42, "kimi.example.com")
			if err == nil {
				assert.Equal(t, assigned.ID, replayed.ID)
				assert.Equal(t, TenantDomainStatusActive, replayed.Status)
			}
			return err
		}))
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := ProvisionTenantDomainTx(tx, 43, "kimi.example.com")
		return err
	})
	assert.ErrorIs(t, err, ErrTenantMembershipConflict)
	// Historical aliases are retained, but cannot become a second active domain.
	legacy := TenantDomain{TenantID: 42, Hostname: "legacy.example.com", Status: TenantDomainStatusDisabled}
	require.NoError(t, db.Create(&legacy).Error)
	err = db.Transaction(func(tx *gorm.DB) error {
		_, err := ProvisionTenantDomainTx(tx, 42, legacy.Hostname)
		return err
	})
	assert.ErrorIs(t, err, ErrResellerDomainAlreadyAssigned)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		replayed, err := ProvisionTenantDomainTx(tx, 42, assigned.Hostname)
		if err == nil {
			assert.Equal(t, assigned.ID, replayed.ID)
		}
		return err
	}))
	err = db.Transaction(func(tx *gorm.DB) error {
		_, err := ProvisionTenantDomainTx(tx, 42, "third.example.com")
		return err
	})
	assert.ErrorIs(t, err, ErrResellerDomainAlreadyAssigned)
	var domains []TenantDomain
	require.NoError(t, db.Order("id").Find(&domains).Error)
	require.Len(t, domains, 2)
	assert.Equal(t, []string{"kimi.example.com", "legacy.example.com"}, []string{domains[0].Hostname, domains[1].Hostname})
	assert.Equal(t, TenantDomainStatusDisabled, domains[1].Status)
}

func TestResellerDomainConcurrentProvisioningCannotAddSecondDomain(t *testing.T) {
	tenantIdentityTestDB(t)
	db := DB
	require.NoError(t, db.AutoMigrate(&TenantDomain{}))
	require.NoError(t, db.Create(&Tenant{ID: 42, Status: TenantStatusProvisioning}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite serializes writers. Exercise concurrent callers without timing assumptions.
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, hostname := range []string{"first.example.com", "second.example.com"} {
		go func(host string) {
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error {
				_, err := ProvisionTenantDomainTx(tx, 42, host)
				return err
			})
		}(hostname)
	}
	close(start)
	a, b := <-results, <-results
	assert.True(t, (a == nil && errors.Is(b, ErrResellerDomainAlreadyAssigned)) || (b == nil && errors.Is(a, ErrResellerDomainAlreadyAssigned)), "provisioning results: %v, %v", a, b)
	var domains []TenantDomain
	require.NoError(t, db.Where("tenant_id = ?", 42).Find(&domains).Error)
	assert.Len(t, domains, 1)
}

func TestResellerActivationRequiresOneActiveDomainOnly(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []string
		ready    bool
	}{
		{"no domain", nil, false},
		{"disabled domain", []string{TenantDomainStatusDisabled}, false},
		{"one active domain", []string{TenantDomainStatusActive}, true},
		{"one active historical alias", []string{TenantDomainStatusActive, TenantDomainStatusDisabled}, true},
		{"multiple active historical aliases", []string{TenantDomainStatusActive, TenantDomainStatusActive}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tenantIdentityTestDB(t)
			db := DB
			require.NoError(t, db.AutoMigrate(&TenantDomain{}))
			for index, status := range test.statuses {
				host := []string{"first.example.com", "second.example.com"}[index]
				require.NoError(t, db.Create(&TenantDomain{TenantID: 42, Hostname: host, Status: status}).Error)
			}
			// Another tenant's domain cannot satisfy this site's activation.
			require.NoError(t, db.Create(&TenantDomain{TenantID: 43, Hostname: "other.example.com", Status: TenantDomainStatusActive}).Error)
			states, err := GetResellerActivations(db, []int64{42, 44})
			require.NoError(t, err)
			assert.Equal(t, map[int64]ResellerActivation{42: {DomainReady: test.ready, Ready: test.ready}, 44: {}}, states)
		})
	}
}

func TestResellerActivationPropagatesDatabaseFailures(t *testing.T) {
	tenantIdentityTestDB(t)
	_, err := GetResellerActivations(DB, []int64{42})
	assert.Error(t, err)
}
