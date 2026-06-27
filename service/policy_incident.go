package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	policyIncidentEvidenceHigh                 = "high"
	policyIncidentCausalityClientPolicyRequest = "client_policy_request"
	policyIncidentCausalityUpstreamKey         = "upstream_key_encountered"
	PolicyIncidentCaseIDContextKey             = "policy_incident_case_id"

	policyIncidentResultSuccess        = "success"
	policyIncidentResultPartial        = "partial"
	policyIncidentResultConfigDisabled = "config_disabled"
)

var policyIncidentClientPolicyKeywords = []string{
	"cyber_policy",
	"网络滥用封禁",
}

var policyIncidentUpstreamKeyKeywords = []string{
	"当前 api key 禁用",
	"当前 api key 已永久禁用",
	"当前 api key 已禁用",
	"当前 api key 被禁用",
	"当前 api key 停用",
	"当前 api key 已停用",
	"当前 api key 被停用",
	"当前 api key 暂停",
	"当前 api key 已暂停",
	"api key 已永久禁用",
	"api key 禁用",
	"api key 已禁用",
	"api key 被禁用",
	"api key 停用",
	"api key 已停用",
	"api key 被停用",
	"api key 暂停",
	"api key 已暂停",
	"api key has been permanently disabled",
	"api key is permanently disabled",
	"api key has been disabled",
	"api key is disabled",
	"api key has been deactivated",
	"api key is deactivated",
	"api key has been suspended",
	"api key is suspended",
	"api key has been banned",
	"api key is banned",
	"key has been permanently disabled",
	"key is permanently disabled",
	"key has been disabled",
	"key is disabled",
	"key has been deactivated",
	"key is deactivated",
	"key has been suspended",
	"key is suspended",
	"key has been banned",
	"key is banned",
	"account has been disabled",
	"account is disabled",
	"account has been deactivated",
	"account is deactivated",
	"account has been suspended",
	"account is suspended",
	"account has been banned",
	"account is banned",
	"invalid api key",
	"api key is invalid",
	"api key has become invalid",
	"invalid key",
	"key is invalid",
	"invalid account",
	"account is invalid",
	"账户已禁用",
	"账号已禁用",
	"账户被禁用",
	"账号被禁用",
	"账户已停用",
	"账号已停用",
	"账户被停用",
	"账号被停用",
	"账户已暂停",
	"账号已暂停",
	"账户被暂停",
	"账号被暂停",
	"账户封禁",
	"账号封禁",
	"api key 无效",
	"key 无效",
	"账户无效",
	"账号无效",
}

var (
	acquirePolicyIncidentLock = acquirePolicyIncidentLockWithRedis
	notifyPolicyIncidentRoot  = NotifyRootUser
)

type PolicyIncidentClassification struct {
	Detected                 bool
	StatusCode               int
	ErrorCode                string
	ErrorMessage             string
	EvidenceLevel            string
	Causality                string
	ClientTokenActionAllowed bool
}

func ClassifyPolicyIncident(err *types.NewAPIError) PolicyIncidentClassification {
	if err == nil {
		return PolicyIncidentClassification{}
	}
	return classifyPolicyIncidentText(err.StatusCode, string(err.GetErrorCode()), err.Error())
}

func classifyPolicyIncidentText(statusCode int, errorCode string, message string) PolicyIncidentClassification {
	message = common.MaskSensitiveInfo(message)
	text := strings.ToLower(strings.TrimSpace(errorCode + " " + message))
	clientPolicyDetected := containsPolicyIncidentKeyword(text, policyIncidentClientPolicyKeywords)
	upstreamKeyDetected := containsPolicyIncidentKeyword(text, policyIncidentUpstreamKeyKeywords)
	if !clientPolicyDetected && !upstreamKeyDetected {
		return PolicyIncidentClassification{StatusCode: statusCode, ErrorCode: errorCode, ErrorMessage: message}
	}

	classification := PolicyIncidentClassification{
		Detected:      true,
		StatusCode:    statusCode,
		ErrorCode:     errorCode,
		ErrorMessage:  message,
		EvidenceLevel: policyIncidentEvidenceHigh,
		Causality:     policyIncidentCausalityUpstreamKey,
	}
	if clientPolicyDetected {
		classification.Causality = policyIncidentCausalityClientPolicyRequest
		classification.ClientTokenActionAllowed = true
		return classification
	}
	if upstreamKeyDetected && !clientPolicyDetected {
		classification.Causality = policyIncidentCausalityUpstreamKey
	}
	return classification
}

