package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func tenantIdentityTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:tenant-identity-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&User{}, &Tenant{}, &TenantMembership{}))
	t.Cleanup(func() {
		DB = previous
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestTenantLoginScopesCustomerIdentityAndOwnerMembership(t *testing.T) {
	tenantIdentityTestDB(t)
	password := "TenantIdentityPass123"
	hash, err := common.HashAccountPassword(password)
	require.NoError(t, err)
	users := []User{
		{Username: "same-name", Email: "same@example.com", Password: hash, TenantID: 41, Status: common.UserStatusEnabled, AffCode: "tenant41"},
		{Username: "same-name", Email: "same@example.com", Password: hash, TenantID: 42, Status: common.UserStatusEnabled, AffCode: "tenant42"},
		{Username: "owner", Password: hash, TenantID: PlatformTenantID, Status: common.UserStatusEnabled, AffCode: "owner01"},
	}
	for index := range users {
		username := users[index].Username
		email := NormalizeEmail(users[index].Email)
		users[index].NormalizedUsername = &username
		if email != "" {
			users[index].NormalizedEmail = &email
		}
		require.NoError(t, DB.Create(&users[index]).Error)
	}
	require.NoError(t, DB.Create(&TenantMembership{UserID: users[2].Id, TenantID: 41, Role: TenantMembershipRoleOwner, Status: TenantMembershipStatusActive}).Error)
	require.NoError(t, DB.Create(&TenantMembership{UserID: users[0].Id, TenantID: 41, Role: TenantMembershipRoleCustomer, Status: TenantMembershipStatusActive}).Error)
	require.NoError(t, DB.Create(&TenantMembership{UserID: users[1].Id, TenantID: 42, Role: TenantMembershipRoleCustomer, Status: TenantMembershipStatusActive}).Error)

	login := User{Username: "same-name", Password: password}
	require.NoError(t, login.ValidateAndFillForTenant(42))
	assert.Equal(t, users[1].Id, login.Id)

	ownerLogin := User{Username: "owner", Password: password}
	require.NoError(t, ownerLogin.ValidateAndFillTenantOwner(41))
	assert.Equal(t, users[2].Id, ownerLogin.Id)

	noMembership := User{Username: "owner", Password: password}
	assert.ErrorIs(t, noMembership.ValidateAndFillTenantOwner(42), ErrInvalidCredentials)

	wrongTenant := User{Username: "same-name", Password: password}
	assert.ErrorIs(t, wrongTenant.ValidateAndFillForTenant(43), ErrInvalidCredentials)
}

func TestTenantOwnerLoginSelectsExplicitNamespaceDespiteMatchingCustomer(t *testing.T) {
	tenantIdentityTestDB(t)
	password := "MatchingOwnerAndCustomer123"
	owner := User{TenantID: PlatformTenantID, Username: "same-name", Email: "same@example.com", Password: password, Status: common.UserStatusEnabled, AffCode: "owner"}
	customer := User{TenantID: 42, Username: owner.Username, Email: owner.Email, Password: password, Status: common.UserStatusEnabled, AffCode: "customer"}
	for _, user := range []*User{&owner, &customer} {
		require.NoError(t, user.prepareForInsert(DB))
		require.NoError(t, DB.Create(user).Error)
	}
	require.NoError(t, DB.Create(&TenantMembership{TenantID: 42, UserID: owner.Id, Role: TenantMembershipRoleOwner, Status: TenantMembershipStatusActive}).Error)
	for _, identity := range []string{owner.Username, "SAME@example.com"} {
		ownerLogin := User{Username: identity, Password: password}
		require.NoError(t, ownerLogin.ValidateAndFillTenantOwner(42))
		assert.Equal(t, owner.Id, ownerLogin.Id)
		assert.Equal(t, PlatformTenantID, ownerLogin.TenantID)
		customerLogin := User{Username: identity, Password: password}
		require.NoError(t, customerLogin.ValidateAndFillForTenant(42))
		assert.Equal(t, customer.Id, customerLogin.Id)
		assert.EqualValues(t, 42, customerLogin.TenantID)
	}
	// A mismatching customer password never triggers a second namespace lookup.
	customerPassword, err := common.HashAccountPassword("DifferentCustomerPassword123")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&customer).Update("password", customerPassword).Error)
	wrongCustomerPassword := User{Username: owner.Username, Password: password}
	assert.ErrorIs(t, wrongCustomerPassword.ValidateAndFillForTenant(42), ErrInvalidCredentials)
	missingCustomer := User{Username: owner.Username, Password: password}
	assert.ErrorIs(t, missingCustomer.ValidateAndFillForTenant(43), ErrInvalidCredentials)
	wrongOwnerPassword := User{Username: owner.Username, Password: "DifferentCustomerPassword123"}
	assert.ErrorIs(t, wrongOwnerPassword.ValidateAndFillTenantOwner(42), ErrInvalidCredentials)
	for _, tenantID := range []int64{PlatformTenantID, 43} {
		unauthorizedOwner := User{Username: owner.Username, Password: password}
		assert.ErrorIs(t, unauthorizedOwner.ValidateAndFillTenantOwner(tenantID), ErrInvalidCredentials)
	}
	require.NoError(t, DB.Model(&TenantMembership{}).Where("tenant_id = ? AND user_id = ?", 42, owner.Id).Update("status", TenantMembershipStatusDisabled).Error)
	revokedOwner := User{Username: owner.Username, Password: password}
	assert.ErrorIs(t, revokedOwner.ValidateAndFillTenantOwner(42), ErrInvalidCredentials)
}

