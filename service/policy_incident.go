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
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	policyIncidentEvidenceHigh = "high"
	policyIncidentCausality    = "encountered"

	policyIncidentResultSuccess = "success"
	policyIncidentResultPartial = "partial"
)

var policyIncidentKeywords = []string{
	"cyber_policy",
	"网络滥用封禁",
	"当前 api key 已永久禁用",
	"api key 已永久禁用",
}

type PolicyIncidentClassification struct {
	Detected      bool
	StatusCode    int
	ErrorCode     string
	ErrorMessage  string
	EvidenceLevel string
	Causality     string
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
	for _, keyword := range policyIncidentKeywords {
		if strings.Contains(text, keyword) {
			return PolicyIncidentClassification{
				Detected:      true,
				StatusCode:    statusCode,
				ErrorCode:     errorCode,
				ErrorMessage:  message,
				EvidenceLevel: policyIncidentEvidenceHigh,
				Causality:     policyIncidentCausality,
			}
		}
	}
	return PolicyIncidentClassification{StatusCode: statusCode, ErrorCode: errorCode, ErrorMessage: message}
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
	_ = SetPolicyTokenBreaker(tokenId)
	_ = SetPolicyUpstreamKeyBreaker(channelError.ChannelId, channelError.UsingKey)

	lockAcquired := acquirePolicyIncidentLock(channelError.ChannelId, channelError.UsingKey)
	actions := []string{"breaker_set"}
	results := []string{policyIncidentResultSuccess}

	if lockAcquired {
		tokenAction, tokenResult := disablePolicyToken(tokenId, userId)
		upstreamAction, upstreamResult := isolatePolicyUpstream(channelError, classification)
		actions = append(actions, tokenAction, upstreamAction)
		results = append(results, tokenResult, upstreamResult)
	} else {
		actions = append(actions, "incident_lock_skipped")
		results = append(results, "deduplicated")
	}

	event := buildPolicyIncidentEvent(c, channelError, classification, strings.Join(actions, ","), summarizePolicyActionResult(results))
	if err := model.InsertPolicyIncidentEvent(event); err != nil {
		common.SysLog("failed to record policy incident event: " + err.Error())
	}
	if lockAcquired {
		NotifyRootUser(formatPolicyIncidentNotifyType(event), "[P0 风控] 上游 key 因安全策略被禁用", formatPolicyIncidentNotification(event))
	}
}

func disablePolicyToken(tokenId int, userId int) (string, string) {
	if tokenId <= 0 || userId <= 0 {
		return "token_disable_failed", "token_context_missing"
	}
	_, changed, err := model.DisableTokenByIds(tokenId, userId)
	if err != nil {
		return "token_disable_failed", err.Error()
	}
	if changed {
		return "token_disabled", policyIncidentResultSuccess
	}
	return "token_unchanged", "already_disabled"
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
	metadata := map[string]any{
		"note":        "该用户是关联请求，不等于已确认源头",
		"use_channel": c.GetStringSlice("use_channel"),
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
归因置信度：%s / %s

该用户是关联请求，不等于已确认源头。当前处置为风险隔离，不代表归因确认；建议人工复核。`,
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
		event.EvidenceLevel,
		event.Causality,
	)
}

func SetPolicyUpstreamKeyBreaker(channelId int, upstreamKey string) error {
	if channelId <= 0 || strings.TrimSpace(upstreamKey) == "" || !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	return common.RedisSet(policyUpstreamKeyBreakerKey(channelId, upstreamKey), "1", 24*time.Hour)
}

func IsUpstreamKeyPolicyBreakerOpen(channelId int, upstreamKey string) bool {
	if channelId <= 0 || strings.TrimSpace(upstreamKey) == "" || !common.RedisEnabled || common.RDB == nil {
		return false
	}
	_, err := common.RedisGet(policyUpstreamKeyBreakerKey(channelId, upstreamKey))
	return err == nil
}

func SetPolicyTokenBreaker(tokenId int) error {
	if tokenId <= 0 || !common.RedisEnabled || common.RDB == nil {
		return nil
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

func acquirePolicyIncidentLock(channelId int, upstreamKey string) bool {
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
		types.ErrorCodeChannelNoAvailableKey,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}
