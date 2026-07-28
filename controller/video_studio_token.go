package controller

import (
	"errors"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type ensureVideoStudioTokenRequest struct {
	Model string `json:"model"`
}

func GetVideoStudioTokenStatus(c *gin.Context) {
	capability, err := service.GetVideoStudioTokenStatus(
		c.Request.Context(), model.DB, c.GetInt("id"), c.Query("model"), c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, capability)
}

func EnsureVideoStudioToken(c *gin.Context) {
	var request ensureVideoStudioTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		respondVideoStudioError(c, service.ErrInvalidVideoStudioSubmission)
		return
	}
	result, err := service.EnsureVideoStudioToken(
		c.Request.Context(), model.DB, c.GetInt("id"), request.Model, c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
