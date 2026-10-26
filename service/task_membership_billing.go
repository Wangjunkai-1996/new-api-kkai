package service

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func preConsumeTaskMembership(c *gin.Context, task *model.Task, info *relaycommon.RelayInfo) *relaytypes.NewAPIError {
	request := model.TaskBillingReservationRequest{Source: model.MembershipSource, TokenID: info.TokenId, TokenKey: info.TokenKey, SkipToken: info.IsPlayground}
	if task.PrivateData.MembershipBillingRequestID == "" {
		maximum := 0.0
		if info.TieredBillingSnapshot != nil && info.MembershipPricing != nil {
			maximum = info.MembershipPricing.MaximumBeforeGroup
		} else {
			if info.TieredBillingSnapshot != nil || !info.PriceData.UsePrice || info.PriceData.ModelPrice < 0 {
				return membershipBillingError(fmt.Errorf("task pricing must provide a verifiable maximum budget before dispatch"))
			}
			maximum = info.PriceData.ModelPrice * common.QuotaPerUnit
			if !common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
				maximum = info.PriceData.ApplyOtherRatiosToFloat(maximum)
			}
		}
		input, err := PrepareMembershipReservation(info, maximum)
		if err != nil {
			return membershipBillingError(err)
		}
		input.RequestID = "task:" + task.TaskID
		request.Membership = &input
	}
	mutation, err := model.ReserveTaskBilling(context.WithoutCancel(c), task.ID, request)
	if err != nil {
		return membershipBillingError(err)
	}
	if mutation == nil || mutation.Task == nil || mutation.Membership == nil {
		return membershipBillingError(model.ErrTaskBillingInvalidRequest)
	}
	*task = *mutation.Task
	if err := ApplyMembershipReservation(info, mutation.Membership); err != nil {
		return membershipBillingError(err)
	}
	if info.TieredBillingSnapshot != nil {
		info.PriceData.Quota = common.QuotaRound(info.TieredBillingSnapshot.EstimatedQuotaBeforeGroup * info.PriceData.GroupRatioInfo.GroupRatio)
	} else {
		info.PriceData.Quota = common.QuotaFromFloat(info.MembershipPricing.MaximumBeforeGroup * info.PriceData.GroupRatioInfo.GroupRatio)
	}
	info.Billing = &membershipTaskBillingSession{info: info, task: task, reserved: int(mutation.Membership.ReservedQuota)}
	return nil
}

// PrepareTaskMembershipExpressionBudget reserves the submission estimate;
// usage above it is assessed at settlement but borne by the platform.
func PrepareTaskMembershipExpressionBudget(info *relaycommon.RelayInfo, _ map[string]jsplugin.UsageFieldSchema) error {
	if info.MembershipBillingRequestID != "" {
		return nil
	}
	snapshot := info.TieredBillingSnapshot
	if snapshot == nil || !snapshot.TaskUsageBilling {
		return model.ErrMembershipInvalidInput
	}
	for key, fact := range snapshot.UsageFacts {
		switch value := fact.(type) {
		case float64:
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("task usage fact %q must be finite and nonnegative", key)
			}
		case float32:
			if value < 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("task usage fact %q must be finite and nonnegative", key)
			}
		}
	}
	maximum := snapshot.EstimatedQuotaBeforeGroup
	if maximum < 0 || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return model.ErrMembershipInvalidInput
	}
	info.MembershipPricing = &types.MembershipPricing{MaximumBeforeGroup: maximum, QuotaPerUnit: snapshot.QuotaPerUnit}
	return nil
}

// Freeze the charge alongside the terminal status so a crash between status
// persistence and settlement cannot recover using the earlier submission quote.
func prepareTaskMembershipCompletion(task *model.Task, result *relaycommon.TaskInfo) error {
	if task == nil || task.PrivateData.MembershipBillingRequestID == "" || task.Status != model.TaskStatusSuccess {
		return nil
	}
	quota := task.Quota
	if task.PrivateData.TargetQuota != nil {
		quota = *task.PrivateData.TargetQuota
	}
	bc := task.PrivateData.BillingContext
	pricing := task.PrivateData.MembershipPricing
	if bc == nil || pricing == nil {
		return model.ErrTaskBillingInvalidRequest
	}
	if bc.TieredSnapshot != nil {
		facts := map[string]any(nil)
		if result != nil {
			facts = result.UsageFacts
		}
		settlement, usage, err := EvaluateTaskCompletionUsage(bc.TieredSnapshot, facts)
		if err != nil {
			return err
		}
		if settlement.Clamp != nil {
			return settlement.Clamp
		}
		bc.TieredSnapshot.UsageFacts, bc.TieredSnapshot.EstimatedTier = usage, settlement.MatchedTier
		task.PrivateData.MembershipCompletionQuota = common.GetPointer(settlement.ActualQuotaAfterGroup)
		return nil
	}
	if !bc.PerCallBilling {
		return model.ErrTaskBillingInvalidRequest
	}
	if bc.OtherRatios["seconds"] > 0 && result != nil {
		if seconds, ok := result.UsageFacts["seconds"].(float64); ok {
			if seconds <= 0 || seconds > relaycommon.MaxTaskDurationSeconds || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
				return model.ErrTaskBillingInvalidRequest
			}
			multiplier := seconds
			for key, ratio := range bc.OtherRatios {
				if key != "seconds" {
					multiplier *= ratio
				}
			}
			var clamp *common.QuotaClamp
			quota, clamp = common.QuotaFromFloatChecked(bc.ModelPrice * bc.GroupRatio * multiplier * pricing.QuotaPerUnit)
			if clamp != nil {
				return clamp
			}
		}
	}
	task.PrivateData.MembershipCompletionQuota = common.GetPointer(quota)
	return nil
}

type membershipTaskBillingSession struct {
	mu          sync.Mutex
	info        *relaycommon.RelayInfo
	task        *model.Task
	reserved    int
	transferred bool
}

func (s *membershipTaskBillingSession) Settle(actual int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mutation, err := model.AdjustTaskBilling(context.Background(), s.task.ID, actual)
	if err != nil {
		return err
	}
	if mutation != nil && mutation.Task != nil {
		*s.task = *mutation.Task
		s.info.MembershipAssessedQuota = int(s.task.PrivateData.MembershipAssessedQuota)
		s.info.MembershipChargedQuota = s.task.Quota
	}
	s.transferred = true
	return nil
}

func (s *membershipTaskBillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transferred {
		return
	}
	if refundDurableTaskQuota(context.WithoutCancel(c), s.task, "task submission failed") == nil {
		s.transferred = true
	}
}
func (s *membershipTaskBillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.transferred
}
func (s *membershipTaskBillingSession) GetPreConsumedQuota() int { return s.reserved }
func (s *membershipTaskBillingSession) Reserve(quota int) error {
	if quota < 0 || quota > s.reserved {
		return model.ErrTaskBillingInvalidRequest
	}
	return nil
}
