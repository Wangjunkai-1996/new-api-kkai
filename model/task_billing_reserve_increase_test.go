package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncreaseTaskBillingReservationPreservesSourceAndRollsBackTokenFailure(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		t.Skip("requires TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	truncateTables(t)
	user := createReserveTestUser(t, 100)
	token := Token{UserId: user.Id, Key: "retry-reservation-token", Status: common.TokenStatusEnabled, RemainQuota: 30, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	task := Task{TaskID: "retry-reservation", UserId: user.Id, Status: TaskStatusNotStart, PrivateData: TaskPrivateData{BillingState: TaskBillingStatePending}}
	require.NoError(t, DB.Create(&task).Error)
	ctx := context.Background()
	_, err := ReserveTaskBilling(ctx, task.ID, TaskBillingReservationRequest{Source: TaskBillingSourceWallet, Quota: 10, TokenID: token.Id})
	require.NoError(t, err)
	increased, err := IncreaseTaskBillingReservation(ctx, task.ID, 20)
	require.NoError(t, err)
	assert.True(t, increased.Applied)
	assert.Equal(t, 20, increased.Task.Quota)
	assert.Equal(t, TaskBillingSourceWallet, increased.Task.PrivateData.BillingSource)
	assert.EqualValues(t, 80, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 10, getTokenFromDB(t, token.Id).RemainQuota)
	for _, target := range []int{20, 15} {
		replay, err := IncreaseTaskBillingReservation(ctx, task.ID, target)
		require.NoError(t, err)
		assert.False(t, replay.Applied)
	}
	_, err = IncreaseTaskBillingReservation(ctx, task.ID, 40)
	require.ErrorIs(t, err, ErrTaskBillingInsufficientToken)
	assert.EqualValues(t, 80, getUserQuotaFromDB(t, user.Id), "funding debit rolls back with token failure")
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.Equal(t, 20, task.Quota)
	assert.Equal(t, 20, task.PrivateData.TokenQuota)
	var owner User
	require.NoError(t, DB.First(&owner, user.Id).Error)
	assert.Zero(t, owner.RequestCount, "reservation growth is not a new request")
	task.PrivateData.BillingContext = &TaskBillingContext{MaxQuota: common.GetPointer(20)}
	require.NoError(t, DB.Save(&task).Error)
	_, err = IncreaseTaskBillingReservation(ctx, task.ID, 21)
	require.ErrorIs(t, err, ErrTaskBillingInvalidRequest)
	task.PrivateData.BillingState = TaskBillingStateDispatching
	require.NoError(t, DB.Save(&task).Error)
	_, err = IncreaseTaskBillingReservation(ctx, task.ID, 20)
	require.ErrorIs(t, err, ErrTaskBillingStateConflict)
}
