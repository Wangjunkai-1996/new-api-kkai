package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreditEpochDeadFinancialEventsStayOnHold(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Now()
	old := seedOutboxEvent(t, db, "task.billing.recovery.v1", now.Unix())
	require.NoError(t, db.Model(&old).Update("status", model.KKAIOutboxStatusDead).Error)
	marker, err := common.Marshal(common.CreditEpochCutover{Version: 1, MigrationID: "outbox-test", SourceEpoch: common.CreditEpochLegacy, TargetEpoch: common.CreditEpochUSD, Numerator: 3, Denominator: 40, PlanHash: "p", PricingPlanHash: "prices", LegacyMaxIDs: map[string]int64{"kkai_outbox": old.ID}, AppliedAt: now.Unix(), LegacyTopUpPolicy: "hold"})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.CreditEpochOption: string(marker)}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })
	_, applied, err := RedriveKKAIOutboxDeadEvent(t.Context(), db, old.ID, "retry", "admin:1", now)
	require.ErrorIs(t, err, ErrKKAILegacyOutboxHeld)
	assert.False(t, applied)
	var unchanged model.KKAIOutboxEvent
	require.NoError(t, db.First(&unchanged, old.ID).Error)
	assert.Equal(t, model.KKAIOutboxStatusDead, unchanged.Status)
	assert.Equal(t, old.Payload, unchanged.Payload)
	newEvent := seedOutboxEvent(t, db, "new.financial.event", now.Unix())
	require.NoError(t, db.Model(&newEvent).Update("status", model.KKAIOutboxStatusDead).Error)
	_, applied, err = RedriveKKAIOutboxDeadEvent(t.Context(), db, newEvent.ID, "new-retry", "admin:1", now)
	require.NoError(t, err)
	assert.True(t, applied)
}