func containsPolicyIncidentKeyword(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func MarkPolicyIncidentNoRetry(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyPolicyIncidentDetected, true)
	common.SetContextKey(c, constant.ContextKeyPolicyNoRetry, true)
}

func ShouldSkipRetryAfterPolicyIncident(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.GetContextKeyBool(c, constant.ContextKeyPolicyNoRetry)
}

func HandlePolicyIncident(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	handlePolicyIncident(c, channelError, ClassifyPolicyIncident(err))
}

func HandleTaskRelayPolicyIncident(c *gin.Context, channelError types.ChannelError, taskErr *dto.TaskError) {
	if taskErr == nil {
		return
	}
	if taskErr.LocalError {
		return
	}
	classification := classifyPolicyIncidentText(taskErr.StatusCode, taskErr.Code, taskErr.Message)
	if !classification.Detected && taskErr.Error != nil {
		classification = classifyPolicyIncidentText(taskErr.StatusCode, taskErr.Code, taskErr.Error.Error())
	}
	handlePolicyIncident(c, channelError, classification)
}

func handlePolicyIncident(c *gin.Context, channelError types.ChannelError, classification PolicyIncidentClassification) {
	if c == nil || !classification.Detected {
		return
	}

	MarkPolicyIncidentNoRetry(c)

	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	upstreamBreakerAction, upstreamBreakerResult := setPolicyUpstreamKeyBreakerAction(channelError.ChannelId, channelError.UsingKey)
	actions := []string{upstreamBreakerAction}
	results := []string{upstreamBreakerResult}

	if classification.ClientTokenActionAllowed {
		tokenBreakerAction, tokenBreakerResult := setPolicyTokenBreakerAction(tokenId)
		actions = append(actions, tokenBreakerAction)
		results = append(results, tokenBreakerResult)
		tokenAction, tokenResult := disablePolicyToken(tokenId, userId, true)
		actions = append(actions, tokenAction)
		results = append(results, tokenResult)
		userAction, userResult := disablePolicyUser(userId)
		actions = append(actions, userAction)
		results = append(results, userResult)
	} else {
		actions = append(actions, "token_unchanged", "user_unchanged")
		results = append(results, "upstream_key_attribution", "upstream_key_attribution")
	}

	incidentLockAcquired := acquirePolicyIncidentLock(channelError.ChannelId, channelError.UsingKey)
	upstreamAction, upstreamResult := isolatePolicyUpstreamAction(channelError, classification, incidentLockAcquired)
	actions = append(actions, upstreamAction)
	results = append(results, upstreamResult)
	notifyAction, notifyResult := rootPolicyIncidentNotifyAction(incidentLockAcquired)
	actions = append(actions, notifyAction)
	results = append(results, notifyResult)

	event := buildPolicyIncidentEvent(c, channelError, classification, strings.Join(actions, ","), strings.Join(results, ","))
	if err := model.InsertPolicyIncidentEvent(event); err != nil {
		common.SysLog("failed to record policy incident event: " + err.Error())
	}
	if incidentLockAcquired {
		notifyPolicyIncidentRoot(formatPolicyIncidentNotifyType(event), "[P0 风控] 检测到安全策略命中", formatPolicyIncidentNotification(event))
	}
}

func setPolicyTokenBreakerAction(tokenId int) (string, string) {
	if tokenId <= 0 {
		return "token_breaker_set", "token_context_missing"
	}
	if err := SetPolicyTokenBreaker(tokenId); err != nil {
		return "token_breaker_set", policyBreakerSetFailureResult(err)
	}
	return "token_breaker_set", policyIncidentResultSuccess
}

func setPolicyUpstreamKeyBreakerAction(channelId int, upstreamKey string) (string, string) {
	if !operation_setting.GetPolicyIncidentSetting().IsolateUpstreamOnPolicyIncident {
		return "upstream_breaker_skipped", policyIncidentResultConfigDisabled
	}
	return "upstream_breaker_set", setPolicyUpstreamKeyBreakerResult(channelId, upstreamKey)
}

func setPolicyUpstreamKeyBreakerResult(channelId int, upstreamKey string) string {
	if channelId <= 0 || strings.TrimSpace(upstreamKey) == "" {
		return "upstream_context_missing"
	}
	if err := SetPolicyUpstreamKeyBreaker(channelId, upstreamKey); err != nil {
		return policyBreakerSetFailureResult(err)
	}
	return policyIncidentResultSuccess
}

