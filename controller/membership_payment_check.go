package controller

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func CheckMembershipPayment(c *gin.Context) {
	requestMembershipPaymentCheck(c, false)
}

func AdminCheckMembershipPayment(c *gin.Context) {
	requestMembershipPaymentCheck(c, true)
}

func requestMembershipPaymentCheck(c *gin.Context, admin bool) {
	actorID := c.GetInt("id")
	if actorID <= 0 {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	userID, source := actorID, "self"
	if admin {
		userID, source = 0, "admin"
	}
	// Ownership is checked before loading credentials or scheduling work. No
	// client-supplied amount, receipt, paid flag or beneficiary is accepted.
	order, err := model.GetMembershipOrder(c.Param("trade_no"), userID)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	if order.Provider != model.PaymentProviderEpay {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	if order.Status == model.MembershipOrderSuccess {
		membershipAPIError(c, model.ErrMembershipConflict)
		return
	}
	if err := service.RequestMembershipPaymentCheck(c.Request.Context(), order, source); err != nil {
		membershipAPIError(c, err)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("membership_payment check requested actor_id=%d order_id=%d source=%s", actorID, order.ID, source))
	view, err := membershipOrderResponse(order, admin)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	if err := loadMembershipPaymentProgress(c.Request.Context(), []*membershipOrderView{view}); err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// Progress is loaded in two bounded queries for the whole order page. Neither
// encrypted snapshots nor raw provider evidence is loaded into an API DTO.
func loadMembershipPaymentProgress(ctx context.Context, views []*membershipOrderView) error {
	byID := make(map[int64]*membershipOrderView)
	ids := make([]int64, 0, len(views))
	for _, view := range views {
		if view != nil && view.epay && view.Status != model.MembershipOrderSuccess {
			byID[view.ID] = view
			ids = append(ids, view.ID)
		}
	}
	if len(ids) == 0 || !model.MembershipPaymentSchemaAvailable() {
		return nil
	}
	for _, view := range byID {
		view.CanCheckPayment = service.MembershipPaymentReconcileEnabled() && (view.Status == model.MembershipOrderPending || view.Status == model.MembershipOrderClosed) && view.PaymentState != "review"
	}
	var checks []struct {
		OrderID int64
		Status  string
	}
	if err := model.DB.WithContext(ctx).Model(&model.MembershipPaymentCheck{}).Select("order_id", "status").Where("order_id IN ?", ids).Find(&checks).Error; err != nil {
		return err
	}
	for _, check := range checks {
		view := byID[check.OrderID]
		if check.Status == model.MembershipPaymentReview {
			view.CanCheckPayment = false
			if view.PaidAt == 0 {
				view.PaymentState = "review"
			}
		} else if check.Status == model.MembershipPaymentPending {
			view.PaymentState = "checking"
		}
	}
	var evidence []struct {
		OrderID         int64
		ValidationError string
	}
	if err := model.DB.WithContext(ctx).Model(&model.MembershipPaymentEvidence{}).Select("order_id", "validation_error").Where("order_id IN ? AND status = ?", ids, model.MembershipPaymentPending).Find(&evidence).Error; err != nil {
		return err
	}
	for _, payment := range evidence {
		view := byID[payment.OrderID]
		view.PaymentState, view.CanCheckPayment = "processing", false
		if payment.ValidationError != "" {
			view.PaymentState = "review"
		}
	}
	return nil
}
