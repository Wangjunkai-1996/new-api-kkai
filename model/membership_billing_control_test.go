package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipRecoveryControlGuardsFundsAndGenericOptionWrites(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	control, err := ParseMembershipRecoveryControl("apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: MembershipBillingRecoveryControlOption, Value: "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}).Error)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "control-fence", 80, 80))
	require.NoError(t, err)
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), membershipRecoveryEvidence(record, 30)))
	ctx := WithMembershipRecoveryGuard(context.Background(), func(tx *gorm.DB) error {
		return RequireMembershipRecoveryControlTx(tx, control)
	})
	require.NoError(t, DB.Model(&Option{}).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).Update("value", "paused:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").Error)
	_, err = ResolveMembershipRelayEvidence(ctx, record.RequestID)
	require.ErrorIs(t, err, ErrMembershipRecoveryControl)
	unchanged, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", unchanged.State)
	assert.ErrorIs(t, UpdateOption(MembershipBillingRecoveryControlOption, "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), ErrMembershipRecoveryControlReadOnly)
	assert.ErrorIs(t, UpdateOptionsBulk(map[string]string{MembershipBillingRecoveryControlOption: "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}), ErrMembershipRecoveryControlReadOnly)
	// Foreground settlement remains usable during an operator recovery pause.
	completed, err := ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "settled", completed.State)
}