func isolatePolicyUpstreamAction(channelError types.ChannelError, classification PolicyIncidentClassification, incidentLockAcquired bool) (string, string) {
	if !operation_setting.GetPolicyIncidentSetting().IsolateUpstreamOnPolicyIncident {
		return "upstream_isolation_skipped", policyIncidentResultConfigDisabled
	}
	if !incidentLockAcquired {
		return "incident_lock_skipped", "deduplicated"
	}
	action, result := isolatePolicyUpstream(channelError, classification)
	return action, result
}

func rootPolicyIncidentNotifyAction(incidentLockAcquired bool) (string, string) {
	if !incidentLockAcquired {
		return "root_notify_skipped", "deduplicated"
	}
	return "root_notify_attempted", "attempted"
}

func policyBreakerSetFailureResult(err error) string {
	if errors.Is(err, errPolicyBreakerRedisUnavailable) {
		return "redis_unavailable"
	}
	return "redis_error"
}

func disablePolicyToken(tokenId int, userId int, force bool) (string, string) {
	if !force && !operation_setting.GetPolicyIncidentSetting().DisableClientTokenPersistently {
		return "token_unchanged", "config_disabled"
	}
	if tokenId <= 0 || userId <= 0 {
		return "token_unchanged", "token_context_missing"
	}
	_, changed, err := model.DisableTokenByIds(tokenId, userId)
	if err != nil {
		common.SysLog("failed to disable policy incident token: " + err.Error())
		return "token_unchanged", "db_error"
	}
	if changed {
		return "token_disabled", policyIncidentResultSuccess
	}
	return "token_unchanged", "already_disabled"
}

func disablePolicyUser(userId int) (string, string) {
	if userId <= 0 {
		return "user_unchanged", "user_context_missing"
	}
	_, changed, err := model.DisableUserForPolicyIncident(userId)
	if errors.Is(err, model.ErrPolicyIncidentPrivilegedUser) {
		return "user_disable_skipped_privileged", "privileged_user"
	}
	if err != nil {
		common.SysLog("failed to disable policy incident user: " + err.Error())
		return "user_unchanged", "db_error"
	}
	if changed {
		return "user_disabled", policyIncidentResultSuccess
	}
	return "user_unchanged", "already_disabled"
}

func isolatePolicyUpstream(channelError types.ChannelError, classification PolicyIncidentClassification) (string, string) {
	if channelError.IsMultiKey && strings.TrimSpace(channelError.UsingKey) == "" {
		return "upstream_isolation_failed", "missing_using_key"
	}
	reason := fmt.Sprintf("P0 cyber policy incident: status_code=%d error_code=%s message=%s",
		classification.StatusCode, classification.ErrorCode, redactPolicyIncidentMessage(classification.ErrorMessage, channelError.UsingKey))
	if model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason) {
		return "upstream_isolated", policyIncidentResultSuccess
	}
	if channelError.IsMultiKey {
		return "upstream_isolation_failed", "using_key_not_found_or_already_disabled"
	}
	return "upstream_isolation_failed", "channel_not_found_or_already_disabled"
}

