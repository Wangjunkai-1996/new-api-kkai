package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ReconcileDueMembershipRelayBilling closes aged synchronous authorizations.
// Async task scopes have separate ownership and are never aged out here.
func ReconcileDueMembershipRelayBilling(ctx context.Context, limit int) (int, error) {
	if err := CheckMembershipSchema(); err != nil {
		return 0, err
	}
	if LOG_DB == nil {
		return 0, errors.New("membership billing log database unavailable")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	now := time.Now().Unix()
	cutoff := now - membershipRelayCompensationDelay
	resolved := 0
	scanned := 0
	var failures []error
	var lastCreated int64
	var lastID string
	for resolved < limit && scanned < 500 {
		var candidates []MembershipBillingRecord
		query := DB.WithContext(ctx).Model(&MembershipBillingRecord{}).
			Where("state = ? AND billing_scope = ? AND created_at <= ?", "reserved", MembershipBillingScopeRelay, cutoff).
			Where("review_started_at > 0 OR dispatched_at = 0 OR (review_after > 0 AND review_after <= ?)", now).
			Where("created_at > ? OR (created_at = ? AND request_id > ?)", lastCreated, lastCreated, lastID).
			Order("created_at, request_id").Limit(min(100, 500-scanned))
		if err := query.Find(&candidates).Error; err != nil {
			return resolved, errors.Join(append(failures, err)...)
		}
		if len(candidates) == 0 {
			break
		}
		lastCreated, lastID = candidates[len(candidates)-1].CreatedAt, candidates[len(candidates)-1].RequestID
		ids := make([]string, 0, len(candidates))
		byID := make(map[string][]Log, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.RequestID)
		}
		var logs []Log
		if err := LOG_DB.WithContext(ctx).Model(&Log{}).
			Select("request_id", "user_id", "token_id", "channel_id", "type", "quota", "other").
			Where("request_id IN ? AND type = ?", ids, LogTypeConsume).Find(&logs).Error; err != nil {
			return resolved, errors.Join(append(failures, err)...)
		}
		for _, log := range logs {
			byID[log.RequestId] = append(byID[log.RequestId], log)
		}
		for _, candidate := range candidates {
			scanned++
			if err := ctx.Err(); err != nil {
				return resolved, errors.Join(append(failures, err)...)
			}
			if resolved >= limit {
				break
			}
			input := MembershipBillingResolutionInput{
				RequestID: candidate.RequestID, ActorID: 0, OperationID: "auto-relay-2h",
				Action: "release", Reason: "Platform compensation: no attributable charge evidence after 2 hours",
			}
			if receipts := byID[candidate.RequestID]; len(receipts) > 0 {
				if len(receipts) != 1 {
					failures = append(failures, fmt.Errorf("membership request %s has %d consume receipts", candidate.RequestID, len(receipts)))
					continue
				}
				log := receipts[0]
				var evidence struct {
					BillingSource string `json:"billing_source"`
					AdminInfo     struct {
						MembershipBilling struct {
							RequestID string `json:"request_id"`
							Assessed  *int64 `json:"assessed"`
							Charged   *int64 `json:"charged"`
						} `json:"membership_billing"`
					} `json:"admin_info"`
				}
				if err := common.UnmarshalJsonStr(log.Other, &evidence); err != nil ||
					log.UserId != candidate.UserID || log.TokenId != candidate.TokenID ||
					log.ChannelId != candidate.ReviewChannelID ||
					evidence.BillingSource != candidate.Source ||
					evidence.AdminInfo.MembershipBilling.RequestID != candidate.RequestID ||
					evidence.AdminInfo.MembershipBilling.Assessed == nil ||
					evidence.AdminInfo.MembershipBilling.Charged == nil ||
					*evidence.AdminInfo.MembershipBilling.Assessed < 0 ||
					*evidence.AdminInfo.MembershipBilling.Charged != int64(log.Quota) ||
					*evidence.AdminInfo.MembershipBilling.Charged != min(*evidence.AdminInfo.MembershipBilling.Assessed, candidate.ReservedQuota) {
					failures = append(failures, fmt.Errorf("membership request %s has conflicting consume receipt", candidate.RequestID))
					continue
				}
				if *evidence.AdminInfo.MembershipBilling.Assessed > 0 {
					input.ActualQuota = *evidence.AdminInfo.MembershipBilling.Assessed
					input.Action = "settle"
					input.Reason = "Automated reconciliation: matching consume receipt confirms assessed quota"
				} else {
					input.Reason = "Automated reconciliation: matching consume receipt confirms zero charge"
				}
			}
			if _, err := resolveMembershipBillingReview(input, true); err != nil {
				failures = append(failures, fmt.Errorf("membership request %s: %w", candidate.RequestID, err))
				continue
			}
			resolved++
		}
	}
	return resolved, errors.Join(failures...)
}
