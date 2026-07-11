package service

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type policyIncidentRootNotification struct {
	notifyType string
	subject    string
	content    string
}

func newPolicyIncidentTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("id", 42)
	ctx.Set("token_id", 77)
	ctx.Set("token_name", "client-token")
	ctx.Set("original_model", "gpt-policy")
	ctx.Set(common.RequestIdKey, "req-policy-test")
	t.Setenv(policyIncidentEvidenceDirEnv, filepath.Join(t.TempDir(), "policy-evidence"))
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Unix(1710000000, 123000000))
	common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 1)
	return ctx
}

func capturePolicyIncidentRootNotifications(t *testing.T) *[]policyIncidentRootNotification {
	t.Helper()
	notifications := make([]policyIncidentRootNotification, 0)
	original := notifyPolicyIncidentRoot
	notifyPolicyIncidentRoot = func(notifyType string, subject string, content string) {
		notifications = append(notifications, policyIncidentRootNotification{
			notifyType: notifyType,
			subject:    subject,
			content:    content,
		})
	}
	t.Cleanup(func() {
		notifyPolicyIncidentRoot = original
	})
	return &notifications
}

func setPolicyIncidentLockSequence(t *testing.T, results ...bool) {
	t.Helper()
	original := acquirePolicyIncidentLock
	index := 0
	acquirePolicyIncidentLock = func(channelId int, upstreamKey string) bool {
		if index >= len(results) {
			return results[len(results)-1]
		}
		result := results[index]
		index++
		return result
	}
	t.Cleanup(func() {
		acquirePolicyIncidentLock = original
	})
}

func createPolicyIncidentUser(t *testing.T, id int, role int) *model.User {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: "policy-user-" + strconv.Itoa(id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-policy-" + strconv.Itoa(id),
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func setPolicyIncidentUpstreamIsolation(t *testing.T, enabled bool) {
	t.Helper()
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.IsolateUpstreamOnPolicyIncident
	setting.IsolateUpstreamOnPolicyIncident = enabled
	t.Cleanup(func() {
		setting.IsolateUpstreamOnPolicyIncident = original
	})
}

func TestClassifyPolicyIncidentMatchesHighConfidenceText(t *testing.T) {
	tests := []string{
		"upstream rejected: cyber_policy",
		"上游返回：网络滥用封禁",
		"当前 API key 已永久禁用",
		"API key 已永久禁用",
		"api key has been deactivated due to policy",
		"account is suspended for policy reasons",
		"账号已停用",
	}

	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			err := types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

			classification := ClassifyPolicyIncident(err)

			assert.True(t, classification.Detected)
			assert.Equal(t, http.StatusForbidden, classification.StatusCode)
			assert.Equal(t, policyIncidentEvidenceHigh, classification.EvidenceLevel)
		})
	}
}