func buildPolicyIncidentEvent(c *gin.Context, channelError types.ChannelError, classification PolicyIncidentClassification, actionTaken string, actionResult string) *model.PolicyIncidentEvent {
	errorMessage := redactPolicyIncidentMessage(classification.ErrorMessage, channelError.UsingKey)
	evidence := recordPolicyIncidentEvidence(c, channelError, classification)
	metadata := map[string]any{
		"client_token_action_allowed": classification.ClientTokenActionAllowed,
		"note":                        policyIncidentAttributionNote(classification),
		"use_channel":                 c.GetStringSlice("use_channel"),
	}
	for key, value := range evidence.Metadata() {
		metadata[key] = value
	}
	if c.Request != nil && c.Request.URL != nil {
		metadata["request_path"] = redactPolicyIncidentMessage(c.Request.URL.Path, channelError.UsingKey)
	}
	if channelError.IsMultiKey {
		metadata["is_multi_key"] = true
		metadata["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	if adminRejectReason := common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason); adminRejectReason != "" {
		metadata["admin_reject_reason"] = redactPolicyIncidentMessage(adminRejectReason, channelError.UsingKey)
	}

	event := &model.PolicyIncidentEvent{
		RequestId:              c.GetString(common.RequestIdKey),
		UserId:                 c.GetInt("id"),
		TokenId:                c.GetInt("token_id"),
		TokenName:              c.GetString("token_name"),
		ModelName:              c.GetString("original_model"),
		ChannelId:              channelError.ChannelId,
		ChannelType:            channelError.ChannelType,
		UpstreamKeyFingerprint: model.FingerprintPolicyIncidentUpstreamKey(channelError.UsingKey),
		StatusCode:             classification.StatusCode,
		ErrorCode:              classification.ErrorCode,
		ErrorMessage:           errorMessage,
		EvidenceLevel:          classification.EvidenceLevel,
		Causality:              classification.Causality,
		ActionTaken:            actionTaken,
		ActionResult:           actionResult,
	}
	if err := event.SetMetadata(metadata); err != nil {
		common.SysLog("failed to set policy incident metadata: " + err.Error())
	}
	return event
}

func policyIncidentAttributionNote(classification PolicyIncidentClassification) string {
	if classification.ClientTokenActionAllowed {
		return "该上游策略事件包含 cyber_policy 客户请求策略命中特征，即使同时出现上游 key/account 禁用文本，也按客户请求策略命中处置；仍建议结合请求证据复核。"
	}
	if operation_setting.GetPolicyIncidentSetting().IsolateUpstreamOnPolicyIncident {
		return "该事件只证明当前上游 key/account 遇到禁用、无效或暂停状态，不等于当前客户是源头；已隔离上游并跳过客户 token/user 封禁。"
	}
	return "该事件只证明当前上游 key/account 遇到禁用、无效或暂停状态，不等于当前客户是源头；当前配置已跳过上游隔离和客户 token/user 封禁。"
}

func redactPolicyIncidentMessage(message string, upstreamKey string) string {
	message = redactPolicyIncidentKeyVariants(message, upstreamKey)
	message = common.MaskSensitiveInfo(message)
	message = redactPolicyIncidentKeyVariants(message, upstreamKey)
	return message
}

func redactPolicyIncidentKeyVariants(message string, upstreamKey string) string {
	upstreamKey = strings.TrimSpace(upstreamKey)
	if upstreamKey == "" {
		return message
	}
	for _, variant := range policyIncidentKeyRedactionVariants(upstreamKey) {
		message = strings.ReplaceAll(message, variant, model.PolicyIncidentMetadataRedacted)
	}
	return message
}

func policyIncidentKeyRedactionVariants(upstreamKey string) []string {
	variants := make([]string, 0, 4)
	addVariant := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range variants {
			if existing == value {
				return
			}
		}
		variants = append(variants, value)
	}

	addVariant(upstreamKey)
	fields := strings.Fields(upstreamKey)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		addVariant(fields[1])
		addVariant("Bearer " + fields[1])
		addVariant("bearer " + fields[1])
	} else {
		addVariant("Bearer " + upstreamKey)
		addVariant("bearer " + upstreamKey)
	}
	return variants
}

func summarizePolicyActionResult(results []string) string {
	for _, result := range results {
		switch result {
		case "", policyIncidentResultSuccess, "already_disabled", "deduplicated":
			continue
		default:
			return policyIncidentResultPartial
		}
	}
	return policyIncidentResultSuccess
}

func formatPolicyIncidentNotifyType(event *model.PolicyIncidentEvent) string {
	key := strings.TrimPrefix(event.UpstreamKeyFingerprint, "sha256:")
	if len(key) > 12 {
		key = key[:12]
	}
	return fmt.Sprintf("%s_policy_%d_%s", dto.NotifyTypeChannelUpdate, event.ChannelId, key)
}

func formatPolicyIncidentNotification(event *model.PolicyIncidentEvent) string {
	createdAt := time.Unix(event.CreatedAt, 0).Format(time.RFC3339)
	return fmt.Sprintf(`时间：%s
request_id：%s
user_id：%d
token_id：%d
token_name：%s
channel_id：%d
channel_type：%d
key 指纹：%s
模型：%s
错误摘要：status=%d code=%s message=%s
已执行动作：%s
动作结果：%s
客户处置：%s
上游处置：%s
归因置信度：%s / %s

该用户是关联请求，不等于已确认源头。请结合请求证据、动作结果和归因置信度复核。`,
		createdAt,
		event.RequestId,
		event.UserId,
		event.TokenId,
		event.TokenName,
		event.ChannelId,
		event.ChannelType,
		event.UpstreamKeyFingerprint,
		event.ModelName,
		event.StatusCode,
		event.ErrorCode,
		event.ErrorMessage,
		event.ActionTaken,
		event.ActionResult,
		summarizePolicyIncidentClientDisposition(event.ActionTaken),
		summarizePolicyIncidentUpstreamDisposition(event.ActionTaken, event.ActionResult),
		event.EvidenceLevel,
		event.Causality,
	)
}

