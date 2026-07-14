package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type kkaiPolicyGuardTestApplier struct {
	inputs []RiskActionInput
	err    error
}

func (a *kkaiPolicyGuardTestApplier) Apply(_ context.Context, input RiskActionInput) (*RiskActionResult, error) {
	a.inputs = append(a.inputs, input)
	if a.err != nil {
		return nil, a.err
	}
	return &RiskActionResult{IncidentID: 42}, nil
}

func newKKAIPolicyGuardTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test"}`))
	ctx.Set(common.RequestIdKey, "req-policy-1")
	ctx.Set("id", 10)
	ctx.Set("token_id", 11)
	ctx.Set("token_key", "client-secret")
	ctx.Set("original_model", "gpt-test")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestClassifyKKAIUpstreamPolicyErrorSeparatesCausality(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		detected  bool
		causality string
	}{
		{name: "client request", message: "upstream rejected cyber_policy", detected: true, causality: KKAIPolicyCausalityClientToken},
		{name: "upstream key", message: "provider API key has been permanently disabled", detected: true, causality: KKAIPolicyCausalityUpstreamKey},
		{name: "ambiguous", message: "cyber_policy; API key has been disabled", detected: true, causality: KKAIPolicyCausalityAmbiguous},
		{name: "ordinary forbidden", message: "permission denied", detected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr := types.NewErrorWithStatusCode(errors.New(test.message), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
			classification := ClassifyKKAIUpstreamPolicyError(apiErr)
			assert.Equal(t, test.detected, classification.Detected)
			assert.Equal(t, test.causality, classification.Causality)
		})
	}
}

func TestKKAIPolicyGuardDisablesOnlyConfirmedClientAttribution(t *testing.T) {
	ctx := newKKAIPolicyGuardTestContext(t)
	applier := &kkaiPolicyGuardTestApplier{}
	guard := NewKKAIPolicyIncidentGuard(applier)
	guard.now = func() time.Time { return time.Unix(1_720_000_000, 0) }
	channel := *types.NewChannelError(12, 1, "policy-channel", false, "upstream-secret", true)
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	detected, err := guard.HandleAPIError(ctx, channel, apiErr)
	require.NoError(t, err)
	require.True(t, detected)
	require.True(t, ShouldSkipRetryAfterKKAIPolicy(ctx))
	require.Len(t, applier.inputs, 1)

	input := applier.inputs[0]
	assert.Equal(t, RiskDecisionDisable, input.Decision)
	assert.Equal(t, RiskDurableActions{DisableToken: true, DisableUser: true}, input.Actions)
	assert.Equal(t, "sha256:"+strings.TrimPrefix(RiskFingerprint("client-secret"), "sha256:"), input.TokenFingerprint)
	assert.Equal(t, RiskFingerprint("upstream-secret"), input.UpstreamKeyFingerprint)
	assert.NotContains(t, input.EventID, "secret")
	assert.NotContains(t, input.Metadata, "secret")
	assert.NotEmpty(t, input.Metadata["request_body_sha256"])
	assert.NotEmpty(t, ctx.GetString(KKAIPolicyCaseContextKey))
}

func TestKKAIPolicyGuardRecordsAmbiguousAttributionWithoutDisabling(t *testing.T) {
	ctx := newKKAIPolicyGuardTestContext(t)
	applier := &kkaiPolicyGuardTestApplier{}
	guard := NewKKAIPolicyIncidentGuard(applier)
	channel := *types.NewChannelError(12, 1, "policy-channel", false, "upstream-secret", true)
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy; API key is disabled"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	detected, err := guard.HandleAPIError(ctx, channel, apiErr)
	require.NoError(t, err)
	require.True(t, detected)
	require.Len(t, applier.inputs, 1)
	assert.Equal(t, RiskDecisionReject, applier.inputs[0].Decision)
	assert.Equal(t, RiskDurableActions{}, applier.inputs[0].Actions)
}

func TestKKAIPolicyGuardKeepsRetryBlockedWhenAuditWriteFails(t *testing.T) {
	ctx := newKKAIPolicyGuardTestContext(t)
	expected := errors.New("database unavailable")
	guard := NewKKAIPolicyIncidentGuard(&kkaiPolicyGuardTestApplier{err: expected})
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	detected, err := guard.HandleAPIError(ctx, types.ChannelError{}, apiErr)
	require.True(t, detected)
	require.ErrorIs(t, err, expected)
	require.True(t, ShouldSkipRetryAfterKKAIPolicy(ctx))
}

func TestKKAIPolicyGuardPersistsClientActionWithoutDisablingChannel(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	ctx := newKKAIPolicyGuardTestContext(t)
	ctx.Set("id", user.Id)
	ctx.Set("token_id", token.Id)
	ctx.Set("token_key", token.Key)
	guard := NewKKAIPolicyIncidentGuard(NewRiskActionService(db))
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	detected, err := guard.HandleAPIError(ctx, *types.NewChannelError(channel.Id, 1, channel.Name, false, channel.Key, true), apiErr)
	require.NoError(t, err)
	require.True(t, detected)

	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, token.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, user.Status)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)

	var incident model.KKAIPolicyIncident
	require.NoError(t, db.Where("request_id = ?", "req-policy-1").First(&incident).Error)
	assert.Equal(t, RiskDecisionDisable, incident.Decision)
	assert.Equal(t, "disable_token,disable_user", incident.ActionTaken)
	assert.NotContains(t, incident.Metadata, token.Key)
	assert.NotContains(t, incident.Metadata, channel.Key)
}

func TestKKAIPolicyGuardRecordsUpstreamKeyWithoutDisablingTargets(t *testing.T) {
	db := newRiskActionTestDB(t)
	user, token, channel := seedRiskActionTargets(t, db, common.RoleCommonUser)
	ctx := newKKAIPolicyGuardTestContext(t)
	ctx.Set("id", user.Id)
	ctx.Set("token_id", token.Id)
	guard := NewKKAIPolicyIncidentGuard(NewRiskActionService(db))
	apiErr := types.NewErrorWithStatusCode(errors.New("API key has been disabled"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	detected, err := guard.HandleAPIError(ctx, *types.NewChannelError(channel.Id, 1, channel.Name, false, channel.Key, true), apiErr)
	require.NoError(t, err)
	require.True(t, detected)

	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, token.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)

	var incident model.KKAIPolicyIncident
	require.NoError(t, db.Where("request_id = ?", "req-policy-1").First(&incident).Error)
	assert.Equal(t, RiskDecisionReject, incident.Decision)
	assert.Equal(t, "record_incident", incident.ActionTaken)
}
