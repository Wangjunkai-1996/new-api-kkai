package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPolicyIncidentTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("id", 42)
	ctx.Set("token_id", 77)
	ctx.Set("token_name", "client-token")
	ctx.Set("original_model", "gpt-policy")
	ctx.Set(common.RequestIdKey, "req-policy-test")
	common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 1)
	return ctx
}

func TestClassifyPolicyIncidentMatchesHighConfidenceText(t *testing.T) {
	tests := []string{
		"upstream rejected: cyber_policy",
		"上游返回：网络滥用封禁",
		"当前 API key 已永久禁用",
		"API key 已永久禁用",
	}

	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			err := types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

			classification := ClassifyPolicyIncident(err)

			assert.True(t, classification.Detected)
			assert.Equal(t, http.StatusForbidden, classification.StatusCode)
			assert.Equal(t, policyIncidentEvidenceHigh, classification.EvidenceLevel)
			assert.Equal(t, policyIncidentCausality, classification.Causality)
		})
	}
}

func TestClassifyPolicyIncidentDoesNotMatchOrdinaryErrors(t *testing.T) {
	err := types.NewOpenAIError(errors.New("upstream overloaded"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	classification := ClassifyPolicyIncident(err)

	assert.False(t, classification.Detected)
}

func TestPolicyIncidentSetsNoRetryEvenWhenTokenDisableFails(t *testing.T) {
	truncate(t)

	ctx := newPolicyIncidentTestContext(t)
	err := types.NewOpenAIError(errors.New("cyber_policy API key 已永久禁用"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(12345, 1, "missing-channel", false, "upstream-key", true), err)

	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyPolicyIncidentDetected))
	assert.True(t, ShouldSkipRetryAfterPolicyIncident(ctx))
}

func TestPolicyIncidentRecordsEventAndIsolatesOnlyCurrentMultiKey(t *testing.T) {
	truncate(t)

	token := &model.Token{UserId: 42, Key: "client-token-key", Status: common.TokenStatusEnabled, Name: "client-token"}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{
		Key:    "upstream-a\nupstream-b",
		Status: common.ChannelStatusEnabled,
		Name:   "policy-channel",
		Type:   1,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, model.DB.Create(channel).Error)

	ctx := newPolicyIncidentTestContext(t)
	ctx.Set("token_id", token.Id)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions/upstream-b", nil)
	common.SetContextKey(ctx, constant.ContextKeyAdminRejectReason, "policy echoed bearer upstream-b and raw upstream-b")
	err := types.NewOpenAIError(errors.New("网络滥用封禁 cyber_policy Bearer upstream-b raw upstream-b"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, true, "upstream-b", true), err)

	var disabledToken model.Token
	require.NoError(t, model.DB.First(&disabledToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, disabledToken.Status)

	reloaded, errGet := model.GetChannelById(channel.Id, true)
	require.NoError(t, errGet)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[1])
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyDisabledReason[1], "upstream-b")

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)
	assert.Equal(t, token.Id, event.TokenId)
	assert.Equal(t, channel.Id, event.ChannelId)
	assert.Equal(t, model.FingerprintPolicyIncidentUpstreamKey("upstream-b"), event.UpstreamKeyFingerprint)
	assert.NotContains(t, event.ErrorMessage, "upstream-b")
	assert.NotContains(t, string(event.Metadata), "upstream-b")
	assert.NotContains(t, formatPolicyIncidentNotification(&event), "upstream-b")
	assert.Equal(t, "encountered", event.Causality)
	assert.Contains(t, event.ActionTaken, "upstream_isolated")
	assert.Equal(t, policyIncidentResultSuccess, event.ActionResult)
}
