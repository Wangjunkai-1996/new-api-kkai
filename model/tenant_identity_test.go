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
		if users[index].TenantID > PlatformTenantID {
			username := users[index].Username
			email := NormalizeEmail(users[index].Email)
			users[index].NormalizedUsername = &username
			if email != "" {
				users[index].NormalizedEmail = &email
			}
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
	assert.Nil(t, original.NormalizedUsername)
	assert.Nil(t, original.NormalizedEmail)
	assert.Error(t, DB.Create(&User{Username: "legacy", AffCode: "duplicate-platform"}).Error)
}

func TestTenantPlatformIdentityPreservesRawUsernameAndNullCanonicalKeys(t *testing.T) {
	tenantIdentityTestDB(t)
	for index, username := range []string{"LegacyName", "legacyname", "LegacyName "} {
		user := User{Username: username, AffCode: fmt.Sprintf("platform-%d", index)}
		require.NoError(t, user.prepareForInsert(DB))
		assert.Equal(t, username, user.Username)
		assert.Nil(t, user.NormalizedUsername)
		assert.Nil(t, user.NormalizedEmail)
		require.NoError(t, DB.Create(&user).Error)
	}
	for _, username := range []string{"LegacyName", "legacyname", "LegacyName "} {
		exists, err := CheckUserExistOrDeletedForTenant(username, "", PlatformTenantID)
		require.NoError(t, err)
		assert.True(t, exists)
	}
	exists, err := CheckUserExistOrDeletedForTenant("LEGACYNAME", "", PlatformTenantID)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Error(t, DB.Create(&User{Username: "LegacyName", AffCode: "duplicate"}).Error)
}

func TestTenantIdentityUsesCanonicalKeysForUnicodeCustomerLoginAndAvailability(t *testing.T) {
	tenantIdentityTestDB(t)
	const password = "CanonicalCustomerPassword123"
	user := User{TenantID: 42, Username: " ÄCustomer ", Email: " Üser@Example.COM ", Password: password, Status: common.UserStatusEnabled, AffCode: "canonical"}
	require.NoError(t, user.prepareForInsert(DB))
	assert.Equal(t, "äcustomer", user.Username)
	assert.Equal(t, "üser@example.com", user.Email)
	require.NoError(t, DB.Create(&user).Error)
	for _, identity := range []string{" ÄCUSTOMER ", " ÜSER@EXAMPLE.COM "} {
		login := User{Username: identity, Password: password}
		require.NoError(t, login.ValidateAndFillForTenant(42))
		assert.Equal(t, user.Id, login.Id)
	}
	exists, err := CheckUserExistOrDeletedForTenant(" ÄCUSTOMER ", "", 42)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.ErrorIs(t, EnsureEmailAvailableForTenant(" ÜSER@example.com ", 0, 42), ErrEmailAlreadyTaken)
	lookup := User{TenantID: 42, Email: " ÜSER@example.com "}
	require.NoError(t, lookup.FillUserByEmail())
	assert.Equal(t, user.Id, lookup.Id)
}

func TestTenantOwnerLoginRetainsPlatformUsernameComparison(t *testing.T) {
	tenantIdentityTestDB(t)
	const password = "LegacyOwnerPassword123"
	for index, username := range []string{"OwnerName", "ownername"} {
		owner := User{Username: username, Password: password, Status: common.UserStatusEnabled, AffCode: fmt.Sprintf("owner-%d", index)}
		require.NoError(t, owner.prepareForInsert(DB))
		require.NoError(t, DB.Create(&owner).Error)
		require.NoError(t, DB.Create(&TenantMembership{TenantID: 42, UserID: owner.Id, Role: TenantMembershipRoleOwner, Status: TenantMembershipStatusActive}).Error)
		login := User{Username: username, Password: password}
		require.NoError(t, login.ValidateAndFillTenantOwner(42))
		assert.Equal(t, owner.Id, login.Id)
	}
	wrongCase := User{Username: "OWNERNAME", Password: password}
	assert.ErrorIs(t, wrongCase.ValidateAndFillTenantOwner(42), ErrInvalidCredentials)
}

func TestTenantPlatformProfileUpdatesPreserveHistoricalSharedEmail(t *testing.T) {
	tenantIdentityTestDB(t)
	users := []User{
		{Username: "LegacyUser", Email: "Shared@Example.com", AffCode: "legacy-1"},
		{Username: "legacyuser", Email: "shared@example.com", AffCode: "legacy-2"},
	}
	require.NoError(t, DB.Create(&users).Error)
	users[0].DisplayName = "Updated"
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return users[0].UpdateWithTx(tx, false) }))
	assert.Equal(t, "Shared@Example.com", users[0].Email)
	assert.Nil(t, users[0].NormalizedUsername)
	assert.Nil(t, users[0].NormalizedEmail)
	users[0].Username = "RenamedUser"
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return users[0].EditWithTx(tx, false) }))
	assert.Equal(t, "RenamedUser", users[0].Username)
	assert.Equal(t, "Shared@Example.com", users[0].Email)
	assert.Nil(t, users[0].NormalizedUsername)
	assert.Nil(t, users[0].NormalizedEmail)
	_, err := GetUniqueUserByEmail("shared@example.com")
	assert.ErrorIs(t, err, ErrEmailAmbiguous)
	lookup := User{Email: "shared@example.com"}
	assert.ErrorIs(t, lookup.FillUserByEmail(), ErrEmailAmbiguous)
}

func TestTenantCustomerProfileUpdatesMaintainCanonicalIdentity(t *testing.T) {
	tenantIdentityTestDB(t)
	user := User{TenantID: 42, Username: "before", Email: "before@example.com", AffCode: "customer-update"}
	require.NoError(t, user.prepareForInsert(DB))
	require.NoError(t, DB.Create(&user).Error)
	user.Username, user.Email = " ÄAFTER ", " ÜSER@Example.com "
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return user.UpdateWithTx(tx, false) }))
	assert.Equal(t, "äafter", user.Username)
	assert.Equal(t, "üser@example.com", user.Email)
	require.NotNil(t, user.NormalizedUsername)
	assert.Equal(t, user.Username, *user.NormalizedUsername)
	require.NotNil(t, user.NormalizedEmail)
	assert.Equal(t, user.Email, *user.NormalizedEmail)
	user.Username = " NextName "
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return user.EditWithTx(tx, false) }))
	assert.Equal(t, "nextname", user.Username)
	assert.Equal(t, user.Username, *user.NormalizedUsername)
}
