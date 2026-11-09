package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipRecoveryPersistentControlCancelsAndDrainsOnlyItsRound(t *testing.T) {
	setupMembershipRecoveryWorkerTest(t)
	ctx := context.Background()
	control, err := refreshMembershipRecoveryControl(ctx)
	require.NoError(t, err)
	round, finish, err := beginMembershipRecoveryRound(ctx, control)
	require.NoError(t, err)
	defer finish()
	require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).
		Update("value", "paused:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").Error)
	status, err := MembershipBillingRecoveryRuntimeStatus(ctx)
	require.NoError(t, err)
	assert.ErrorIs(t, round.Err(), context.Canceled)
	assert.Equal(t, "paused", status.EffectiveMode)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", status.Generation)
	assert.True(t, status.Inflight, "cancellation alone is not a drain acknowledgement")
	finish()
	status, err = MembershipBillingRecoveryRuntimeStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.Inflight)
	initializeMembershipRecoveryControl("restarted-worker", "apply")
	status, err = MembershipBillingRecoveryRuntimeStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "paused", status.EffectiveMode, "restart cannot erase durable pause")
	assert.False(t, status.Inflight)
}

func TestMembershipRecoveryControlMissingMalformedAndReadErrorsStopAdmission(t *testing.T) {
	setupMembershipRecoveryWorkerTest(t)
	ctx := context.Background()
	for _, value := range []string{"apply", "APPLY:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "apply:bad"} {
		require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).Update("value", value).Error)
		status, err := MembershipBillingRecoveryRuntimeStatus(ctx)
		require.Error(t, err)
		assert.Equal(t, "paused", status.EffectiveMode)
		assert.False(t, status.Synchronized)
	}
	require.NoError(t, model.DB.Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).Delete(&model.Option{}).Error)
	status, err := MembershipBillingRecoveryRuntimeStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "paused", status.EffectiveMode)
	assert.Empty(t, status.Generation)
	require.NoError(t, model.DB.Migrator().DropTable(&model.Option{}))
	status, err = MembershipBillingRecoveryRuntimeStatus(ctx)
	require.Error(t, err)
	assert.Equal(t, "paused", status.EffectiveMode)
	assert.False(t, status.Synchronized)
}
