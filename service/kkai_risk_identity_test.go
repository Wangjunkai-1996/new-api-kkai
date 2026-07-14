package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIRiskActionRejectsMismatchedTokenFingerprint(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	input := riskActionInput(user, token, channel)
	input.TokenFingerprint = RiskFingerprint("different-token")

	_, err := NewRiskActionService(db).Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionIdentityMismatch)

	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, token.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)

	var incidents int64
	require.NoError(t, db.Model(&model.KKAIPolicyIncident{}).Count(&incidents).Error)
	assert.Zero(t, incidents)
}

func TestKKAIRiskActionRejectsMismatchedChannelFingerprint(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	input := riskActionInput(user, token, channel)
	input.Actions = RiskDurableActions{DisableChannel: true}
	input.UpstreamKeyFingerprint = RiskFingerprint("different-upstream-key")

	_, err := NewRiskActionService(db).Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionIdentityMismatch)

	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)

	var incidents int64
	require.NoError(t, db.Model(&model.KKAIPolicyIncident{}).Count(&incidents).Error)
	assert.Zero(t, incidents)
}
