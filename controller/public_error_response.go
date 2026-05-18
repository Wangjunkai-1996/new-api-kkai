package controller

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type publicErrorCategory string

const (
	publicErrorPolicyBlocked       publicErrorCategory = "policy_blocked"
	publicErrorUpstreamUnavailable publicErrorCategory = "upstream_unavailable"
	publicErrorUpstreamSanitized   publicErrorCategory = "upstream_error_sanitized"
	publicErrorPassthrough         publicErrorCategory = "passthrough"
	publicErrorRedacted                                = "[redacted]"
)

type publicErrorClassification struct {
	Category   publicErrorCategory
	StatusCode int
	CaseID     string
}

var (
	publicAuthorizationPattern = regexp.MustCompile(`(?i)\b(authorization|x-api-key|api-key|api_key|upstream[_ -]?key|client[_ -]?token)\s*[:=]\s*(bearer\s+)?[^\s,;)}\]]+`)
	publicBearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	publicSKLikePattern        = regexp.MustCompile(`(?i)\bsk-[a-z0-9][a-z0-9._-]{6,}`)
	publicLinkPattern          = regexp.MustCompile(`(?i)(https?://[^\s,;)}\]]+|t\.me/[^\s,;)}\]]+|discord\.gg/[^\s,;)}\]]+)`)
	publicNoisyWordPattern     = regexp.MustCompile(`(?i)\b(telegram|wechat|qq group|buy key|buy api|recharge|contact us)\b`)
	publicNoisyCJKReplacer     = strings.NewReplacer(
		"广告", publicErrorRedacted,
		"购买", publicErrorRedacted,
		"充值", publicErrorRedacted,
		"加群", publicErrorRedacted,
		"联系我们", publicErrorRedacted,
		"qq群", publicErrorRedacted,
		"微信", publicErrorRedacted,
	)
)

func publicOpenAIError(c *gin.Context, apiErr *types.NewAPIError) (int, types.OpenAIError) {
	classification := classifyPublicAPIError(c, apiErr)
	switch classification.Category {
	case publicErrorPolicyBlocked:
		return http.StatusForbidden, withPolicyCaseID(types.OpenAIError{
			Message: types.PublicMessageRequestBlockedByPolicy,
			Type:    string(types.ErrorTypeNewAPIError),
			Code:    types.ErrorCodePolicyBlocked,
		}, classification.CaseID)
	case publicErrorUpstreamUnavailable:
		return http.StatusServiceUnavailable, withPolicyCaseID(upstreamUnavailableOpenAIError(), classification.CaseID)
	case publicErrorUpstreamSanitized:
		return classification.StatusCode, upstreamErrorSanitizedOpenAIError()
	}

	openAIError := apiErr.ToOpenAIError()
	return classification.StatusCode, scrubPublicOpenAIError(c, openAIError)
}

func publicClaudeError(c *gin.Context, apiErr *types.NewAPIError) (int, types.ClaudeError) {
	statusCode, openAIError := publicOpenAIError(c, apiErr)
	return statusCode, types.ClaudeError{
		Message: openAIError.Message,
		Type:    fmt.Sprintf("%v", openAIError.Code),
		CaseID:  openAIError.CaseID,
	}
}

func publicTaskError(c *gin.Context, taskErr *dto.TaskError) *dto.TaskError {
	if taskErr == nil {
		return nil
	}
	publicErr := *taskErr

	classification := classifyPublicTaskError(c, taskErr)
	switch classification.Category {
	case publicErrorPolicyBlocked:
		publicErr.CaseID = classification.CaseID
		publicErr.Code = string(types.ErrorCodePolicyBlocked)
		publicErr.Message = types.PublicMessageRequestBlockedByPolicy
		publicErr.StatusCode = http.StatusForbidden
		return &publicErr
	case publicErrorUpstreamUnavailable:
		publicErr.CaseID = classification.CaseID
		publicErr.Code = string(types.ErrorCodeUpstreamUnavailable)
		publicErr.Message = types.PublicMessageUpstreamUnavailable
		publicErr.StatusCode = http.StatusServiceUnavailable
		return &publicErr
	case publicErrorUpstreamSanitized:
		publicErr.Code = string(types.ErrorTypeUpstreamError)
		publicErr.Message = types.PublicMessageUpstreamError
		publicErr.CaseID = ""
	default:
		publicErr.Message = scrubPublicText(c, publicErr.Message)
	}
	return &publicErr
}

