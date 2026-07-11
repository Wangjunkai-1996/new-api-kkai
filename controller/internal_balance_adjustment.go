package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func CreateInternalBalanceAdjustment(c *gin.Context) {
	var request dto.InternalBalanceAdjustmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeInternalBalanceError(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	result, err := service.CreateInternalBalanceAdjustment(request)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidInternalBalanceAdjustment):
			writeInternalBalanceError(c, http.StatusBadRequest, "invalid_request", "invalid request")
		case errors.Is(err, service.ErrInternalBalanceUserNotFound):
			writeInternalBalanceError(c, http.StatusNotFound, "user_not_found", "user not found")
		case errors.Is(err, service.ErrInternalBalanceIdempotencyConflict):
			writeInternalBalanceError(c, http.StatusConflict, "idempotency_conflict", "operation conflict")
		case errors.Is(err, service.ErrInternalBalanceReversalConflict):
			writeInternalBalanceError(c, http.StatusConflict, "reversal_conflict", "reversal conflict")
		case errors.Is(err, service.ErrInternalBalanceInsufficientBalance):
			writeInternalBalanceError(c, http.StatusUnprocessableEntity, "insufficient_balance", "insufficient balance")
		case errors.Is(err, service.ErrInternalBalanceOverflow):
			writeInternalBalanceError(c, http.StatusUnprocessableEntity, "balance_overflow", "balance limit exceeded")
		default:
			common.SysLog("internal balance adjustment failed")
			writeInternalBalanceError(c, http.StatusInternalServerError, "internal_error", "internal error")
		}
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{
		"success": true,
		"data":    result,
	})
}

func writeInternalBalanceError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}
