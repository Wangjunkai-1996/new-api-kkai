package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyMembershipFailedAuthorizationLeavesNoTask(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Midjourney{}))
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "mj-atomic-reserve")
	token := Token{UserId: user.Id, Key: "mj-token-small", RemainQuota: 5, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "replaced", 80, 90)
	input.TokenID, input.TokenKey, input.SkipToken = token.Id, token.Key, false
	task := &Midjourney{UserId: user.Id, ChannelId: 1, Action: "IMAGINE"}
	_, err := CreateMidjourneyMembershipTask(task, input)
	assert.ErrorIs(t, err, ErrTaskBillingInsufficientToken)
	assert.Zero(t, task.Id)
	var count int64
	require.NoError(t, DB.Model(&Midjourney{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&MembershipBillingRecord{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 5, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestMidjourneyMembershipFailureAfterUpgradeReleasesOriginalAuthorization(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Midjourney{}))
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "mj-upgrade-original")
	require.NoError(t, DB.Model(&user).Update("quota", 0).Error)
	token := Token{UserId: user.Id, Key: "mj-token-upgrade", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "replaced", 80, 90)
	input.TokenID, input.TokenKey, input.SkipToken = token.Id, token.Key, false
	task := &Midjourney{UserId: user.Id, ChannelId: 1, Action: "IMAGINE"}
	initial, err := CreateMidjourneyMembershipTask(task, input)
	require.NoError(t, err)
	require.Equal(t, MembershipSource, initial.Source)
	task.MjId, task.Status = "mj-upgrade", "IN_PROGRESS"
	_, err = UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	_, upgraded := buyMembershipTest(t, user.Id, catalog, "diamond", "mj-upgrade-diamond")
	require.Equal(t, account.ID, upgraded.ID)
	assert.EqualValues(t, 80, upgraded.AmountReserved)
	task.Status, task.FailReason = "FAILURE", "confirmed provider failure"
	released, err := UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.Equal(t, "released", released.State)
	assert.Equal(t, initial.AccountRevision, released.AccountRevision)
	assert.Zero(t, task.Quota)
	assert.Equal(t, "100%", task.Progress)
	duplicate, err := UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.False(t, duplicate.Applied)
	require.NoError(t, DB.First(upgraded, upgraded.ID).Error)
	assert.Equal(t, "diamond", upgraded.TierID)
	assert.EqualValues(t, 300, upgraded.Available())
	assert.Zero(t, upgraded.AmountUsed)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Zero(t, user.RequestCount)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestMidjourneyMembershipTerminalAccountingRollsBackTogether(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Midjourney{}, &Channel{}))
	channel := Channel{Type: 1, Name: "mj-atomic", Key: "test"}
	require.NoError(t, DB.Create(&channel).Error)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "mj-atomic-charge")
	task := &Midjourney{UserId: user.Id, ChannelId: channel.Id, Action: "IMAGINE"}
	initial, err := CreateMidjourneyMembershipTask(task, membershipReservation(t, user.Id, "replaced", 80, 90))
	require.NoError(t, err)
	task.MjId, task.Status, task.Progress = "mj-atomic-upstream", "IN_PROGRESS", "100%"
	_, err = UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.NotEqual(t, "100%", task.Progress, "nonterminal progress must remain eligible for polling")

	// A failed statistics write must roll back the task, source and ledger.
	require.NoError(t, DB.Migrator().DropTable(&Channel{}))
	task.Status = "SUCCESS"
	_, err = UpdateMidjourneyMembershipTask(task)
	require.Error(t, err)
	record, err := GetMembershipBillingRecord(initial.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
	var stored Midjourney
	require.NoError(t, DB.First(&stored, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", stored.Status)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountUsed)
	assert.EqualValues(t, 80, account.AmountReserved)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)

	require.NoError(t, DB.AutoMigrate(&Channel{}))
	require.NoError(t, DB.Create(&channel).Error)
	// Disabling new sales/authorizations must not lose a pending task's source.
	membershipRuntimeEnabled.Store(false)
	found, err := GetMidjourneyMembershipRecord(task.Id)
	require.NoError(t, err)
	require.NotNil(t, found)
	settled, err := UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.True(t, settled.Applied)
	assert.EqualValues(t, 80, settled.ChargedQuota)
	assert.Equal(t, "SUCCESS", task.Status)
	assert.Equal(t, "100%", task.Progress)
	duplicate, err := UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.False(t, duplicate.Applied)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 80, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, DB.First(&channel, channel.Id).Error)
	assert.EqualValues(t, 80, channel.UsedQuota)
}