func summarizePolicyIncidentClientDisposition(actionTaken string) string {
	if strings.Contains(actionTaken, "token_disabled") || strings.Contains(actionTaken, "user_disabled") {
		return "已封禁命中的客户 token/user，详见已执行动作"
	}
	if strings.Contains(actionTaken, "token_unchanged") && strings.Contains(actionTaken, "user_unchanged") {
		return "未封禁客户 token/user"
	}
	return "见已执行动作"
}

func summarizePolicyIncidentUpstreamDisposition(actionTaken string, actionResult string) string {
	if strings.Contains(actionTaken, "upstream_isolated") {
		return "已隔离上游 key"
	}
	if strings.Contains(actionTaken, "incident_lock_skipped") {
		return "重复事件，已按去重策略跳过上游隔离"
	}
	if strings.Contains(actionTaken, "upstream_isolation_skipped") && strings.Contains(actionResult, policyIncidentResultConfigDisabled) {
		return "配置关闭，未隔离上游 key"
	}
	return "见已执行动作"
}

var errPolicyBreakerRedisUnavailable = errors.New("policy breaker redis unavailable")

func SetPolicyUpstreamKeyBreaker(channelId int, upstreamKey string) error {
	if channelId <= 0 || strings.TrimSpace(upstreamKey) == "" {
		return nil
	}
	if !operation_setting.GetPolicyIncidentSetting().IsolateUpstreamOnPolicyIncident {
		return nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return errPolicyBreakerRedisUnavailable
	}
	return common.RedisSet(policyUpstreamKeyBreakerKey(channelId, upstreamKey), "1", 24*time.Hour)
}

func IsUpstreamKeyPolicyBreakerOpen(channelId int, upstreamKey string) bool {
	if channelId <= 0 || strings.TrimSpace(upstreamKey) == "" || !common.RedisEnabled || common.RDB == nil {
		return false
	}
	if !operation_setting.GetPolicyIncidentSetting().IsolateUpstreamOnPolicyIncident {
		return false
	}
	_, err := common.RedisGet(policyUpstreamKeyBreakerKey(channelId, upstreamKey))
	return err == nil
}

func SetPolicyTokenBreaker(tokenId int) error {
	if tokenId <= 0 {
		return nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return errPolicyBreakerRedisUnavailable
	}
	return common.RedisSet(fmt.Sprintf("risk:cyber:token:%d", tokenId), "1", 30*time.Minute)
}

func IsPolicyTokenBreakerOpen(tokenId int) bool {
	if tokenId <= 0 || !common.RedisEnabled || common.RDB == nil {
		return false
	}
	_, err := common.RedisGet(fmt.Sprintf("risk:cyber:token:%d", tokenId))
	return err == nil
}

func acquirePolicyIncidentLockWithRedis(channelId int, upstreamKey string) bool {
	if channelId <= 0 || !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ok, err := common.RDB.SetNX(context.Background(), policyIncidentLockKey(channelId, upstreamKey), "1", time.Minute).Result()
	if err != nil {
		common.SysLog("failed to acquire policy incident lock: " + err.Error())
		return true
	}
	return ok
}

func policyUpstreamKeyBreakerKey(channelId int, upstreamKey string) string {
	return fmt.Sprintf("risk:cyber:upstream_key:%d:%s", channelId, policyKeyHash(upstreamKey))
}

func policyIncidentLockKey(channelId int, upstreamKey string) string {
	return fmt.Sprintf("risk:cyber:incident_lock:%d:%s", channelId, policyKeyHash(upstreamKey))
}

func policyKeyHash(upstreamKey string) string {
	fingerprint := model.FingerprintPolicyIncidentUpstreamKey(upstreamKey)
	if fingerprint == "" {
		return "unknown"
	}
	return strings.TrimPrefix(fingerprint, "sha256:")
}

func PolicyBreakerError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("upstream key is temporarily isolated by cyber policy breaker"),
		types.ErrorCodePolicyUpstreamKeyIsolated,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}
