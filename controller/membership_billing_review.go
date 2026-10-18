package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminListMembershipBillingReviews(c *gin.Context) {
	status := c.DefaultQuery("status", "pending")
	if status != "pending" && status != "resolved" {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	records, err := model.ListMembershipBillingReviews(status == "resolved", limit)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, records)
}

func AdminGetMembershipBillingReview(c *gin.Context) {
	record, err := model.GetMembershipBillingReview(c.Param("request_id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, record)
}

func AdminResolveMembershipBillingReview(c *gin.Context) {
	var input struct {
		OperationID string `json:"operation_id"`
		Action      string `json:"action"`
		ActualQuota *int64 `json:"actual_quota"`
		Reason      string `json:"reason"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil || (input.Action == "settle" && input.ActualQuota == nil) {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	actual := int64(0)
	if input.ActualQuota != nil {
		actual = *input.ActualQuota
	}
	record, err := model.ResolveMembershipBillingReview(model.MembershipBillingResolutionInput{RequestID: c.Param("request_id"), ActorID: c.GetInt("id"), OperationID: input.OperationID, Action: input.Action, ActualQuota: actual, Reason: input.Reason})
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, record)
}
