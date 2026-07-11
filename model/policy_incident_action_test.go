package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistPolicyIncidentClientDisableRollsBackWhenEventInsertFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&PolicyIncidentEvent{}))

	user := &User{
		Username: "policy-rollback-user",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Group:    "default",
		AffCode:  "policy-rollback-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	token := &Token{
		UserId: user.Id,
		Name:   "policy-rollback-token",
		Key:    "policy-rollback-key",
		Status: common.TokenStatusEnabled,
	}
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Exec(`
		CREATE TRIGGER reject_policy_incident_event
		BEFORE INSERT ON policy_incident_events
		BEGIN
			SELECT RAISE(FAIL, 'forced policy incident event failure');
		END
	`).Error)
	t.Cleanup(func() {
		DB.Exec("DROP TRIGGER IF EXISTS reject_policy_incident_event")
	})
	event := &PolicyIncidentEvent{RequestId: "req-policy-rollback"}

	err := PersistPolicyIncidentClientDisable(event, token.Id, user.Id)
	require.Error(t, err)

	var reloadedToken Token
	require.NoError(t, DB.First(&reloadedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloadedToken.Status)
	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, reloadedUser.Status)
	var eventCount int64
	require.NoError(t, DB.Model(&PolicyIncidentEvent{}).Where("request_id = ?", event.RequestId).Count(&eventCount).Error)
	assert.Zero(t, eventCount)
}

func TestPersistPolicyIncidentClientDisableStopsWhenTokenLookupFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&PolicyIncidentEvent{}))

	user := &User{
		Username: "policy-missing-token-user",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Group:    "default",
		AffCode:  "policy-missing-token-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	event := &PolicyIncidentEvent{RequestId: "req-policy-missing-token"}

	err := PersistPolicyIncidentClientDisable(event, 999999, user.Id)
	require.Error(t, err)

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, reloadedUser.Status)
	var eventCount int64
	require.NoError(t, DB.Model(&PolicyIncidentEvent{}).Where("request_id = ?", event.RequestId).Count(&eventCount).Error)
	assert.Zero(t, eventCount)
}

func TestPersistPolicyIncidentClientDisableRollsBackTokenWhenUserLookupFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&PolicyIncidentEvent{}))

	const missingUserId = 999998
	token := &Token{
		UserId: missingUserId,
		Name:   "policy-missing-user-token",
		Key:    "policy-missing-user-key",
		Status: common.TokenStatusEnabled,
	}
	require.NoError(t, DB.Create(token).Error)
	event := &PolicyIncidentEvent{RequestId: "req-policy-missing-user"}

	err := PersistPolicyIncidentClientDisable(event, token.Id, missingUserId)
	require.Error(t, err)

	var reloadedToken Token
	require.NoError(t, DB.First(&reloadedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloadedToken.Status)
	var eventCount int64
	require.NoError(t, DB.Model(&PolicyIncidentEvent{}).Where("request_id = ?", event.RequestId).Count(&eventCount).Error)
	assert.Zero(t, eventCount)
}