func TestClassifyPolicyIncidentSeparatesClientAndUpstreamKeyCausality(t *testing.T) {
	clientErr := types.NewOpenAIError(errors.New("upstream rejected: cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	clientClassification := ClassifyPolicyIncident(clientErr)
	assert.True(t, clientClassification.Detected)
	assert.True(t, clientClassification.ClientTokenActionAllowed)
	assert.Equal(t, policyIncidentCausalityClientPolicyRequest, clientClassification.Causality)

	mixedErr := types.NewOpenAIError(errors.New("网络滥用封禁：上游返回 cyber_policy，当前 API key 已永久禁用"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	mixedClassification := ClassifyPolicyIncident(mixedErr)
	assert.True(t, mixedClassification.Detected)
	assert.False(t, mixedClassification.ClientTokenActionAllowed)
	assert.Equal(t, policyIncidentCausalityAmbiguousMixedAttribution, mixedClassification.Causality)

	mixedCodeMessageErr := types.WithOpenAIError(types.OpenAIError{
		Code:    "cyber_policy",
		Message: "current API key has been permanently disabled",
	}, http.StatusForbidden)
	mixedCodeMessageClassification := ClassifyPolicyIncident(mixedCodeMessageErr)
	assert.True(t, mixedCodeMessageClassification.Detected)
	assert.False(t, mixedCodeMessageClassification.ClientTokenActionAllowed)
	assert.Equal(t, policyIncidentCausalityAmbiguousMixedAttribution, mixedCodeMessageClassification.Causality)

	upstreamKeyOnlyErr := types.NewOpenAIError(errors.New("api key has been deactivated by provider"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	upstreamKeyOnlyClassification := ClassifyPolicyIncident(upstreamKeyOnlyErr)
	assert.True(t, upstreamKeyOnlyClassification.Detected)
	assert.False(t, upstreamKeyOnlyClassification.ClientTokenActionAllowed)
	assert.Equal(t, policyIncidentCausalityUpstreamKey, upstreamKeyOnlyClassification.Causality)
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

func TestPolicyIncidentRecordsEventAndSkipsUpstreamIsolationByDefault(t *testing.T) {
	truncate(t)
	notifications := capturePolicyIncidentRootNotifications(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	originalPersistentDisable := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = true
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = originalPersistentDisable
	})

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
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

	var disabledUser model.User
	require.NoError(t, model.DB.First(&disabledUser, 42).Error)
	assert.Equal(t, common.UserStatusDisabled, disabledUser.Status)

	reloaded, errGet := model.GetChannelById(channel.Id, true)
	require.NoError(t, errGet)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyStatusList, 0)
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyStatusList, 1)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)
	assert.Equal(t, token.Id, event.TokenId)
	assert.Equal(t, channel.Id, event.ChannelId)
	assert.Equal(t, model.FingerprintPolicyIncidentUpstreamKey("upstream-b"), event.UpstreamKeyFingerprint)
	assert.NotContains(t, event.ErrorMessage, "upstream-b")
	assert.NotContains(t, string(event.Metadata), "upstream-b")
	assert.NotContains(t, formatPolicyIncidentNotification(&event), "upstream-b")
	assert.Equal(t, policyIncidentCausalityClientPolicyRequest, event.Causality)
	assert.Contains(t, event.ActionTaken, "token_breaker_set")
	assert.Contains(t, event.ActionTaken, "token_disabled")
	assert.Contains(t, event.ActionTaken, "user_disabled")
	assert.Contains(t, event.ActionTaken, "upstream_breaker_skipped")
	assert.Contains(t, event.ActionTaken, "upstream_isolation_skipped")
	assert.Contains(t, event.ActionTaken, "root_notify_attempted")
	assert.NotContains(t, event.ActionTaken, "upstream_breaker_set")
	assert.NotContains(t, event.ActionTaken, "upstream_isolated")
	assert.Contains(t, event.ActionResult, "redis_unavailable")
	assert.Contains(t, event.ActionResult, policyIncidentResultConfigDisabled)
	assert.Contains(t, event.ActionResult, policyIncidentResultSuccess)
	assert.Contains(t, event.ActionResult, "attempted")
	require.Len(t, *notifications, 1)
	assert.Equal(t, "[P0 风控] 检测到安全策略命中", (*notifications)[0].subject)
	assert.Contains(t, (*notifications)[0].content, "客户处置：已封禁命中的客户 token/user")
	assert.Contains(t, (*notifications)[0].content, "上游处置：配置关闭，未隔离上游 key")
	assert.NotContains(t, (*notifications)[0].content, "上游 key 因安全策略被禁用")
}

func TestPolicyIncidentNotificationUsesIncidentLockForDeduplication(t *testing.T) {
	truncate(t)
	setPolicyIncidentLockSequence(t, true, false)
	notifications := capturePolicyIncidentRootNotifications(t)

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
	channel := &model.Channel{Key: "upstream-key-dedupe", Status: common.ChannelStatusEnabled, Name: "policy-channel-dedupe", Type: 1}
	require.NoError(t, model.DB.Create(channel).Error)

	firstCtx := newPolicyIncidentTestContext(t)
	firstCtx.Set(common.RequestIdKey, "req-policy-dedupe-1")
	firstErr := types.NewOpenAIError(errors.New("cyber_policy request rejected by upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	HandlePolicyIncident(firstCtx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true), firstErr)

	secondCtx := newPolicyIncidentTestContext(t)
	secondCtx.Set(common.RequestIdKey, "req-policy-dedupe-2")
	secondErr := types.NewOpenAIError(errors.New("cyber_policy request rejected by upstream again"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	HandlePolicyIncident(secondCtx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true), secondErr)

	require.Len(t, *notifications, 1)

	var count int64
	require.NoError(t, model.DB.Model(&model.PolicyIncidentEvent{}).Where("request_id IN ?", []string{"req-policy-dedupe-1", "req-policy-dedupe-2"}).Count(&count).Error)
	assert.EqualValues(t, 2, count)

	var secondEvent model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-dedupe-2").First(&secondEvent).Error)
	assert.Contains(t, secondEvent.ActionTaken, "root_notify_skipped")
	assert.Contains(t, secondEvent.ActionResult, "deduplicated")
}

func TestPolicyIncidentCanIsolateUpstreamWhenExplicitlyEnabled(t *testing.T) {
	truncate(t)
	setPolicyIncidentUpstreamIsolation(t, true)

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
	token := &model.Token{UserId: 42, Key: "client-token-key-enabled", Status: common.TokenStatusEnabled, Name: "client-token"}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{
		Key:    "upstream-a\nupstream-b",
		Status: common.ChannelStatusEnabled,
		Name:   "policy-channel-enabled",
		Type:   1,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, model.DB.Create(channel).Error)

	ctx := newPolicyIncidentTestContext(t)
	ctx.Set("token_id", token.Id)
	ctx.Set(common.RequestIdKey, "req-policy-test-upstream-enabled")
	err := types.NewOpenAIError(errors.New("cyber_policy request rejected for upstream-b"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, true, "upstream-b", true), err)

	reloaded, errGet := model.GetChannelById(channel.Id, true)
	require.NoError(t, errGet)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[1])
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyDisabledReason[1], "upstream-b")

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test-upstream-enabled").First(&event).Error)
	assert.Contains(t, event.ActionTaken, "upstream_breaker_set")
	assert.Contains(t, event.ActionTaken, "upstream_isolated")
	assert.Contains(t, event.ActionResult, "redis_unavailable")
}

func TestPolicyIncidentWritesGzipEvidenceAndSafeMetadata(t *testing.T) {
	truncate(t)
	evidenceDir := filepath.Join(t.TempDir(), "policy-evidence")

	const upstreamKey = "upstream-raw-key"
	const clientToken = "sk-client-token-secret"
	rawBody := `{"model":"gpt-policy","messages":[{"role":"user","content":"investigate policy prompt with upstream-raw-key and sk-client-token-secret"}]}`
	ctx := newPolicyIncidentTestContext(t)
	t.Setenv(policyIncidentEvidenceDirEnv, evidenceDir)
	ctx.Set("token_key", "client-token-secret")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions/"+upstreamKey, strings.NewReader(rawBody))
	ctx.Request.RemoteAddr = "203.0.113.10:4444"
	ctx.Request.Header.Set("Authorization", "Bearer "+clientToken)
	ctx.Request.Header.Set("X-Forwarded-For", "198.51.100.2, 203.0.113.10")
	apiErr := types.NewOpenAIError(errors.New("cyber_policy denied for Bearer "+upstreamKey), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(88, 1, "policy-channel", true, upstreamKey, true), apiErr)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)

	var metadata map[string]any
	require.NoError(t, common.Unmarshal(event.Metadata, &metadata))
	caseID, ok := metadata["case_id"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(caseID, "policy-1710000000123-"))
	assert.Equal(t, testPolicyIncidentHexSHA256([]byte(rawBody)), metadata["evidence_body_sha256"])
	assert.NotContains(t, string(event.Metadata), "investigate policy prompt")

	evidencePath, ok := metadata["evidence_path"].(string)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(evidenceDir, caseID+".json.gz"), evidencePath)
	assert.Empty(t, metadata["evidence_error"])

	dirInfo, err := os.Stat(evidenceDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(policyIncidentEvidenceDirPerm), dirInfo.Mode().Perm())
	fileInfo, err := os.Stat(evidencePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(policyIncidentEvidenceFilePerm), fileInfo.Mode().Perm())

	gzipPayload, err := os.ReadFile(evidencePath)
	require.NoError(t, err)
	assert.Equal(t, testPolicyIncidentHexSHA256(gzipPayload), metadata["evidence_sha256"])

	var evidence policyIncidentEvidenceFile
	decodedEvidence, err := readPolicyIncidentEvidenceGzip(gzipPayload, &evidence)
	require.NoError(t, err)
	assert.Equal(t, caseID, evidence.CaseID)
	assert.Equal(t, "2024-03-09T16:00:00.123Z", evidence.RequestTime)
	assert.Equal(t, "req-policy-test", evidence.RequestID)
	assert.Equal(t, 42, evidence.UserID)
	assert.Equal(t, 77, evidence.TokenID)
	assert.Equal(t, "client-token", evidence.TokenName)
	assert.Equal(t, "gpt-policy", evidence.Model)
	assert.Equal(t, "/v1/chat/completions/"+model.PolicyIncidentMetadataRedacted, evidence.Path)
	assert.Equal(t, "203.0.113.10", evidence.RemoteIP)
	assert.Equal(t, "198.51.100.2, 203.0.113.10", evidence.XForwardedFor)
	assert.Equal(t, 88, evidence.ChannelID)
	assert.Equal(t, 1, evidence.MultiKeyIndex)
	assert.Equal(t, model.FingerprintPolicyIncidentUpstreamKey(upstreamKey), evidence.UpstreamKeyFingerprint)
	assert.Equal(t, http.StatusForbidden, evidence.Status)
	assert.Equal(t, string(types.ErrorCodeBadResponseStatusCode), evidence.Error.Code)
	assert.Equal(t, testPolicyIncidentHexSHA256([]byte(rawBody)), evidence.BodySHA256)
	assert.Contains(t, evidence.Body, "investigate policy prompt")
	assert.True(t, evidence.BodyRedacted)
	assert.NotContains(t, evidence.Body, upstreamKey)
	assert.NotContains(t, evidence.Body, clientToken)
	assert.NotContains(t, evidence.Error.Message, upstreamKey)
	assert.NotContains(t, string(decodedEvidence), upstreamKey)
	assert.NotContains(t, string(decodedEvidence), clientToken)
	assert.NotContains(t, string(decodedEvidence), "Authorization")
}

func TestPolicyIncidentEvidenceWriteFailureDoesNotBlockEvent(t *testing.T) {
	truncate(t)
	tempDir := t.TempDir()
	blockedPath := filepath.Join(tempDir, "not-a-directory")
	require.NoError(t, os.WriteFile(blockedPath, []byte("blocked"), 0600))

	rawBody := `{"messages":[{"role":"user","content":"safe prompt for evidence failure"}]}`
	ctx := newPolicyIncidentTestContext(t)
	t.Setenv(policyIncidentEvidenceDirEnv, blockedPath)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(rawBody))
	err := types.NewOpenAIError(errors.New("cyber_policy request rejected"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(89, 1, "policy-channel", false, "upstream-key", true), err)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)

	var metadata map[string]any
	require.NoError(t, common.Unmarshal(event.Metadata, &metadata))
	caseID, _ := metadata["case_id"].(string)
	assert.NotEmpty(t, caseID)
	assert.Equal(t, caseID, ctx.GetString(PolicyIncidentCaseIDContextKey))
	assert.Equal(t, testPolicyIncidentHexSHA256([]byte(rawBody)), metadata["evidence_body_sha256"])
	assert.NotEmpty(t, metadata["evidence_error"])
	assert.NotContains(t, string(event.Metadata), "safe prompt for evidence failure")
	_, hasPath := metadata["evidence_path"]
	assert.False(t, hasPath)
	_, hasEvidenceHash := metadata["evidence_sha256"]
	assert.False(t, hasEvidenceHash)
}

func TestPolicyIncidentRespectsPersistentDisableConfig(t *testing.T) {
	truncate(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = false
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = original
	})

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
	token := &model.Token{UserId: 42, Key: "client-token-key-configured", Status: common.TokenStatusEnabled, Name: "client-token"}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{Key: "upstream-key", Status: common.ChannelStatusEnabled, Name: "policy-channel", Type: 1}
	require.NoError(t, model.DB.Create(channel).Error)

	ctx := newPolicyIncidentTestContext(t)
	ctx.Set("token_id", token.Id)
	err := types.NewOpenAIError(errors.New("cyber_policy request rejected by upstream policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, "upstream-key", true), err)

	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloaded.Status)

	var user model.User
	require.NoError(t, model.DB.First(&user, 42).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)
	assert.Contains(t, event.ActionTaken, "token_breaker_set")
	assert.Contains(t, event.ActionTaken, "token_db_disable_skipped")
	assert.Contains(t, event.ActionTaken, "user_db_disable_skipped")
	assert.Contains(t, event.ActionResult, policyIncidentResultConfigDisabled)
	assert.NotContains(t, event.ActionTaken, "token_disabled")
	assert.NotContains(t, event.ActionTaken, "user_disabled")
}

func TestPolicyIncidentTreatsMixedCyberPolicyAndKeyDisabledAsAmbiguous(t *testing.T) {
	truncate(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = true
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = original
	})

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
	token := &model.Token{UserId: 42, Key: "client-token-key-mixed", Status: common.TokenStatusEnabled, Name: "client-token"}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{Key: "upstream-key", Status: common.ChannelStatusEnabled, Name: "policy-channel", Type: 1}
	require.NoError(t, model.DB.Create(channel).Error)

	ctx := newPolicyIncidentTestContext(t)
	ctx.Set("token_id", token.Id)
	err := types.NewOpenAIError(errors.New("网络滥用封禁：上游返回 cyber_policy，当前 API key 已永久禁用"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, "upstream-key", true), err)

	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloaded.Status)

	var user model.User
	require.NoError(t, model.DB.First(&user, 42).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test").First(&event).Error)
	assert.Equal(t, policyIncidentCausalityAmbiguousMixedAttribution, event.Causality)
	assert.Contains(t, event.ActionTaken, "token_breaker_skipped")
	assert.Contains(t, event.ActionTaken, "token_db_disable_skipped")
	assert.Contains(t, event.ActionTaken, "user_db_disable_skipped")
	assert.Contains(t, event.ActionTaken, "upstream_breaker_skipped")
	assert.Contains(t, event.ActionTaken, "upstream_isolation_skipped")
	assert.Contains(t, event.ActionResult, "ambiguous_attribution")
	assert.NotContains(t, event.ActionTaken, "token_breaker_set")
	assert.NotContains(t, event.ActionTaken, "token_disabled")
	assert.NotContains(t, event.ActionTaken, "user_disabled")
	assert.NotContains(t, event.ActionTaken, "upstream_breaker_set")
	assert.NotContains(t, event.ActionTaken, "upstream_isolated")
	assert.True(t, ShouldSkipRetryAfterPolicyIncident(ctx))
}

