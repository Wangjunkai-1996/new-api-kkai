package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const membershipUndispatchedReviewDelay int64 = 60 * 60
const membershipSettlementReviewGrace int64 = 60

type MembershipBillingResolutionInput struct {
	RequestID   string
	ActorID     int
	OperationID string
	Action      string
	ActualQuota int64
	Reason      string
}

type membershipBillingResolution struct {
	OperationID string `json:"operation_id"`
	ActorID     int    `json:"actor_id"`
	Action      string `json:"action"`
	ActualQuota int64  `json:"actual_quota"`
	Reason      string `json:"reason"`
	At          int64  `json:"at"`
}

// MarkMembershipBillingDispatched must commit before upstream I/O. deadlineUnix
// is the caller's enforced transport deadline, including draining late usage for
// synchronous relays. Confirmed Midjourney acceptance replaces this with the
// native task's investigation threshold; neither threshold releases funds.
func MarkMembershipBillingDispatched(requestID string, channelID int, deadlineUnix int64) error {
	now := common.GetTimestamp()
	if strings.TrimSpace(requestID) == "" || channelID <= 0 || deadlineUnix <= now || deadlineUnix > math.MaxInt64-membershipSettlementReviewGrace {
		return ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return err
	}
	initial, err := GetMembershipBillingRecord(requestID)
	if err != nil {
		return err
	}
	return membershipTransaction(func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
			return err
		}
		if (record.Scope != MembershipBillingScopeRelay && record.Scope != MembershipBillingScopeMidjourney) || record.State != "reserved" || record.ReviewStartedAt > 0 {
			return ErrMembershipConflict
		}
		if record.DispatchedAt > 0 {
			if record.ReviewChannelID != channelID || record.ReviewAfter != deadlineUnix+membershipSettlementReviewGrace {
				return ErrMembershipConflict
			}
			return nil
		}
		now = common.GetTimestamp()
		if deadlineUnix <= now || record.CreatedAt <= now-membershipUndispatchedReviewDelay {
			return ErrMembershipConflict
		}
		return tx.Model(&record).Updates(map[string]any{"dispatched_at": now, "review_channel_id": channelID, "review_after": deadlineUnix + membershipSettlementReviewGrace}).Error
	})
}

// ClearMembershipBillingRejectedAttempt permits another channel only when the
// caller has confirmed that this attempt was not accepted and incurred no usage.
// An unknown outcome must never enter this path.
func ClearMembershipBillingRejectedAttempt(requestID string, channelID int) error {
	if strings.TrimSpace(requestID) == "" || channelID <= 0 {
		return ErrMembershipInvalidInput
	}
	initial, err := GetMembershipBillingRecord(requestID)
	if err != nil {
		return err
	}
	return membershipTransaction(func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
			return err
		}
		if record.Scope != MembershipBillingScopeRelay || record.State != "reserved" || record.ReviewStartedAt > 0 || record.DispatchedAt == 0 || record.ReviewChannelID != channelID {
			return ErrMembershipConflict
		}
		return tx.Model(&record).Updates(map[string]any{"dispatched_at": 0, "review_channel_id": 0, "review_after": 0}).Error
	})
}

// MarkMembershipBillingUncertain is called only after the submitting transport
// has stopped. It retains the full authorization until evidence establishes usage.
func MarkMembershipBillingUncertain(requestID string, channelID int, reason string) error {
	if strings.TrimSpace(requestID) == "" || channelID < 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return err
	}
	initial, err := GetMembershipBillingRecord(requestID)
	if err != nil {
		return err
	}
	return membershipTransaction(func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
			return err
		}
		if record.Scope != MembershipBillingScopeRelay && record.Scope != MembershipBillingScopeMidjourney {
			return ErrMembershipConflict
		}
		if record.State != "reserved" {
			return nil
		}
		if record.ReviewStartedAt > 0 {
			if record.ReviewChannelID != channelID {
				return ErrMembershipConflict
			}
			return nil
		}
		if record.DispatchedAt > 0 && record.ReviewChannelID != channelID {
			return ErrMembershipConflict
		}
		return tx.Model(&record).Updates(map[string]any{"review_reason": reason, "review_channel_id": channelID, "review_started_at": common.GetTimestamp()}).Error
	})
}

func ListMembershipBillingReviews(resolved bool, limit int) ([]MembershipBillingRecord, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	records := []MembershipBillingRecord{}
	now := common.GetTimestamp()
	query := DB.Where("billing_scope IN ?", []string{MembershipBillingScopeRelay, MembershipBillingScopeMidjourney, MembershipBillingScopeTask})
	if resolved {
		query = query.Where("review_started_at > 0 AND state <> ?", "reserved")
	} else {
		query = query.Where("state = ? AND (review_started_at > 0 OR (billing_scope <> ? AND ((dispatched_at = 0 AND created_at <= ?) OR (dispatched_at > 0 AND review_after > 0 AND review_after <= ?))))", "reserved", MembershipBillingScopeTask, now-membershipUndispatchedReviewDelay, now)
	}
	err := query.Order("review_started_at asc").Limit(limit).Find(&records).Error
	for i := range records {
		records[i].prepareMembershipReview(now)
	}
	return records, err
}

