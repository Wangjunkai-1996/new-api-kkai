package service

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

const (
	KKAIPolicyCausalityClientToken = "client_token"
	KKAIPolicyCausalityUpstreamKey = "upstream_key"
	KKAIPolicyCausalityAmbiguous   = "ambiguous"
)

var kkaiClientPolicyMarkers = []string{
	"cyber_policy",
	"network abuse ban",
	"网络滥用封禁",
}

var kkaiUpstreamKeyPolicyMarkers = []string{
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
	"account has been disabled",
	"account is disabled",
	"当前 api key 已永久禁用",
	"当前 api key 已禁用",
	"当前 api key 被禁用",
	"账户已禁用",
	"账号已禁用",
}

type KKAIPolicyClassification struct {
	Detected   bool
	Causality  string
	StatusCode int
	ErrorCode  string
	Evidence   string
}

func ClassifyKKAIUpstreamPolicyError(apiErr *types.NewAPIError) KKAIPolicyClassification {
	if apiErr == nil {
		return KKAIPolicyClassification{}
	}
	if IsKKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode())) {
		return KKAIPolicyClassification{}
	}
	evidence := strings.TrimSpace(apiErr.GetPolicyEvidence())
	return classifyKKAIPolicyText(apiErr.GetOriginalStatusCode(), string(apiErr.GetOriginalErrorCode()), evidence)
}

func ClassifyKKAITaskPolicyError(taskErr *dto.TaskError) KKAIPolicyClassification {
	if taskErr == nil || taskErr.LocalError {
		return KKAIPolicyClassification{}
	}
	if IsKKAILocalPolicyCode(taskErr.Code, taskErr.UpstreamErrorCode) {
		return KKAIPolicyClassification{}
	}
	return classifyKKAIPolicyText(taskErr.UpstreamStatusCode, taskErr.UpstreamErrorCode, taskErr.PolicyEvidence)
}

func classifyKKAIPolicyText(statusCode int, errorCode string, evidence string) KKAIPolicyClassification {
	if IsKKAILocalPolicyCode(errorCode) {
		return KKAIPolicyClassification{}
	}
	// Any marker with a concrete upstream status suppresses failover and is
	// recorded. Sub2 can return a confirmed cyber_policy with HTTP 400, so the
	// exact structured code, rather than one particular status, establishes
	// client causality. Plain-text markers remain ambiguous.
	if statusCode < 100 || statusCode > 599 {
		return KKAIPolicyClassification{}
	}
	maskedEvidence := common.MaskSensitiveInfo(strings.TrimSpace(evidence))
	normalizedCode := strings.ToLower(strings.TrimSpace(errorCode))
	searchText := strings.ToLower(strings.TrimSpace(normalizedCode + " " + maskedEvidence))
	clientPolicyConfirmed := normalizedCode == "cyber_policy"
	clientPolicyMarker := containsKKAIPolicyMarker(searchText, kkaiClientPolicyMarkers)
	upstreamKeyPolicy := containsKKAIPolicyMarker(searchText, kkaiUpstreamKeyPolicyMarkers)
	if !clientPolicyMarker && !upstreamKeyPolicy {
		return KKAIPolicyClassification{}
	}

	causality := KKAIPolicyCausalityAmbiguous
	if clientPolicyConfirmed && !upstreamKeyPolicy {
		causality = KKAIPolicyCausalityClientToken
	} else if upstreamKeyPolicy && !clientPolicyMarker {
		causality = KKAIPolicyCausalityUpstreamKey
	} else if clientPolicyMarker && upstreamKeyPolicy {
		causality = KKAIPolicyCausalityAmbiguous
	}
	return KKAIPolicyClassification{
		Detected:   true,
		Causality:  causality,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Evidence:   maskedEvidence,
	}
}

func IsKKAILocalPolicyCode(values ...string) bool {
	return KKAILocalPolicyCode(values...) != ""
}

func KKAILocalPolicyCode(values ...string) types.ErrorCode {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, code := range []types.ErrorCode{
			types.ErrorCodeRequestPolicyBlocked,
			types.ErrorCodePolicyContextIncomplete,
			types.ErrorCodePolicyAuditUnavailable,
			types.ErrorCodeSessionBlockedByCyberPolicy,
		} {
			if value == string(code) {
				return code
			}
		}
	}
	return ""
}

func KKAILocalPolicyStatus(code types.ErrorCode) int {
	switch code {
	case types.ErrorCodePolicyContextIncomplete:
		return http.StatusUnprocessableEntity
	case types.ErrorCodePolicyAuditUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusForbidden
	}
}

func containsKKAIPolicyMarker(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func kkaiPolicyMarkerEvidence(text string) string {
	searchText := strings.ToLower(text)
	matches := make([]string, 0, len(kkaiClientPolicyMarkers)+len(kkaiUpstreamKeyPolicyMarkers))
	for _, markers := range [][]string{kkaiClientPolicyMarkers, kkaiUpstreamKeyPolicyMarkers} {
		for _, marker := range markers {
			if strings.Contains(searchText, marker) {
				matches = append(matches, marker)
			}
		}
	}
	return strings.Join(matches, " ")
}

func KKAIPolicyMarkerEvidence(text string) string {
	return kkaiPolicyMarkerEvidence(text)
}