func TestTaskRelayPolicyIncidentTreatsSplitMixedMarkersAsAmbiguous(t *testing.T) {
	truncate(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = true
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = original
	})

	createPolicyIncidentUser(t, 42, common.RoleCommonUser)
	token := &model.Token{UserId: 42, Key: "client-token-key-task-mixed", Status: common.TokenStatusEnabled, Name: "client-token"}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{Key: "upstream-key", Status: common.ChannelStatusEnabled, Name: "policy-channel", Type: 1}
	require.NoError(t, model.DB.Create(channel).Error)

	ctx := newPolicyIncidentTestContext(t)
	ctx.Set("token_id", token.Id)
	ctx.Set(common.RequestIdKey, "req-policy-task-mixed")
	taskErr := &dto.TaskError{
		Code:       "cyber_policy",
		Message:    "task failed",
		Error:      errors.New("current API key has been permanently disabled"),
		StatusCode: http.StatusForbidden,
	}

	HandleTaskRelayPolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true), taskErr)

	var reloadedToken model.Token
	require.NoError(t, model.DB.First(&reloadedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloadedToken.Status)

	var user model.User
	require.NoError(t, model.DB.First(&user, 42).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)

	var event model.PolicyIncidentEvent
	require.NoError(t, model.DB.Where("request_id = ?", "req-policy-task-mixed").First(&event).Error)
	assert.Equal(t, policyIncidentCausalityAmbiguousMixedAttribution, event.Causality)
	assert.Contains(t, event.ActionTaken, "token_breaker_skipped")
	assert.NotContains(t, event.ActionTaken, "token_breaker_set")
	assert.True(t, ShouldSkipRetryAfterPolicyIncident(ctx))
}

