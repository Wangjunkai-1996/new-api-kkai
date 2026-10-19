package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTenantTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:tenant-model-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Tenant{}, &TenantDomain{}, &TenantMembership{}))
	return db
}

func TestNormalizeTenantHost(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		err  bool
	}{
		{name: "case and port", raw: " KIMI.Example.com:443 ", want: "kimi.example.com"},
		{name: "trailing dot", raw: "kimi.example.com.", want: "kimi.example.com"},
		{name: "explicit private service host", raw: "newapi-active:3000", want: "newapi-active"},
		{name: "invalid URL", raw: "https://kimi.example.com", err: true},
		{name: "invalid IP", raw: "127.0.0.1", err: true},
		{name: "invalid empty label", raw: "kimi..example.com", err: true},
		{name: "invalid service port", raw: "kimi.example.com:https", err: true},
		{name: "invalid port range", raw: "kimi.example.com:65536", err: true},
		{name: "invalid empty port", raw: "kimi.example.com:", err: true},
		{name: "IDNA", raw: "bücher.example", want: "xn--bcher-kva.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeTenantHost(test.raw)
			if test.err {
				require.ErrorIs(t, err, ErrTenantInvalidHost)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestResolveTenantByHostUsesExplicitPlatformHostAndRejectsUnknown(t *testing.T) {
	db := newTenantTestDB(t)
	active := Tenant{ID: 42, Status: TenantStatusActive, DisplayName: "KimiAI"}
	disabled := Tenant{ID: 43, Status: TenantStatusDisabled, DisplayName: "Disabled"}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Create(&TenantDomain{TenantID: active.ID, Hostname: "kimi.example.com", Status: TenantDomainStatusActive}).Error)
	require.NoError(t, db.Create(&TenantDomain{TenantID: disabled.ID, Hostname: "disabled.example.com", Status: TenantDomainStatusActive}).Error)

	platform, err := ResolveTenantByHost(nil, "MAIN.Example.com:443", "main.example.com")
	require.NoError(t, err)
	assert.Equal(t, PlatformTenantID, platform.TenantID)

	resolved, err := ResolveTenantByHost(db, "KIMI.ExAMPLE.COM", "main.example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(42), resolved.TenantID)

	_, err = ResolveTenantByHost(db, "unknown.example.com", "main.example.com")
	require.ErrorIs(t, err, ErrTenantUnknownHost)
	_, err = ResolveTenantByHost(db, "disabled.example.com", "main.example.com")
	require.ErrorIs(t, err, ErrTenantDisabled)
}

func TestTenantScopeMembershipAndQueryStayWithinTenant(t *testing.T) {
	db := newTenantTestDB(t)
	require.NoError(t, db.Create(&Tenant{ID: 42, Status: TenantStatusActive}).Error)
	require.NoError(t, db.Create(&Tenant{ID: 43, Status: TenantStatusActive}).Error)
	require.NoError(t, db.Create(&TenantMembership{UserID: 7, TenantID: 42, Role: TenantMembershipRoleOwner, Status: TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&TenantMembership{UserID: 8, TenantID: 42, Role: TenantMembershipRoleCustomer, Status: TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&TenantMembership{UserID: 7, TenantID: 43, Role: TenantMembershipRoleCustomer, Status: TenantMembershipStatusActive}).Error)
	require.ErrorIs(t, db.Create(&TenantMembership{UserID: 7, TenantID: 42, Role: TenantMembershipRoleCustomer, Status: TenantMembershipStatusActive}).Error, ErrTenantMembershipConflict)
	require.NoError(t, db.Create(&TenantDomain{TenantID: 42, Hostname: "a.example.com", Status: TenantDomainStatusActive}).Error)
	require.NoError(t, db.Create(&TenantDomain{TenantID: 43, Hostname: "b.example.com", Status: TenantDomainStatusActive}).Error)

	scope := TenantScope{TenantID: 42}
	owner, err := scope.Membership(db, 7, TenantMembershipRoleOwner)
	require.NoError(t, err)
	require.NoError(t, ValidateTenantMembership(owner, 42, 7, TenantMembershipRoleOwner))
	require.ErrorIs(t, ValidateTenantMembership(owner, 43, 7, TenantMembershipRoleOwner), ErrTenantMembershipMissing)
	require.ErrorIs(t, ValidateTenantMembership(owner, 42, 8, TenantMembershipRoleOwner), ErrTenantMembershipMissing)

	_, err = scope.Membership(db, 7, TenantMembershipRoleCustomer)
	require.ErrorIs(t, err, ErrTenantMembershipMissing)

	var domains []TenantDomain
	require.NoError(t, scope.Apply(db.Model(&TenantDomain{})).Find(&domains).Error)
	require.Len(t, domains, 1)
	assert.Equal(t, "a.example.com", domains[0].Hostname)
}