func TestTenantUsernameEmailUniquenessAllowsOtherTenant(t *testing.T) {
	tenantIdentityTestDB(t)
	first := User{TenantID: 11, Username: "TenantUser", Email: "same@example.com", AffCode: "first01"}
	second := User{TenantID: 12, Username: "tenantuser", Email: "SAME@example.com", AffCode: "second1"}
	require.NoError(t, first.prepareForInsert(DB))
	require.NoError(t, DB.Create(&first).Error)
	require.NoError(t, second.prepareForInsert(DB))
	require.NoError(t, DB.Create(&second).Error)

	duplicate := User{TenantID: 11, Username: "tenantuser", Email: "other@example.com"}
	assert.ErrorIs(t, duplicate.prepareForInsert(DB), ErrUsernameAlreadyTaken)
	assert.ErrorIs(t, EnsureEmailAvailableForTenant("SAME@example.com", 0, 11), ErrEmailAlreadyTaken)
	assert.NoError(t, EnsureEmailAvailableForTenant("SAME@example.com", 0, 13))
}

func TestTenantIdentityMigrationRemovesLegacyGlobalUsernameConstraint(t *testing.T) {
	tenantIdentityTestDB(t)
	require.NoError(t, DB.Migrator().DropTable(&User{}))
	require.NoError(t, DB.Exec("CREATE TABLE users (id integer PRIMARY KEY, username text, password text NOT NULL DEFAULT '', CONSTRAINT uni_users_username UNIQUE (username))").Error)
	require.NoError(t, DB.Exec("INSERT INTO users (id, username) VALUES (1, 'legacy')").Error)
	require.NoError(t, DB.AutoMigrate(&User{}))
	require.NoError(t, InitializeTenantUserIdentities())
	newUser := User{TenantID: 42, Username: "legacy", AffCode: "newtenant"}
	require.NoError(t, newUser.prepareForInsert(DB))
	require.NoError(t, DB.Create(&newUser).Error)
	var original User
	require.NoError(t, DB.First(&original, 1).Error)
	require.NotNil(t, original.NormalizedUsername)
	assert.Equal(t, "legacy", *original.NormalizedUsername)
}
