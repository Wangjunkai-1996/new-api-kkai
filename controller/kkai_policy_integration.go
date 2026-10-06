package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func kkaiTaskAPIError(taskErr *dto.TaskError) *types.NewAPIError {
	if taskErr == nil {
		return nil
	}
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	return types.NewOpenAIError(
		err,
		types.ErrorCodeBadResponseStatusCode,
		taskErr.StatusCode,
		types.ErrOptionWithOriginalStatusCode(taskErr.UpstreamStatusCode),
		types.ErrOptionWithOriginalErrorCode(types.ErrorCode(taskErr.UpstreamErrorCode)),
		types.ErrOptionWithPolicyEvidence(taskErr.PolicyEvidence),
	)
}

func processKKAIPolicyTaskError(c *gin.Context, channel types.ChannelError, taskErr *dto.TaskError) bool {
	if taskErr != nil && service.IsKKAILocalPolicyCode(taskErr.Code) {
		service.MarkKKAIPolicyNoRetry(c)
		return true
	}
	guard := service.NewKKAIPolicyIncidentGuard(service.NewRiskActionService(model.DB))
	detected, err := guard.HandleTaskError(c, channel, taskErr)
	if err != nil {
		logger.LogError(c, "KKAI policy incident persistence failed: "+err.Error())
	}
	return detected
}
