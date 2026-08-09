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
	return classifyKKAIPolicyText(apiErr.GetOriginalStatusCode(), string(apiErr.GetOriginalErrorCode()), apiErr.Error())
}

func ClassifyKKAITaskPolicyError(taskErr *dto.TaskError) KKAIPolicyClassification {
	if taskErr == nil || taskErr.LocalError {
		return KKAIPolicyClassification{}
	}
	evidence := taskErr.Message
	if taskErr.Error != nil {
		evidence += " " + taskErr.Error.Error()
	}
	return classifyKKAIPolicyText(taskErr.StatusCode, taskErr.Code, evidence)
}

func classifyKKAIPolicyText(statusCode int, errorCode string, evidence string) KKAIPolicyClassification {
	if statusCode != http.StatusForbidden {
		return KKAIPolicyClassification{}
	}
	maskedEvidence := common.MaskSensitiveInfo(strings.TrimSpace(evidence))
	searchText := strings.ToLower(strings.TrimSpace(errorCode + " " + maskedEvidence))
	clientPolicy := containsKKAIPolicyMarker(searchText, kkaiClientPolicyMarkers)
	upstreamKeyPolicy := containsKKAIPolicyMarker(searchText, kkaiUpstreamKeyPolicyMarkers)
	if !clientPolicy && !upstreamKeyPolicy {
		return KKAIPolicyClassification{}
	}

	causality := KKAIPolicyCausalityUpstreamKey
	if clientPolicy && upstreamKeyPolicy {
		causality = KKAIPolicyCausalityAmbiguous
	} else if clientPolicy {
		causality = KKAIPolicyCausalityClientToken
	}
	return KKAIPolicyClassification{
		Detected:   true,
		Causality:  causality,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Evidence:   maskedEvidence,
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