func classifyPublicAPIError(c *gin.Context, apiErr *types.NewAPIError) publicErrorClassification {
	if apiErr == nil {
		return publicErrorClassification{Category: publicErrorUpstreamSanitized, StatusCode: http.StatusInternalServerError}
	}

	classification := service.ClassifyPolicyIncident(apiErr)
	if classification.Detected {
		if classification.ClientTokenActionAllowed {
			return publicErrorClassification{Category: publicErrorPolicyBlocked, StatusCode: http.StatusForbidden, CaseID: policyIncidentCaseID(c)}
		}
		return publicErrorClassification{Category: publicErrorUpstreamUnavailable, StatusCode: http.StatusServiceUnavailable, CaseID: policyIncidentCaseID(c)}
	}

	if isPolicyIncidentContext(c) {
		return publicErrorClassification{Category: publicErrorPolicyBlocked, StatusCode: http.StatusForbidden, CaseID: policyIncidentCaseID(c)}
	}

	if types.IsUpstreamUnavailableError(apiErr) {
		return publicErrorClassification{Category: publicErrorUpstreamUnavailable, StatusCode: http.StatusServiceUnavailable}
	}

	statusCode := apiErr.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	if shouldSanitizeUnsafeUpstreamError(c, apiErr, apiErr.ToOpenAIError().Message) {
		return publicErrorClassification{Category: publicErrorUpstreamSanitized, StatusCode: statusCode}
	}
	return publicErrorClassification{Category: publicErrorPassthrough, StatusCode: statusCode}
}

func classifyPublicTaskError(c *gin.Context, taskErr *dto.TaskError) publicErrorClassification {
	if taskErr == nil {
		return publicErrorClassification{Category: publicErrorUpstreamSanitized, StatusCode: http.StatusInternalServerError}
	}
	classification := taskPolicyIncidentClassification(taskErr)
	if classification.Detected {
		if classification.ClientTokenActionAllowed {
			return publicErrorClassification{Category: publicErrorPolicyBlocked, StatusCode: http.StatusForbidden, CaseID: policyIncidentCaseID(c)}
		}
		return publicErrorClassification{Category: publicErrorUpstreamUnavailable, StatusCode: http.StatusServiceUnavailable, CaseID: policyIncidentCaseID(c)}
	}
	if isTaskUpstreamUnavailableError(taskErr) {
		return publicErrorClassification{Category: publicErrorUpstreamUnavailable, StatusCode: http.StatusServiceUnavailable}
	}
	if taskErr.StatusCode == http.StatusServiceUnavailable && types.LooksLikeNoisyUpstreamMessage(taskErr.Message) {
		return publicErrorClassification{Category: publicErrorUpstreamUnavailable, StatusCode: http.StatusServiceUnavailable}
	}
	statusCode := taskErr.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	if !taskErr.LocalError && publicMessageLooksUnsafe(c, taskErr.Message) {
		return publicErrorClassification{Category: publicErrorUpstreamSanitized, StatusCode: statusCode}
	}
	if !taskErr.LocalError && taskErr.Error != nil && publicMessageLooksUnsafe(c, taskErr.Error.Error()) {
		return publicErrorClassification{Category: publicErrorUpstreamSanitized, StatusCode: statusCode}
	}
	return publicErrorClassification{Category: publicErrorPassthrough, StatusCode: statusCode}
}

