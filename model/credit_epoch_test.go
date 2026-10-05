package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTestCreditEpoch(t *testing.T, boundaries map[string]int64) {
	t.Helper()
	data, err := common.Marshal(common.CreditEpochCutover{
		Version: 1, MigrationID: "test-usd", SourceEpoch: common.CreditEpochLegacy,
		TargetEpoch: common.CreditEpochUSD, Numerator: 3, Denominator: 40,
		PlanHash: "wallet-proof", PricingPlanHash: "pricing-proof", AppliedAt: 100,
		LegacyMaxIDs:      boundaries,
		LegacyTopUpPolicy: "hold",
	})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.CreditEpochOption: string(data)}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

func TestCreditEpochRedemptionHistoryDoesNotDoubleConvert(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&Redemption{}))
	rows := []Redemption{
		{Id: 1, Key: "used-before-cutover", Quota: 665000000, Status: common.RedemptionCodeStatusUsed},
		{Id: 2, Key: "used-after-cutover", Quota: 49875000, Status: common.RedemptionCodeStatusUsed},
		{Id: 3, Key: "new-voucher", Quota: 500000, Status: common.RedemptionCodeStatusEnabled},
	}
	require.NoError(t, db.Create(&rows).Error)
	setTestCreditEpoch(t, map[string]int64{"redemptions": 2})
	epoch, err := common.CurrentCreditEpoch()
	require.NoError(t, err)
	epoch.LegacyUsedRedemptionIDs = []int64{1}
	raw, err := common.Marshal(epoch)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[common.CreditEpochOption] = string(raw)
	common.OptionMapRWMutex.Unlock()
	list := []*Redemption{&rows[0], &rows[1], &rows[2]}
	require.NoError(t, NormalizeRedemptionCreditEpoch(list))
	require.NoError(t, NormalizeRedemptionCreditEpoch(list))
	assert.Equal(t, 49875000, rows[0].Quota)
	require.NotNil(t, rows[0].OriginalQuota)
	assert.Equal(t, 665000000, *rows[0].OriginalQuota)
	assert.Equal(t, 49875000, rows[1].Quota)
	assert.Nil(t, rows[1].OriginalQuota)
	assert.Equal(t, 500000, rows[2].Quota)
	var stored Redemption
	require.NoError(t, db.First(&stored, 1).Error)
	assert.Equal(t, 665000000, stored.Quota)
}

func TestCreditEpochOldProblemOrdersRemainOnHold(t *testing.T) {
	for _, provider := range []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo} {
		t.Run(provider, func(t *testing.T) {
			db := setupTopUpFinalizeTestDB(t)
			order := seedTopUpFinalizeFixture(t, db)
			order.PaymentProvider = provider
			require.NoError(t, db.Save(&order).Error)
			setTestCreditEpoch(t, map[string]int64{"top_ups": int64(order.Id)})
			input := FinalizeTopUpInput{TradeNo: order.TradeNo, ExpectedProvider: provider, Prepare: prepareManualTopUpCompletion}
			result, err := FinalizeTopUp(input)
			require.ErrorIs(t, err, ErrTopUpLegacyReviewRequired)
			assert.Nil(t, result)
			var saved TopUp
			require.NoError(t, db.First(&saved, order.Id).Error)
			assert.Equal(t, order, saved)
			replay, err := FinalizeTopUp(input)
			require.ErrorIs(t, err, ErrTopUpLegacyReviewRequired)
			assert.Nil(t, replay)
			var user User
			require.NoError(t, db.First(&user, order.UserId).Error)
			assert.Equal(t, int64(10), user.Quota)
			var events int64
			require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&events).Error)
			assert.Zero(t, events)
			// A previously completed order still has a read-only replay path.
			require.NoError(t, db.Model(&order).Update("status", common.TopUpStatusSuccess).Error)
			completed, err := FinalizeTopUp(input)
			require.NoError(t, err)
			assert.True(t, completed.AlreadyCompleted)
		})
	}
}

func TestCreditEpochNewTopUpIsNotScaled(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	order := seedTopUpFinalizeFixture(t, db)
	setTestCreditEpoch(t, map[string]int64{"top_ups": int64(order.Id - 1)})
	result, err := FinalizeTopUp(FinalizeTopUpInput{TradeNo: order.TradeNo, Prepare: prepareManualTopUpCompletion})
	require.NoError(t, err)
	want, err := prepareManualTopUpCompletion(&order, nil)
	require.NoError(t, err)
	assert.Equal(t, want.QuotaDelta, result.QuotaDelta)
}

