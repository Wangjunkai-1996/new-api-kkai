package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIPolicyGuardRecordsUnconfirmedMarkersWithoutDisabling(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		code      types.ErrorCode
		causality string
	}{
		{
			name:      "plain text cyber marker",
			message:   "cyber_policy",
			code:      types.ErrorCodeBadResponseStatusCode,
			causality: KKAIPolicyCausalityAmbiguous,
		},
		{
			name:      "upstream key marker",
			message:   "provider API key has been permanently disabled",
			code:      types.ErrorCodeBadResponseStatusCode,
			causality: KKAIPolicyCausalityUpstreamKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newKKAIPolicyGuardTestContext(t)
			applier := &kkaiPolicyGuardTestApplier{}
			guard := NewKKAIPolicyIncidentGuardWithKeyCooldown(applier, &fakeKKAIPolicyKeyCooldownStore{})
			apiErr := newUpstreamPolicyTestError(test.message, test.code, http.StatusUnauthorized)

			detected, err := guard.HandleAPIError(ctx, types.ChannelError{ChannelId: 12}, apiErr)
			require.NoError(t, err)
			require.True(t, detected)
			require.True(t, ShouldSkipRetryAfterKKAIPolicy(ctx))
			require.Len(t, applier.inputs, 1)
			assert.Equal(t, test.causality, applier.inputs[0].Metadata["causality"])
			assert.Equal(t, RiskDecisionReject, applier.inputs[0].Decision)
			assert.Equal(t, RiskDurableActions{}, applier.inputs[0].Actions)
		})
	}
}

func TestKKAIPolicyGuardAuthorizesStructuredCyberOnBadRequest(t *testing.T) {
	ctx := newKKAIPolicyGuardTestContext(t)
	applier := &kkaiPolicyGuardTestApplier{}
	cooldown := &fakeKKAIPolicyKeyCooldownStore{}
	guard := NewKKAIPolicyIncidentGuardWithKeyCooldown(applier, cooldown)
	channel := *types.NewChannelError(12, 1, "policy-channel", false, "upstream-secret", true)
	apiErr := newUpstreamPolicyTestError("request rejected", types.ErrorCode("cyber_policy"), http.StatusBadRequest)

	detected, err := guard.HandleAPIError(ctx, channel, apiErr)
	require.NoError(t, err)
	require.True(t, detected)
	require.True(t, ShouldSkipRetryAfterKKAIPolicy(ctx))
	require.Len(t, applier.inputs, 1)
	assert.Equal(t, KKAIPolicyCausalityClientToken, applier.inputs[0].Metadata["causality"])
	assert.Equal(t, http.StatusBadRequest, applier.inputs[0].Metadata["original_status_code"])
	assert.Equal(t, RiskDecisionReject, applier.inputs[0].Decision)
	assert.Equal(t, RiskDurableActions{}, applier.inputs[0].Actions)
	require.Len(t, cooldown.recordKeys, 1)
}

func TestKKAIPolicyEventIDWithoutRequestIDIsScopedToClientIdentity(t *testing.T) {
	now := time.Unix(1_720_000_000, 123)
	first := newKKAIPolicyGuardTestContext(t)
	first.Set(common.RequestIdKey, "")
	first.Set("id", 101)
	first.Set("token_id", 201)
	second := newKKAIPolicyGuardTestContext(t)
	second.Set(common.RequestIdKey, "")
	second.Set("id", 102)
	second.Set("token_id", 202)

	firstID := kkaiPolicyEventID(first, 12, now)
	secondID := kkaiPolicyEventID(second, 12, now)

	require.NotEqual(t, firstID, secondID)
	require.True(t, riskEventIDPattern.MatchString(firstID))
	require.True(t, riskEventIDPattern.MatchString(secondID))
}

func TestClassifyKKAIPolicyErrorsRejectsLocalPolicyCodes(t *testing.T) {
	for _, code := range []types.ErrorCode{
		types.ErrorCodeRequestPolicyBlocked,
		types.ErrorCodePolicyContextIncomplete,
		types.ErrorCodePolicyAuditUnavailable,
		types.ErrorCodeSessionBlockedByCyberPolicy,
	} {
		t.Run(string(code), func(t *testing.T) {
			apiErr := types.NewErrorWithStatusCode(
				errors.New(string(code)+" cyber_policy"),
				code,
				http.StatusForbidden,
				types.ErrOptionWithOriginalStatusCode(http.StatusForbidden),
				types.ErrOptionWithPolicyEvidence(string(code)+" cyber_policy"),
			)
			assert.False(t, ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
			assert.False(t, ClassifyKKAITaskPolicyError(&dto.TaskError{
				Code:               string(code),
				StatusCode:         http.StatusForbidden,
				UpstreamStatusCode: http.StatusForbidden,
				PolicyEvidence:     string(code) + " cyber_policy",
			}).Detected)
		})
	}
}

func TestClassifyKKAIPolicyErrorDoesNotTrustLocalCodeInEvidence(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("upstream policy rejection"),
		types.ErrorCode("cyber_policy"),
		http.StatusForbidden,
		types.ErrOptionWithOriginalStatusCode(http.StatusForbidden),
		types.ErrOptionWithPolicyEvidence("cyber_policy session_blocked_by_cyber_policy"),
	)

	classification := ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	assert.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
}

func TestKKAIPolicyGuardTreatsOnlyExplicitPlaygroundAsUserOnly(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantDecision string
		wantActions  RiskDurableActions
	}{
		{name: "ordinary internal request", path: "/v1/chat/completions", wantDecision: RiskDecisionReject},
		{name: "playground request", path: "/pg/chat/completions", wantDecision: RiskDecisionReject},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newKKAIPolicyGuardTestContext(t)
			ctx.Request.URL.Path = test.path
			ctx.Set("token_id", 0)
			ctx.Set("token_key", "")
			applier := &kkaiPolicyGuardTestApplier{}
			cooldown := &fakeKKAIPolicyKeyCooldownStore{}
			guard := NewKKAIPolicyIncidentGuardWithKeyCooldown(applier, cooldown)
			channel := *types.NewChannelError(12, 1, "policy-channel", false, "upstream-secret", true)

			detected, err := guard.HandleAPIError(ctx, channel, newUpstreamPolicyTestError("cyber_policy", types.ErrorCode("cyber_policy"), http.StatusForbidden))
			require.NoError(t, err)
			require.True(t, detected)
			require.Len(t, applier.inputs, 1)
			assert.Equal(t, test.wantDecision, applier.inputs[0].Decision)
			assert.Equal(t, test.wantActions, applier.inputs[0].Actions)
			assert.Empty(t, cooldown.recordKeys)
		})
	}
}