func TestPolicyIncidentSkipsClientTokenActionsForUpstreamOnlyVariants(t *testing.T) {
	truncate(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = true
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = original
	})

	tests := []struct {
		name    string
		message string
	}{
		{name: "disabled_key", message: "API key is disabled by provider policy"},
		{name: "invalid_key", message: "invalid api key"},
		{name: "deactivated_account", message: "account has been deactivated"},
		{name: "suspended_account", message: "账号已暂停"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := 420 + i
			createPolicyIncidentUser(t, userId, common.RoleCommonUser)
			token := &model.Token{UserId: userId, Key: "client-token-key-" + tt.name, Status: common.TokenStatusEnabled, Name: "client-token"}
			require.NoError(t, model.DB.Create(token).Error)
			channel := &model.Channel{Key: "upstream-key-" + tt.name, Status: common.ChannelStatusEnabled, Name: "policy-channel", Type: 1}
			require.NoError(t, model.DB.Create(channel).Error)

			ctx := newPolicyIncidentTestContext(t)
			ctx.Set("id", userId)
			ctx.Set("token_id", token.Id)
			ctx.Set(common.RequestIdKey, "req-policy-test-"+tt.name)
			err := types.NewOpenAIError(errors.New(tt.message), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

			HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true), err)

			var reloaded model.Token
			require.NoError(t, model.DB.First(&reloaded, token.Id).Error)
			assert.Equal(t, common.TokenStatusEnabled, reloaded.Status)

			var user model.User
			require.NoError(t, model.DB.First(&user, userId).Error)
			assert.Equal(t, common.UserStatusEnabled, user.Status)

			var event model.PolicyIncidentEvent
			require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test-"+tt.name).First(&event).Error)
			assert.Equal(t, policyIncidentCausalityUpstreamKey, event.Causality)
			assert.Contains(t, event.ActionTaken, "token_breaker_skipped")
			assert.Contains(t, event.ActionTaken, "token_db_disable_skipped")
			assert.Contains(t, event.ActionTaken, "user_db_disable_skipped")
			assert.NotContains(t, event.ActionTaken, "token_breaker_set")
			assert.NotContains(t, event.ActionTaken, "token_disabled")
			assert.NotContains(t, event.ActionTaken, "user_disabled")
			assert.Contains(t, event.ActionResult, "upstream_key_attribution")
		})
	}
}