func taskPolicyIncidentClassification(taskErr *dto.TaskError) service.PolicyIncidentClassification {
	if taskErr == nil {
		return service.PolicyIncidentClassification{}
	}
	apiErr := types.NewErrorWithStatusCode(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	classification := service.ClassifyPolicyIncident(apiErr)
	if classification.Detected || taskErr.Error == nil {
		return classification
	}
	apiErr = types.NewErrorWithStatusCode(taskErr.Error, types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	return service.ClassifyPolicyIncident(apiErr)
}

func isTaskUpstreamUnavailableError(taskErr *dto.TaskError) bool {
	if taskErr == nil {
		return false
	}
	switch taskErr.Code {
	case "policy_breaker_open", "channel_no_available_key":
		return true
	}
	apiErr := types.NewErrorWithStatusCode(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	if types.IsUpstreamUnavailableError(apiErr) {
		return true
	}
	if taskErr.Error == nil {
		return false
	}
	apiErr = types.NewErrorWithStatusCode(taskErr.Error, types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	return types.IsUpstreamUnavailableError(apiErr)
}

func upstreamUnavailableOpenAIError() types.OpenAIError {
	return types.OpenAIError{
		Message: types.PublicMessageUpstreamUnavailable,
		Type:    string(types.ErrorTypeUpstreamError),
		Code:    types.ErrorCodeUpstreamUnavailable,
	}
}

func upstreamErrorSanitizedOpenAIError() types.OpenAIError {
	return types.OpenAIError{
		Message: types.PublicMessageUpstreamError,
		Type:    string(types.ErrorTypeUpstreamError),
		Code:    types.ErrorTypeUpstreamError,
	}
}

func withPolicyCaseID(openAIError types.OpenAIError, caseID string) types.OpenAIError {
	if caseID == "" {
		return openAIError
	}
	openAIError.CaseID = caseID
	metadata, err := common.Marshal(map[string]string{"case_id": caseID})
	if err == nil {
		openAIError.Metadata = metadata
	}
	return openAIError
}

func isPolicyIncidentContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.GetContextKeyBool(c, constant.ContextKeyPolicyIncidentDetected) ||
		service.ShouldSkipRetryAfterPolicyIncident(c)
}

func shouldSanitizeUnsafeUpstreamError(c *gin.Context, apiErr *types.NewAPIError, message string) bool {
	if apiErr == nil {
		return false
	}
	if !isPublicUpstreamError(apiErr) {
		return false
	}
	return publicMessageLooksUnsafe(c, message) || publicMessageLooksUnsafe(c, apiErr.Error())
}

func isPublicUpstreamError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	switch apiErr.GetErrorType() {
	case types.ErrorTypeOpenAIError, types.ErrorTypeClaudeError, types.ErrorTypeUpstreamError:
		return true
	}
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeDoRequestFailed,
		types.ErrorCodeReadResponseBodyFailed,
		types.ErrorCodeBadResponseStatusCode,
		types.ErrorCodeBadResponse,
		types.ErrorCodeBadResponseBody,
		types.ErrorCodeEmptyResponse,
		types.ErrorCodeAwsInvokeError,
		types.ErrorCodePromptBlocked,
		types.ErrorCodeChannelAwsClientError,
		types.ErrorCodeChannelInvalidKey:
		return true
	default:
		return false
	}
}

func publicMessageLooksUnsafe(c *gin.Context, message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	if types.LooksLikeNoisyUpstreamMessage(message) ||
		publicAuthorizationPattern.MatchString(message) ||
		publicBearerPattern.MatchString(message) ||
		publicSKLikePattern.MatchString(message) ||
		publicLinkPattern.MatchString(message) {
		return true
	}
	for _, secret := range publicContextSecretVariants(c) {
		if strings.Contains(message, secret) {
			return true
		}
	}
	return false
}

func scrubPublicOpenAIError(c *gin.Context, openAIError types.OpenAIError) types.OpenAIError {
	openAIError.Message = scrubPublicText(c, openAIError.Message)
	openAIError.Param = scrubPublicText(c, openAIError.Param)
	openAIError.Metadata = scrubPublicMetadata(c, openAIError.Metadata)
	return openAIError
}

func scrubPublicText(c *gin.Context, text string) string {
	if text == "" {
		return text
	}
	text = common.MaskSensitiveInfo(text)
	for _, secret := range publicContextSecretVariants(c) {
		text = strings.ReplaceAll(text, secret, publicErrorRedacted)
	}
	text = publicAuthorizationPattern.ReplaceAllString(text, "$1: "+publicErrorRedacted)
	text = publicBearerPattern.ReplaceAllString(text, publicErrorRedacted)
	text = publicSKLikePattern.ReplaceAllString(text, publicErrorRedacted)
	text = publicLinkPattern.ReplaceAllString(text, publicErrorRedacted)
	text = publicNoisyWordPattern.ReplaceAllString(text, publicErrorRedacted)
	text = publicNoisyCJKReplacer.Replace(text)
	return text
}

func scrubPublicMetadata(c *gin.Context, metadata []byte) []byte {
	if len(metadata) == 0 {
		return nil
	}
	var decoded any
	if err := common.Unmarshal(metadata, &decoded); err != nil {
		scrubbed, marshalErr := common.Marshal(scrubPublicText(c, string(metadata)))
		if marshalErr != nil {
			return nil
		}
		return scrubbed
	}
	scrubbed, err := common.Marshal(scrubPublicMetadataValue(c, decoded))
	if err != nil {
		return nil
	}
	return scrubbed
}

func scrubPublicMetadataValue(c *gin.Context, value any) any {
	switch v := value.(type) {
	case map[string]any:
		scrubbed := make(map[string]any, len(v))
		for key, item := range v {
			if isPublicSensitiveMetadataKey(key) {
				scrubbed[key] = publicErrorRedacted
				continue
			}
			scrubbed[key] = scrubPublicMetadataValue(c, item)
		}
		return scrubbed
	case []any:
		scrubbed := make([]any, len(v))
		for i, item := range v {
			scrubbed[i] = scrubPublicMetadataValue(c, item)
		}
		return scrubbed
	case string:
		return scrubPublicText(c, v)
	default:
		return value
	}
}

func isPublicSensitiveMetadataKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "upstreamkey") ||
		strings.Contains(normalized, "clienttoken")
}

