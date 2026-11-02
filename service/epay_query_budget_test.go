package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpayQueryBudgetSharesNormalizedMerchantIdentity(t *testing.T) {
	db := newLeaderLeaseTestDB(t)
	previous := model.DB
	model.DB = nil
	t.Cleanup(func() {
		model.DB = previous
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	ctx := context.Background()
	require.NoError(t, WaitEpayQueryBudget(ctx, db, "https://PAY.EXAMPLE.COM/gateway/", "merchant"))
	var lease model.KKAIJobLease
	require.NoError(t, db.First(&lease).Error)
	assert.Positive(t, lease.LeaseUntil)
	assert.NotContains(t, lease.LeaseName, "merchant")
	// Expire the lease explicitly; the test does not depend on a wall-clock wait.
	require.NoError(t, db.Model(&lease).Update("lease_until", 0).Error)
	require.NoError(t, WaitEpayQueryBudget(ctx, db, "https://pay.example.com/gateway", "merchant"))
	var leases []model.KKAIJobLease
	require.NoError(t, db.Find(&leases).Error)
	require.Len(t, leases, 1)
	assert.EqualValues(t, 2, leases[0].Fence)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, WaitEpayQueryBudget(cancelled, db, "https://pay.example.com", "merchant"), ErrEpayQueryBudgetUnavailable)
	assert.ErrorIs(t, WaitEpayQueryBudget(ctx, db, "https://user:secret@pay.example.com", "merchant"), ErrEpayQueryBudgetUnavailable)
}

func TestMembershipNotificationOriginDoesNotChangeBrowserReturnBase(t *testing.T) {
	previous := operation_setting.CustomCallbackAddress
	operation_setting.CustomCallbackAddress = "https://console.example.com"
	t.Cleanup(func() { operation_setting.CustomCallbackAddress = previous })
	t.Setenv("KKAI_MEMBERSHIP_NOTIFY_ADDRESS", "https://notify.example.com/")
	address, err := MembershipNotifyURL()
	require.NoError(t, err)
	assert.Equal(t, "https://notify.example.com/api/membership/epay/notify", address.String())
	assert.Equal(t, "https://console.example.com", GetCallbackAddress())
	for _, invalid := range []string{"http://notify.example.com", "https://notify.example.com/?token=secret", "https://user:secret@notify.example.com", "https://notify.example.com/console"} {
		t.Setenv("KKAI_MEMBERSHIP_NOTIFY_ADDRESS", invalid)
		_, err := MembershipNotifyURL()
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
	}
}