func TestCreditEpochHistoryPreservesRowsAndNormalizesStatistics(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })
	require.NoError(t, db.AutoMigrate(&Log{}, &Checkin{}))
	setTestCreditEpoch(t, map[string]int64{"logs": 1, "checkins": 1})
	original := Log{Id: 1, UserId: 1, Type: LogTypeConsume, Quota: 1330, Other: `{"model_price":7}`, CreatedAt: 100}
	require.NoError(t, db.Create(&original).Error)
	require.NoError(t, db.Create(&Log{Id: 2, UserId: 1, Type: LogTypeConsume, Quota: 5, CreatedAt: 101}).Error)
	rows, _, err := GetUserLogs(1, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 5, rows[0].Quota)
	assert.Equal(t, 100, rows[1].Quota)
	assert.Equal(t, common.CreditEpochLegacy, rows[1].PricingEpoch)
	require.NotNil(t, rows[1].OriginalQuota)
	assert.Equal(t, 1330, *rows[1].OriginalQuota)
	stats, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 105, stats.Quota)
	var raw Log
	require.NoError(t, db.First(&raw, 1).Error)
	assert.Equal(t, original.Quota, raw.Quota)
	assert.Equal(t, original.Other, raw.Other)
	require.NoError(t, db.Create(&Checkin{Id: 1, UserId: 1, CheckinDate: "2026-10-01", QuotaAwarded: 40}).Error)
	require.NoError(t, db.Create(&Checkin{Id: 2, UserId: 1, CheckinDate: "2026-10-02", QuotaAwarded: 4}).Error)
	checkinStats, err := GetUserCheckinStats(1, "2026-10")
	require.NoError(t, err)
	assert.Equal(t, int64(7), checkinStats["total_quota"])
	var rawCheckin Checkin
	require.NoError(t, db.First(&rawCheckin, 1).Error)
	assert.Equal(t, 40, rawCheckin.QuotaAwarded)
}

func TestCreditEpochReversalUsesRecordedWalletDelta(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&KKAIInternalBalanceAdjustment{}))
	require.NoError(t, db.Create(&User{Id: 1, Username: "epoch-user", Quota: 100}).Error)
	setTestCreditEpoch(t, map[string]int64{"kkai_internal_balance_adjustments": 1})
	old := KKAIInternalBalanceAdjustment{ID: 1, OperationID: "old", UserID: 1, Delta: 40,
		Reason: KKAIBalanceAdjustmentReasonCredit, Metadata: "{}", PayloadSHA256: strings.Repeat("a", 64),
		BalanceBefore: 1290, BalanceAfter: 1330, CreatedAt: 50}
	require.NoError(t, db.Create(&old).Error)
	for _, tc := range []struct{ delta, wallet int64 }{{14, 1}, {1, 0}} {
		operation := fmt.Sprintf("late-%d", tc.delta)
		input := KKAIBalanceAdjustmentInput{OperationID: operation, UserID: 1, Delta: tc.delta,
			Reason: KKAIBalanceAdjustmentReasonCredit, Metadata: "{}", PayloadSHA256: strings.Repeat("b", 64),
			CreatedAt: 101, QuotaEpoch: common.CreditEpochLegacy}
		credited, err := ApplyKKAIBalanceAdjustment(input)
		require.NoError(t, err)
		require.NotNil(t, credited.Adjustment.WalletDelta)
		assert.Equal(t, tc.wallet, *credited.Adjustment.WalletDelta)
		reversal := KKAIBalanceAdjustmentInput{OperationID: operation + "-reverse", UserID: 1, Delta: -tc.delta,
			Reason: KKAIBalanceAdjustmentReasonReversal, Metadata: "{}", PayloadSHA256: strings.Repeat("c", 64),
			OriginalOperationID: &operation, CreatedAt: 102}
		reversed, err := ApplyKKAIBalanceAdjustment(reversal)
		require.NoError(t, err)
		assert.Equal(t, -tc.wallet, *reversed.Adjustment.WalletDelta)
		replayed, err := ApplyKKAIBalanceAdjustment(reversal)
		require.NoError(t, err)
		assert.True(t, replayed.Replayed)
	}
	oldOperation := old.OperationID
	result, err := ApplyKKAIBalanceAdjustment(KKAIBalanceAdjustmentInput{
		OperationID: "old-reverse", UserID: 1, Delta: -40, Reason: KKAIBalanceAdjustmentReasonReversal,
		Metadata: "{}", PayloadSHA256: strings.Repeat("d", 64), OriginalOperationID: &oldOperation, CreatedAt: 103,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(-3), *result.Adjustment.WalletDelta)
	var unchanged KKAIInternalBalanceAdjustment
	require.NoError(t, db.First(&unchanged, old.ID).Error)
	assert.Equal(t, old, unchanged)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, int64(97), user.Quota)
	_, err = ApplyKKAIBalanceAdjustment(KKAIBalanceAdjustmentInput{
		OperationID: "ambiguous", UserID: 1, Delta: 50, Reason: KKAIBalanceAdjustmentReasonCredit,
		Metadata: "{}", PayloadSHA256: strings.Repeat("e", 64), CreatedAt: 104,
	})
	assert.ErrorIs(t, err, common.ErrCreditEpochInvalid)
	assert.Error(t, validateOptionValue(common.CreditEpochOption, "{}"))
}
