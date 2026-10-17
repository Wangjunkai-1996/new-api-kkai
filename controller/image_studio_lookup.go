package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func LookupImageStudioSubmission(c *gin.Context) {
	result, err := service.LookupImageSubmission(c.Request.Context(), model.DB, c.GetInt("id"), c.GetHeader("Idempotency-Key"))
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, result)
}
