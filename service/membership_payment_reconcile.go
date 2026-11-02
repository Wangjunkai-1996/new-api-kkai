package service

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	MembershipPaymentReconcileTaskType = "membership_payment_reconcile"
	MembershipPaymentRetryTaskType     = "membership_payment_retry"
)

func MembershipPaymentReconcileEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("KKAI_MEMBERSHIP_PAYMENT_RECONCILE_ENABLED")), "true")
}

func MembershipPaymentFulfillEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("KKAI_MEMBERSHIP_PAYMENT_FULFILL_ENABLED")), "true")
}

type membershipPaymentReconcileHandler struct{}

func (membershipPaymentReconcileHandler) Type() string { return MembershipPaymentReconcileTaskType }
func (membershipPaymentReconcileHandler) Enabled() bool {
	return common.CanRunWriteBackgroundJobs() && model.MembershipEnabled() && MembershipPaymentReconcileEnabled() && model.MembershipPaymentSchemaAvailable()
}
func (membershipPaymentReconcileHandler) Interval() time.Duration { return 15 * time.Second }
func (membershipPaymentReconcileHandler) NewPayload() any         { return nil }
func (membershipPaymentReconcileHandler) Run(ctx context.Context, task *model.SystemTask, runner string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	counts, err := ReconcileDueMembershipPayments(ctx, 20)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, "membership payment reconciliation unavailable"
	}
	_ = model.FinishSystemTask(task.TaskID, runner, status, counts, message)
}

type membershipPaymentRetryHandler struct{}

func (membershipPaymentRetryHandler) Type() string { return MembershipPaymentRetryTaskType }
func (membershipPaymentRetryHandler) Enabled() bool {
	return common.CanRunWriteBackgroundJobs() && model.MembershipEnabled() && MembershipPaymentFulfillEnabled() && model.MembershipPaymentSchemaAvailable()
}
func (membershipPaymentRetryHandler) Interval() time.Duration { return 15 * time.Second }
func (membershipPaymentRetryHandler) NewPayload() any         { return nil }
func (membershipPaymentRetryHandler) Run(ctx context.Context, task *model.SystemTask, runner string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	counts, err := RetryDueMembershipPayments(ctx, 20)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, "membership payment fulfillment unavailable"
	}
	_ = model.FinishSystemTask(task.TaskID, runner, status, counts, message)
}

func init() {
	RegisterSystemTaskHandler(membershipPaymentReconcileHandler{})
	RegisterSystemTaskHandler(membershipPaymentRetryHandler{})
}

func ReconcileDueMembershipPayments(ctx context.Context, limit int) (map[string]int, error) {
	counts := map[string]int{"complete": 0, "review": 0, "pending": 0, "processing": 0, "failed": 0}
	if !common.CanRunWriteBackgroundJobs() || !MembershipPaymentReconcileEnabled() {
		return counts, errors.New("write background jobs disabled")
	}
	if err := CheckMembershipPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return counts, err
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	imported, review, err := discoverLegacyMembershipEpayPayments(ctx, limit)
	counts["legacy_imported"], counts["legacy_review"] = imported, review
	if err != nil {
		return counts, err
	}
	checks, err := model.ListDueMembershipPaymentChecks(ctx, time.Now().Unix(), limit)
	if err != nil {
		return counts, err
	}
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return counts, err
		}
		result, err := ReconcileMembershipPayment(ctx, check.OrderID)
		if err != nil {
			counts["failed"]++
			continue
		}
		counts[result]++
	}
	return counts, ctx.Err()
}

// ReconcileMembershipPayment is a writer-only operation. User requests enqueue
// a check instead of making network requests or granting membership inline.
func ReconcileMembershipPayment(ctx context.Context, orderID int64) (string, error) {
	return reconcileMembershipPayment(ctx, orderID, http.DefaultClient)
}

