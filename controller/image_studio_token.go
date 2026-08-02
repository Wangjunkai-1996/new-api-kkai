package controller

import (
	"errors"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type ensureImageStudioTokenRequest struct {
	Model string `json:"model"`
}

func GetImageStudioTokenStatus(c *gin.Context) {
	capability, err := service.GetImageStudioTokenStatus(
		c.Request.Context(), model.DB, c.GetInt("id"), c.Query("model"), c.ClientIP(),
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, capability)
}

func EnsureImageStudioToken(c *gin.Context) {
	var request ensureImageStudioTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		respondImageStudioError(c, service.ErrInvalidImageStudioSubmission)
		return
	}
	result, err := service.EnsureImageStudioToken(
		c.Request.Context(), model.DB, c.GetInt("id"), request.Model, c.ClientIP(),
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