func TestPolicyIncidentSkipsPrivilegedUserDisableButDisablesToken(t *testing.T) {
	truncate(t)
	setting := operation_setting.GetPolicyIncidentSetting()
	original := setting.DisableClientTokenPersistently
	setting.DisableClientTokenPersistently = true
	t.Cleanup(func() {
		setting.DisableClientTokenPersistently = original
	})

	tests := []struct {
		name string
		role int
	}{
		{name: "admin", role: common.RoleAdminUser},
		{name: "root", role: common.RoleRootUser},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := 520 + i
			createPolicyIncidentUser(t, userId, tt.role)
			token := &model.Token{UserId: userId, Key: "client-token-key-" + tt.name, Status: common.TokenStatusEnabled, Name: "client-token"}
			require.NoError(t, model.DB.Create(token).Error)
			channel := &model.Channel{Key: "upstream-key-" + tt.name, Status: common.ChannelStatusEnabled, Name: "policy-channel", Type: 1}
			require.NoError(t, model.DB.Create(channel).Error)

			ctx := newPolicyIncidentTestContext(t)
			ctx.Set("id", userId)
			ctx.Set("token_id", token.Id)
			ctx.Set(common.RequestIdKey, "req-policy-test-privileged-"+tt.name)
			err := types.NewOpenAIError(errors.New("cyber_policy request rejected by upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

			HandlePolicyIncident(ctx, *types.NewChannelError(channel.Id, channel.Type, channel.Name, false, channel.Key, true), err)

			var reloadedToken model.Token
			require.NoError(t, model.DB.First(&reloadedToken, token.Id).Error)
			assert.Equal(t, common.TokenStatusDisabled, reloadedToken.Status)

			var user model.User
			require.NoError(t, model.DB.First(&user, userId).Error)
			assert.Equal(t, common.UserStatusEnabled, user.Status)

			var event model.PolicyIncidentEvent
			require.NoError(t, model.DB.Where("request_id = ?", "req-policy-test-privileged-"+tt.name).First(&event).Error)
			assert.Equal(t, policyIncidentCausalityClientPolicyRequest, event.Causality)
			assert.Contains(t, event.ActionTaken, "token_disabled")
			assert.Contains(t, event.ActionTaken, "user_disable_skipped_privileged")
			assert.Contains(t, event.ActionTaken, "upstream_breaker_skipped")
			assert.Contains(t, event.ActionTaken, "upstream_isolation_skipped")
			assert.NotContains(t, event.ActionTaken, "upstream_breaker_set")
			assert.NotContains(t, event.ActionTaken, "upstream_isolated")
		})
	}
}