func reconcileMembershipPayment(ctx context.Context, orderID int64, client *http.Client) (string, error) {
	if !common.CanRunWriteBackgroundJobs() || !MembershipPaymentReconcileEnabled() {
		return "failed", errors.New("write background jobs disabled")
	}
	if err := CheckMembershipPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return "failed", err
	}
	check, err := model.ClaimMembershipPaymentCheck(ctx, orderID, time.Now().Unix())
	if err != nil {
		return "failed", err
	}
	result := model.MembershipPaymentCheckResult{Status: model.MembershipPaymentPending}
	delay := min(time.Duration(1<<min(check.Attempts, 6))*15*time.Second, 5*time.Minute)
	if check.Attempts >= 32 || time.Now().Unix()-check.CreatedAt >= 24*60*60 {
		delay = time.Hour
	}
	result.NextCheckAt = time.Now().Add(delay).Unix()
	var order model.MembershipOrder
	if err := model.DB.WithContext(ctx).First(&order, orderID).Error; err != nil {
		return "failed", err
	}
	if order.Status == model.MembershipOrderSuccess || order.Status == model.MembershipOrderException || order.ResolvedAt > 0 {
		result.Status = model.MembershipPaymentReview
		if order.Status == model.MembershipOrderSuccess {
			result.Status = model.MembershipPaymentComplete
		}
		return result.Status, model.FinishMembershipPaymentCheck(ctx, orderID, check.ClaimToken, result)
	}
	snapshot, err := MembershipEpayGateway(ctx, &order)
	if err != nil {
		result.Reason = "PAYMENT_SNAPSHOT_UNAVAILABLE"
		if errors.Is(err, ErrMembershipPaymentEvidence) {
			result.Status, result.Reason = model.MembershipPaymentReview, "PAYMENT_SNAPSHOT_INVALID"
		}
		return result.Status, model.FinishMembershipPaymentCheck(ctx, orderID, check.ClaimToken, result)
	}
	if err := WaitEpayQueryBudget(ctx, model.DB, snapshot.APIURL, snapshot.MerchantID); err != nil {
		return "failed", err
	}
	queried, err := queryMembershipEpay(ctx, client, &order, snapshot)
	if contextErr := ctx.Err(); contextErr != nil {
		return "failed", contextErr
	}
	if err != nil {
		result.Reason = "PAYMENT_PROVIDER_UNAVAILABLE"
		if errors.Is(err, ErrMembershipPaymentEvidence) {
			result.Status, result.Reason = model.MembershipPaymentReview, "PAYMENT_EVIDENCE_INVALID"
		} else {
			var queryFailure *membershipQueryFailure
			if errors.As(err, &queryFailure) {
				result.Reason = queryFailure.reason
			}
		}
		if saveErr := model.FinishMembershipPaymentCheck(ctx, orderID, check.ClaimToken, result); saveErr != nil {
			return "failed", saveErr
		}
		return result.Status, nil
	}
	if !queried.Paid {
		result.Reason = "PAYMENT_NOT_CONFIRMED"
		return result.Status, model.FinishMembershipPaymentCheck(ctx, orderID, check.ClaimToken, result)
	}
	evidence, err := model.PersistVerifiedMembershipPayment(ctx, model.MembershipVerifiedPaymentInput{
		OrderID: order.ID, ClaimToken: check.ClaimToken, MerchantID: snapshot.MerchantID,
		ReceiptID: queried.ReceiptID, AmountMinor: queried.AmountMinor, Currency: queried.Currency,
		ProviderPaidAt: queried.ProviderPaidAt, ValidationError: queried.ValidationError,
	})
	if err != nil {
		return "failed", err
	}
	if !MembershipPaymentFulfillEnabled() {
		return "processing", nil
	}
	// The paid fact is committed before fulfillment begins. A crash or database
	// error from here leaves an independently retryable evidence row.
	fulfilled, err := model.RetryVerifiedMembershipPayment(ctx, evidence.ID)
	if err != nil {
		return "pending", err
	}
	if fulfilled.Status == model.MembershipOrderSuccess {
		return "complete", nil
	}
	return "review", nil
}

func RetryDueMembershipPayments(ctx context.Context, limit int) (map[string]int, error) {
	counts := map[string]int{"complete": 0, "review": 0, "failed": 0}
	if !common.CanRunWriteBackgroundJobs() || !MembershipPaymentFulfillEnabled() {
		return counts, errors.New("write background jobs disabled")
	}
	if err := CheckMembershipPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return counts, err
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	evidence, err := model.ListDueMembershipPaymentEvidence(ctx, time.Now().Unix(), limit)
	if err != nil {
		return counts, err
	}
	for _, receipt := range evidence {
		if err := ctx.Err(); err != nil {
			return counts, err
		}
		order, err := model.RetryVerifiedMembershipPayment(ctx, receipt.ID)
		if err != nil {
			counts["failed"]++
		} else if order.Status == model.MembershipOrderSuccess {
			counts["complete"]++
		} else {
			counts["review"]++
		}
	}
	return counts, ctx.Err()
}
