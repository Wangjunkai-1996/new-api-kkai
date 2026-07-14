package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newRiskActionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:risk-action-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.KKAIPolicyIncident{},
		&model.KKAIOutboxEvent{},
	))
	return db
}

func seedRiskActionTargets(t *testing.T, db *gorm.DB, role int) (model.User, model.Token, model.Channel) {
	t.Helper()
	user := model.User{
		Username: fmt.Sprintf("risk-user-%d", time.Now().UnixNano()),
		Password: "not-used-in-test",
		Role:     role,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{
		UserId: user.Id,
		Key:    fmt.Sprintf("risk-token-%d", time.Now().UnixNano()),
		Name:   "risk test token",
		Status: common.TokenStatusEnabled,
	}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{
		Name:   fmt.Sprintf("risk-channel-%d", time.Now().UnixNano()),
		Key:    "upstream-test-key",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(&channel).Error)
	return user, token, channel
}

func riskActionInput(user model.User, token model.Token, channel model.Channel) RiskActionInput {
	evidence := RiskFingerprint("sanitized evidence")
	return RiskActionInput{
		EventID:                "risk-event-0001",
		Source:                 RiskSourceUpstreamPolicy,
		OccurredAt:             1_720_000_000,
		RequestID:              "request-0001",
		UserID:                 user.Id,
		TokenID:                token.Id,
		ChannelID:              channel.Id,
		ModelName:              "gpt-test",
		RuleVersion:            "policy-v1",
		EvidenceSHA256:         evidence,
		TokenFingerprint:       RiskFingerprint(token.Key),
		UpstreamKeyFingerprint: RiskFingerprint(channel.Key),
		Decision:               RiskDecisionDisable,
		Metadata: map[string]any{
			"case_id":        "policy-case-0001",
			"evidence_level": "confirmed",
		},
		Actions: RiskDurableActions{
			DisableToken:   true,
			DisableUser:    true,
			DisableChannel: true,
		},
	}
}

func TestRiskActionServiceAppliesAllDurableChangesAtomically(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	service.now = func() time.Time { return time.Unix(1_720_000_100, 0) }

	result, err := service.Apply(context.Background(), riskActionInput(user, token, channel))
	require.NoError(t, err)
	require.False(t, result.Replayed)
	require.True(t, result.TokenDisabled)
	require.True(t, result.UserDisabled)
	require.True(t, result.ChannelDisabled)

	require.NoError(t, db.First(&token, token.Id).Error)
	require.Equal(t, common.TokenStatusDisabled, token.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, user.Status)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)

	var incident model.KKAIPolicyIncident
	require.NoError(t, db.Where("event_id = ?", "risk-event-0001").First(&incident).Error)
	require.Equal(t, result.IncidentID, incident.ID)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, incident.TokenFingerprint)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, incident.UpstreamKeyFingerprint)
	require.NotContains(t, incident.Metadata, token.Key)
	require.NotContains(t, incident.Metadata, channel.Key)

	var outbox []model.KKAIOutboxEvent
	require.NoError(t, db.Find(&outbox).Error)
	require.Len(t, outbox, 1)
	require.Equal(t, model.KKAIOutboxStatusPending, outbox[0].Status)
	require.NotContains(t, outbox[0].Payload, token.Key)
	require.NotContains(t, outbox[0].Payload, channel.Key)
}

func TestRiskActionServiceReplaysSameEventWithoutRepeatingEffects(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)

	first, err := service.Apply(context.Background(), input)
	require.NoError(t, err)
	second, err := service.Apply(context.Background(), input)
	require.NoError(t, err)
	require.True(t, second.Replayed)
	require.Equal(t, first.IncidentID, second.IncidentID)
	require.True(t, second.TokenDisabled)
	require.True(t, second.UserDisabled)
	require.True(t, second.ChannelDisabled)

	var incidentCount int64
	require.NoError(t, db.Model(&model.KKAIPolicyIncident{}).Count(&incidentCount).Error)
	require.EqualValues(t, 1, incidentCount)
	var outboxCount int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.EqualValues(t, 1, outboxCount)
}

func TestRiskActionServiceRejectsConflictingReplay(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)
	require.NoError(t, applyRiskActionForTest(service, input))

	input.Decision = RiskDecisionObserve
	_, err := service.Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionIdempotencyConflict)
}

func TestRiskActionServiceRollsBackIncidentWhenActionFails(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)
	input.TokenID = token.Id + 10000

	_, err := service.Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionTokenNotFound)

	var incidentCount int64
	require.NoError(t, db.Model(&model.KKAIPolicyIncident{}).Count(&incidentCount).Error)
	require.Zero(t, incidentCount)
	var outboxCount int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestRiskActionServiceNeverDisablesPrivilegedUser(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleAdminUser)
	service := NewRiskActionService(db)

	result, err := service.Apply(context.Background(), riskActionInput(user, token, channel))
	require.NoError(t, err)
	require.False(t, result.UserDisabled)
	require.True(t, result.UserDisableSkipped)

	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, common.UserStatusEnabled, user.Status)
}

func TestRiskActionServiceRejectsSensitiveOrMalformedMetadata(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)
	input.Metadata["authorization"] = "Bearer secret"

	_, err := service.Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionInvalidInput)
}

func TestRiskActionServiceRequiresRequestBodyDigestPair(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)
	input.Metadata["request_body_sha256"] = RiskFingerprint("body")

	_, err := service.Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrRiskActionInvalidInput)
}

func TestRiskFingerprintUsesSHA256AndNeverReturnsRawSecret(t *testing.T) {
	fingerprint := RiskFingerprint("sk-secret-value")
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, fingerprint)
	require.NotContains(t, fingerprint, "secret")
	require.Empty(t, RiskFingerprint("  "))
}

func applyRiskActionForTest(service *RiskActionService, input RiskActionInput) error {
	_, err := service.Apply(context.Background(), input)
	return err
}

func TestRiskActionServiceRequiresMatchingTokenOwner(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	service := NewRiskActionService(db)
	input := riskActionInput(user, token, channel)
	input.UserID = user.Id + 999
	input.Actions.DisableUser = false

	_, err := service.Apply(context.Background(), input)
	require.True(t, errors.Is(err, ErrRiskActionTokenNotFound))
}
