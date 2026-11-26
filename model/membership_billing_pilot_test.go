package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipBillingPilotAllowlistCanBeEnabledAndStopped(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	t.Setenv("KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", "10")
	require.True(t, MembershipPilotHoldEnabled(10))
	require.NoError(t, SyncOptionsOnce())
	enabled, standard := membershipBillingPolicy(membershipEarlyReleaseUsers, 99887)
	assert.True(t, enabled && standard, "new members use the default after database settings load")
	_, standard = membershipBillingPolicy(membershipHoldUsers, 10)
	assert.False(t, standard, "the existing environment list keeps pilot semantics")
	require.NoError(t, UpdateOption("MembershipPilotHoldUserIDs", "1, 20"))
	assert.True(t, MembershipPilotHoldEnabled(1))
	assert.False(t, MembershipPilotHoldEnabled(10))
	_, standard = membershipBillingPolicy(membershipHoldUsers, 1)
	assert.False(t, standard)
	assert.Error(t, UpdateOption("MembershipPilotHoldUserIDs", "*,1"))
	require.NoError(t, UpdateOption("MembershipPilotHoldUserIDs", "*"))
	_, standard = membershipBillingPolicy(membershipHoldUsers, 99123)
	assert.True(t, standard)
	assert.True(t, MembershipPilotHoldEnabled(99123))
	assert.False(t, MembershipPilotHoldEnabled(0))
	require.NoError(t, UpdateOption("MembershipPilotHoldUserIDs", ""))
	assert.False(t, MembershipPilotHoldEnabled(1))
	assert.False(t, MembershipPilotHoldEnabled(10))
	require.NoError(t, SyncOptionsOnce())
	assert.False(t, MembershipPilotHoldEnabled(10), "a persisted empty value survives refresh")
}

func TestMembershipBillingPilotBudgetIsCumulative(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	spend := func(kind string, userID int, quota int64, quotaPerUnit float64) bool {
		t.Helper()
		var allowed bool
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			var err error
			allowed, err = spendMembershipPilotBudgetTx(tx, kind, userID, quota, quotaPerUnit)
			return err
		}))
		return allowed
	}

	assert.False(t, spend(membershipPilotEarlyRelease, 1, 601, 100))
	for userID := 1; userID <= 3; userID++ {
		assert.True(t, spend(membershipPilotEarlyRelease, userID, 600, 100))
		for range 4 {
			assert.True(t, spend(membershipPilotEarlyRelease, userID, 1200, 200))
		}
		if userID < 3 {
			assert.False(t, spend(membershipPilotEarlyRelease, userID, 600, 100))
			assert.True(t, spend(membershipPilotEarlyRelease, userID, 500, 100))
			assert.False(t, spend(membershipPilotEarlyRelease, userID, 1, 100))
		}
	}
	assert.False(t, spend(membershipPilotEarlyRelease, 4, 1, 100))

	rolledBack := errors.New("cancel billing transaction")
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		allowed, err := spendMembershipPilotBudgetTx(tx, membershipPilotHold, 1, 200, 100)
		require.NoError(t, err)
		require.True(t, allowed)
		return rolledBack
	}), rolledBack)
	for range 10 {
		assert.True(t, spend(membershipPilotHold, 1, 200, 100))
	}
	assert.False(t, spend(membershipPilotHold, 1, 1, 100))
	assert.True(t, spend(membershipPilotHold, 2, 1, 1_000_001))
	var counter Option
	require.NoError(t, db.First(&counter, map[string]any{"key": membershipPilotHold}).Error)
	assert.JSONEq(t, `{"1":20000000,"2":1}`, counter.Value)
}

func TestMembershipBillingPilotBudgetsAreInternalOptions(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{Key: membershipPilotHold, Value: `{"1":1000000}`}).Error)
	require.NoError(t, db.Create(&Option{Key: membershipPilotEarlyRelease, Value: `{"1":2000000}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "VisibleOption", Value: "visible"}).Error)

	assert.Error(t, UpdateOption(membershipStandardHoldAdmission, "{}"))
	assert.Error(t, UpdateOption(membershipPilotHold, "{}"))
	assert.Error(t, UpdateOptionsBulk(map[string]string{membershipPilotEarlyRelease: "{}"}))
	assert.Error(t, UpdateOption(strings.ToLower(membershipPilotHold), "{}"))
	options, err := AllOption()
	require.NoError(t, err)
	require.Len(t, options, 1)
	assert.Equal(t, "VisibleOption", options[0].Key)
}