func TestPolicyUpstreamBreakerNoopsWhenUpstreamIsolationDisabled(t *testing.T) {
	setPolicyIncidentUpstreamIsolation(t, false)
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})

	require.NoError(t, SetPolicyUpstreamKeyBreaker(123, "upstream-key"))
	assert.False(t, IsUpstreamKeyPolicyBreakerOpen(123, "upstream-key"))
}

func TestTaskRelayPolicyIncidentSkipsLocalErrors(t *testing.T) {
	truncate(t)

	ctx := newPolicyIncidentTestContext(t)
	taskErr := &dto.TaskError{
		Code:       "policy_breaker_open",
		Message:    "cyber_policy API key 已永久禁用",
		StatusCode: http.StatusServiceUnavailable,
		Error:      errors.New("cyber_policy API key 已永久禁用"),
		LocalError: true,
	}

	HandleTaskRelayPolicyIncident(ctx, *types.NewChannelError(12345, 1, "missing-channel", false, "upstream-key", true), taskErr)

	assert.False(t, ShouldSkipRetryAfterPolicyIncident(ctx))
	var count int64
	require.NoError(t, model.DB.Model(&model.PolicyIncidentEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func testPolicyIncidentHexSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func readPolicyIncidentEvidenceGzip(payload []byte, v any) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return decoded, common.Unmarshal(decoded, v)
}