func GetMembershipBillingReview(requestID string) (*MembershipBillingRecord, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	record, err := GetMembershipBillingRecord(requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMembershipNotFound
	}
	if err != nil {
		return nil, err
	}
	if !record.prepareMembershipReview(common.GetTimestamp()) {
		return nil, ErrMembershipNotFound
	}
	return record, nil
}

// prepareMembershipReview exposes orphan authorizations without moving money.
// A resolver persists the derived reason atomically with its final adjustment.
func (record *MembershipBillingRecord) prepareMembershipReview(now int64) bool {
	if record.Scope == MembershipBillingScopeTask {
		// Native task recovery owns the uncertainty decision. Age alone cannot
		// make an active asynchronous task eligible for manual accounting.
		return record.ReviewStartedAt > 0
	}
	if record.Scope != MembershipBillingScopeRelay && record.Scope != MembershipBillingScopeMidjourney {
		return false
	}
	if record.ReviewStartedAt > 0 {
		return true
	}
	if record.State != "reserved" {
		return false
	}
	if record.DispatchedAt == 0 && record.CreatedAt <= now-membershipUndispatchedReviewDelay {
		record.ReviewStartedAt = record.CreatedAt + membershipUndispatchedReviewDelay
		record.ReviewReason = "authorization_stalled_before_dispatch"
		return true
	}
	if record.DispatchedAt > 0 && record.ReviewAfter > 0 && record.ReviewAfter <= now {
		record.ReviewStartedAt = record.ReviewAfter
		record.ReviewReason = "dispatch_deadline_elapsed_without_settlement"
		if record.Scope == MembershipBillingScopeMidjourney {
			// An asynchronous job may still be running. Age only makes it
			// discoverable for investigation; the resolver must verify its cost.
			record.ReviewReason = "midjourney_outcome_requires_review"
		}
		return true
	}
	return false
}

// ResolveMembershipBillingReview never moves funds between sources and never
// grants more than the original authorization. The review and money changes
// share one transaction; identical operation retries do not repeat statistics.
func ResolveMembershipBillingReview(input MembershipBillingResolutionInput) (*MembershipBillingRecord, error) {
	input.OperationID, input.Reason = strings.TrimSpace(input.OperationID), strings.TrimSpace(input.Reason)
	if input.ActorID <= 0 || input.OperationID == "" || len(input.OperationID) > 128 || input.Reason == "" || len(input.Reason) > 2048 || input.ActualQuota < 0 || (input.Action != "settle" && input.Action != "release") || (input.Action == "release" && input.ActualQuota != 0) {
		return nil, ErrMembershipInvalidInput
	}
	initial, err := GetMembershipBillingReview(input.RequestID)
	if err != nil {
		return nil, err
	}
	var result *MembershipBillingRecord
	err = membershipTransaction(func(tx *gorm.DB) error {
		var task *Task
		if initial.Scope == MembershipBillingScopeTask {
			var err error
			// Native task funding always locks Task before User and Ledger.
			// Preserve that ordering when entering from the common review API.
			task, err = lockTaskMembershipBillingReviewTx(tx, initial)
			if err != nil {
				return err
			}
		}
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).First(&record, "request_id = ?", input.RequestID).Error; err != nil {
			return err
		}
		if record.ReviewResolvedAt > 0 {
			var previous membershipBillingResolution
			if err := common.UnmarshalJsonStr(record.ReviewResolution, &previous); err != nil {
				return err
			}
			if previous.OperationID != input.OperationID || previous.ActorID != input.ActorID || previous.Action != input.Action || previous.ActualQuota != input.ActualQuota || previous.Reason != input.Reason {
				return ErrMembershipConflict
			}
			result = &record
			return nil
		}
		if record.State != "reserved" || !record.prepareMembershipReview(common.GetTimestamp()) {
			return ErrMembershipConflict
		}
		var err error
		if record.Scope == MembershipBillingScopeTask {
			result, err = resolveTaskMembershipBillingReviewTx(tx, task, &record, input)
		} else if record.Scope == MembershipBillingScopeMidjourney {
			result, err = resolveMidjourneyMembershipBillingTx(tx, &record, input)
		} else if input.Action == "release" {
			result, err = ReleaseMembershipBillingTx(tx, input.RequestID)
		} else {
			result, err = settleMembershipRelayBillingTx(tx, input.RequestID, input.ActualQuota, record.ReviewChannelID, true)
		}
		if err != nil {
			return err
		}
		audit := membershipBillingResolution{OperationID: input.OperationID, ActorID: input.ActorID, Action: input.Action, ActualQuota: input.ActualQuota, Reason: input.Reason, At: common.GetTimestamp()}
		encoded, err := common.Marshal(audit)
		if err != nil {
			return err
		}
		result.ReviewResolution, result.ReviewResolvedAt = string(encoded), audit.At
		result.ReviewReason, result.ReviewStartedAt = record.ReviewReason, record.ReviewStartedAt
		return tx.Model(result).Updates(map[string]any{"review_reason": result.ReviewReason, "review_started_at": result.ReviewStartedAt, "review_resolution": result.ReviewResolution, "review_resolved_at": result.ReviewResolvedAt}).Error
	})
	if err != nil {
		return nil, err
	}
	MembershipBillingAfterCommit(result)
	return result, nil
}
