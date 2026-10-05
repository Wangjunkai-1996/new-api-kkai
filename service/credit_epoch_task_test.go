package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreditEpochLegacyTaskCannotMoveNewWalletFunds(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 10000)
	seedChannel(t, 1)
	task := makeTask(1, 1, 3000, 0, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	marker, err := common.Marshal(common.CreditEpochCutover{Version: 1, MigrationID: "task-test", SourceEpoch: common.CreditEpochLegacy, TargetEpoch: common.CreditEpochUSD, Numerator: 3, Denominator: 40, PlanHash: "p", PricingPlanHash: "prices", LegacyMaxIDs: map[string]int64{"tasks": task.ID}, AppliedAt: 1, LegacyTopUpPolicy: "hold"})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.CreditEpochOption: string(marker)}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })

	assert.False(t, RefundTaskQuota(t.Context(), task, "old retry"))
	RecalculateTaskQuota(t.Context(), task, 5000, "old settlement")
	_, err = model.RefundTaskBilling(t.Context(), task.ID)
	require.ErrorIs(t, err, model.ErrLegacyTaskBillingHeld)
	_, err = model.AdjustTaskBilling(t.Context(), task.ID, 5000)
	require.ErrorIs(t, err, model.ErrLegacyTaskBillingHeld)
	assert.EqualValues(t, 10000, getUserQuota(t, 1))
	assert.Equal(t, 3000, getTaskQuota(t, task.ID))

	current := makeTask(1, 1, 3000, 0, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(current).Error)
	assert.True(t, RefundTaskQuota(t.Context(), current, "current refund"))
	assert.EqualValues(t, 13000, getUserQuota(t, 1))
}
