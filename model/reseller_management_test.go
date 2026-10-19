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
