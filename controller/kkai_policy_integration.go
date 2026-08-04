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

func processKKAIPolicyAPIError(c *gin.Context, channel types.ChannelError, apiErr *types.NewAPIError) bool {
	guard := service.NewKKAIPolicyIncidentGuard(service.NewRiskActionService(model.DB))
	detected, err := guard.HandleAPIError(c, channel, apiErr)
	if err != nil {
		logger.LogError(c, "KKAI policy incident persistence failed: "+err.Error())
	}
	return detected
}

func kkaiTaskAPIError(taskErr *dto.TaskError) *types.NewAPIError {
	if taskErr == nil {
		return nil
	}
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
}

func processKKAIPolicyTaskError(c *gin.Context, channel types.ChannelError, taskErr *dto.TaskError) bool {
	guard := service.NewKKAIPolicyIncidentGuard(service.NewRiskActionService(model.DB))
	detected, err := guard.HandleTaskError(c, channel, taskErr)
	if err != nil {
		logger.LogError(c, "KKAI policy incident persistence failed: "+err.Error())
	}
	return detected
}