func publicContextSecretVariants(c *gin.Context) []string {
	secrets := make([]string, 0, 12)
	if c == nil {
		return secrets
	}
	if c.Request != nil {
		addPublicSecretVariants(&secrets, c.Request.Header.Get("Authorization"))
	}
	addPublicSecretVariants(&secrets, c.GetString("token_key"))
	addPublicSecretVariants(&secrets, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	return secrets
}

func addPublicSecretVariants(secrets *[]string, value string) {
	add := func(variant string) {
		variant = strings.TrimSpace(variant)
		if variant == "" {
			return
		}
		for _, existing := range *secrets {
			if existing == variant {
				return
			}
		}
		*secrets = append(*secrets, variant)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	add(value)
	if strings.HasPrefix(value, "sk-") {
		add(strings.TrimPrefix(value, "sk-"))
	} else {
		add("sk-" + value)
	}
	fields := strings.Fields(value)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		add(fields[1])
		add("Bearer " + fields[1])
		add("bearer " + fields[1])
		return
	}
	add("Bearer " + value)
	add("bearer " + value)
}

func policyIncidentCaseID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if caseID := c.GetString(service.PolicyIncidentCaseIDContextKey); caseID != "" {
		return caseID
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" || model.DB == nil {
		return ""
	}
	var event model.PolicyIncidentEvent
	if err := model.DB.Where("request_id = ?", requestID).Order("id DESC").First(&event).Error; err != nil {
		return ""
	}
	return policyIncidentCaseIDFromMetadata(event.Metadata)
}

func policyIncidentCaseIDFromMetadata(metadata model.JSONValue) string {
	if len(metadata) == 0 {
		return ""
	}
	var decoded map[string]any
	if err := common.Unmarshal([]byte(metadata), &decoded); err != nil {
		return ""
	}
	caseID, _ := decoded["case_id"].(string)
	return caseID
}
