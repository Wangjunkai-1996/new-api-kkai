package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The router installs AdminAuth. The explicit guard also prevents accidental
// reuse of a sensitive handler from a tenant owner route.
func resellerFinancePlatformAdmin(c *gin.Context) bool {
	tenant, _ := middleware.GetTenantContext(c)
	if c.GetInt("id") <= 0 || c.GetInt("role") < common.RoleAdminUser || tenant.TenantID != model.PlatformTenantID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "platform finance access required"})
		return false
	}
	if model.DB == nil {
		resellerNotReady(c)
		return false
	}
	return true
}

func AdminListResellerWithdrawals(c *gin.Context) {
	if !resellerFinancePlatformAdmin(c) {
		return
	}
	page := common.GetPageQuery(c)
	query := model.DB.Model(&model.ResellerWithdrawal{})
	if status := c.Query("status"); status != "" {
		switch status {
		case model.ResellerWithdrawalRequested, model.ResellerWithdrawalApproved, model.ResellerWithdrawalPaid, model.ResellerWithdrawalRejected, model.ResellerWithdrawalFailed:
			query = query.Where("status = ?", status)
		default:
			resellerBadRequest(c, "invalid withdrawal status")
			return
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var withdrawals []model.ResellerWithdrawal
	if err := query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&withdrawals).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// Payee snapshots are returned only from owner/admin routes, never ledger views.
	common.ApiSuccess(c, gin.H{"items": withdrawals, "total": total})
}

func AdminReviewResellerWithdrawal(c *gin.Context) {
	if !resellerFinancePlatformAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		Status     string `json:"status"`
		PaymentRef string `json:"payment_ref"`
		Note       string `json:"note"`
	}
	if err != nil || id <= 0 || common.DecodeJson(c.Request.Body, &request) != nil {
		resellerBadRequest(c, "invalid withdrawal review")
		return
	}
	withdrawal, err := service.NewResellerFinanceService(model.DB).ReviewWithdrawal(c.Request.Context(), model.ResellerWithdrawalReviewInput{
		WithdrawalID: id, ReviewerID: c.GetInt("id"), Status: request.Status, PaymentRef: request.PaymentRef, Note: request.Note,
	})
	if err != nil {
		resellerFinanceAdminError(c, err)
		return
	}
	common.ApiSuccess(c, withdrawal)
}

// AdminReverseResellerSettlement corrects a single profit record. Customer
// payment refunds/chargebacks must use AdminReverseResellerPayment so unspent
// quota and every affected settlement are reversed in the same transaction.
func AdminReverseResellerSettlement(c *gin.Context) {
	if !resellerFinancePlatformAdmin(c) {
		return
	}
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		OriginalOperation string `json:"original_operation"`
		Reason            string `json:"reason"`
	}
	if err != nil || tenantID <= 0 || common.DecodeJson(c.Request.Body, &request) != nil || len(request.Reason) > 4096 {
		resellerBadRequest(c, "invalid settlement reversal")
		return
	}
	entry, err := service.NewResellerFinanceService(model.DB).ReverseSettlement(c.Request.Context(), model.ResellerChargebackInput{
		TenantID: tenantID, OriginalOperation: request.OriginalOperation, Reason: request.Reason, ActorID: c.GetInt("id"),
	})
	if err != nil {
		resellerFinanceAdminError(c, err)
		return
	}
	common.ApiSuccess(c, entry)
}

func AdminReverseResellerPayment(c *gin.Context) {
	if !resellerFinancePlatformAdmin(c) {
		return
	}
	tenantID, tenantErr := strconv.ParseInt(c.Param("id"), 10, 64)
	topupID, topupErr := strconv.ParseInt(c.Param("topup_id"), 10, 64)
	var request struct {
		IdempotencyKey   string `json:"idempotency_key"`
		Kind             string `json:"kind"`
		Reason           string `json:"reason"`
		PaymentReference string `json:"payment_reference"`
	}
	if tenantErr != nil || topupErr != nil || tenantID <= 0 || topupID <= 0 || common.DecodeJson(c.Request.Body, &request) != nil {
		resellerBadRequest(c, "invalid customer payment reversal")
		return
	}
	reversal, err := service.NewResellerFinanceService(model.DB).ReversePayment(c.Request.Context(), model.CustomerPaymentReversalInput{TenantID: tenantID, TopUpID: topupID, IdempotencyKey: request.IdempotencyKey, Kind: request.Kind, ActorID: c.GetInt("id"), Reason: request.Reason, PaymentReference: request.PaymentReference})
	if err != nil {
		resellerFinanceAdminError(c, err)
		return
	}
	common.ApiSuccess(c, reversal)
}

func AdminReconcileResellerConsumption(c *gin.Context) {
	if !resellerFinancePlatformAdmin(c) {
		return
	}
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		IdempotencyKey            string `json:"idempotency_key"`
		ActualQuota               *int64 `json:"actual_quota"`
		ConfirmedZero             bool   `json:"confirmed_zero"`
		ConfirmedExecutionStopped bool   `json:"confirmed_execution_stopped"`
		Reason                    string `json:"reason"`
		Evidence                  string `json:"evidence"`
	}
	if err != nil || tenantID <= 0 || common.DecodeJson(c.Request.Body, &request) != nil || (request.ActualQuota == nil && !request.ConfirmedZero) {
		resellerBadRequest(c, "invalid consumption reconciliation")
		return
	}
	actualQuota := int64(0)
	if request.ActualQuota != nil {
		actualQuota = *request.ActualQuota
	}
	entry, err := service.NewResellerFinanceService(model.DB).ReconcileConsumption(c.Request.Context(), model.TenantConsumptionReconciliationInput{
		TenantID: tenantID, RequestID: c.Param("request_id"), IdempotencyKey: request.IdempotencyKey,
		ActualQuota: actualQuota, ConfirmedZero: request.ConfirmedZero, ActorID: c.GetInt("id"),
		ConfirmedExecutionStopped: request.ConfirmedExecutionStopped, Reason: request.Reason, Evidence: request.Evidence,
	})
	if err != nil {
		resellerFinanceAdminError(c, err)
		return
	}
	common.ApiSuccess(c, entry)
}

func resellerFinanceAdminError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, model.ErrResellerLedgerConflict) || errors.Is(err, model.ErrResellerWithdrawalConflict) || errors.Is(err, model.ErrResellerPaymentInFlight) || errors.Is(err, model.ErrTenantConsumptionConflict) {
		status = http.StatusConflict
	} else if !errors.Is(err, model.ErrResellerFinanceInvalidInput) && !errors.Is(err, model.ErrResellerDebtOutstanding) && !errors.Is(err, model.ErrResellerInsufficientFunds) {
		common.ApiError(c, err)
		return
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}
