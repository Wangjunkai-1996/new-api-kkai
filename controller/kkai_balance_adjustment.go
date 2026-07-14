package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func CreateKKAIBalanceAdjustment(c *gin.Context) {
	var request dto.KKAIBalanceAdjustmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeKKAIBalanceError(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	result, err := service.ApplyKKAIBalanceAdjustment(request)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentInvalidInput):
			writeKKAIBalanceError(c, http.StatusBadRequest, "invalid_request", "invalid request")
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentUserNotFound):
			writeKKAIBalanceError(c, http.StatusNotFound, "user_not_found", "user not found")
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentIdempotencyConflict):
			writeKKAIBalanceError(c, http.StatusConflict, "idempotency_conflict", "operation conflict")
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentReversalConflict):
			writeKKAIBalanceError(c, http.StatusConflict, "reversal_conflict", "reversal conflict")
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentInsufficientBalance):
			writeKKAIBalanceError(c, http.StatusUnprocessableEntity, "insufficient_balance", "insufficient balance")
		case errors.Is(err, service.ErrKKAIBalanceAdjustmentOverflow):
			writeKKAIBalanceError(c, http.StatusUnprocessableEntity, "balance_overflow", "balance limit exceeded")
		default:
			common.SysLog("KKAI internal balance adjustment failed")
			writeKKAIBalanceError(c, http.StatusInternalServerError, "internal_error", "internal error")
		}
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"success": true, "data": result})
}

func writeKKAIBalanceError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}
