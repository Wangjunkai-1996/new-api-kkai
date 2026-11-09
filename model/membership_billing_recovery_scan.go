package model

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrMembershipRecoveryFenceLost = errors.New("membership recovery leader ownership lost")

type MembershipRecoveryLeaderFence struct {
	Name   string
	Holder string
	Fence  int64
}

// RequireMembershipRecoveryLeaderTx fences background writes in their owning
// transaction. The lease lock precedes all user, ledger and recovery locks.
func RequireMembershipRecoveryLeaderTx(tx *gorm.DB, fence MembershipRecoveryLeaderFence) error {
	if tx == nil || fence.Name == "" || fence.Holder == "" || fence.Fence <= 0 {
		return ErrMembershipRecoveryFenceLost
	}
	if err := tx.Model(&KKAIJobLease{}).Where("lease_name = ?", fence.Name).
		UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
		return err
	}
	var lease KKAIJobLease
	if err := lockForUpdate(tx).First(&lease, "lease_name = ?", fence.Name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMembershipRecoveryFenceLost
		}
		return err
	}
	var query string
	switch tx.Dialector.Name() {
	case "postgres":
		query = "SELECT FLOOR(EXTRACT(EPOCH FROM clock_timestamp()))::bigint"
	case "mysql":
		query = "SELECT UNIX_TIMESTAMP()"
	case "sqlite":
		query = "SELECT CAST(strftime('%s','now') AS INTEGER)"
	default:
		return ErrMembershipRecoveryFenceLost
	}
	var now int64
	if err := tx.Raw(query).Scan(&now).Error; err != nil {
		return err
	}
	// DB time is second-granular. Refuse the boundary second rather than
	// starting a financial transaction with effectively no lease remaining.
	if lease.Holder != fence.Holder || lease.Fence != fence.Fence || lease.LeaseUntil <= now+1 {
		return ErrMembershipRecoveryFenceLost
	}
	return nil
}

type MembershipBillingScanProgress struct {
	Lane                string `gorm:"type:varchar(32);primaryKey"`
	CursorAt            int64  `gorm:"not null"`
	CursorRequestID     string `gorm:"type:varchar(128);not null"`
	SweepUpperAt        int64  `gorm:"not null"`
	SweepUpperRequestID string `gorm:"type:varchar(128);not null"`
	Revision            int64  `gorm:"not null"`
	UpdatedAt           int64  `gorm:"not null"`
}

func (MembershipBillingScanProgress) TableName() string {
	return "kkai_membership_billing_scan_progress"
}

func membershipBillingScanQuery(db *gorm.DB, lane string, now int64) (*gorm.DB, string, error) {
	lane = strings.TrimPrefix(lane, "observe_")
	query := db.Model(&MembershipBillingRecord{}).Where("billing_scope = ? AND state = ?", MembershipBillingScopeRelay, "reserved")
	column := "created_at"
	switch lane {
	case "aged":
		query = query.Where("created_at <= ?", now-72*60*60).
			Where("review_started_at > 0 OR dispatched_at = 0 OR (review_after > 0 AND review_after <= ?)", now)
	case "dispatched_orphan":
		column = "review_after"
		query = query.Where("review_after > 0 AND review_after <= ?", now)
	case "undispatched_orphan":
		query = query.Where("dispatched_at = 0 AND created_at <= ?", now-membershipUndispatchedReviewDelay)
	default:
		return nil, "", ErrMembershipInvalidInput
	}
	if lane != "aged" {
		// Existing evidence has its own due queue. Rediscovering it as an
		// orphan repeats account locks and bypasses conflict backoff.
		existing := db.Model(&MembershipBillingRecovery{}).Select("1").
			Where("kkai_membership_billing_recovery.request_id = kkai_membership_billing.request_id")
		query = query.Where("NOT EXISTS (?)", existing)
	}
	return query, column, nil
}

// MembershipBillingScanPage freezes each sweep's upper key, so new arrivals
// cannot indefinitely prevent the cursor returning to earlier deferred rows.
func MembershipBillingScanPage(ctx context.Context, lane string, now int64, limit int) ([]MembershipBillingRecord, *MembershipBillingScanProgress, error) {
	if limit < 1 || limit > 100 || now <= 0 {
		return nil, nil, ErrMembershipInvalidInput
	}
	if _, _, err := membershipBillingScanQuery(DB, lane, now); err != nil {
		return nil, nil, err
	}
	var progress MembershipBillingScanProgress
	err := membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		err := lockForUpdate(tx).First(&progress, "lane = ?", lane).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			progress = MembershipBillingScanProgress{Lane: lane, Revision: 1, UpdatedAt: now}
			if err := tx.Create(&progress).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if progress.SweepUpperRequestID != "" {
			return nil
		}
		query, column, err := membershipBillingScanQuery(tx, lane, now)
		if err != nil {
			return err
		}
		var upper MembershipBillingRecord
		err = query.Select("request_id", "created_at", "review_after").Order(column + " DESC, request_id DESC").First(&upper).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		progress.SweepUpperAt = upper.CreatedAt
		if column == "review_after" {
			progress.SweepUpperAt = upper.ReviewAfter
		}
		progress.SweepUpperRequestID = upper.RequestID
		progress.Revision++
		progress.UpdatedAt = now
		return tx.Save(&progress).Error
	})
	if err != nil {
		return nil, nil, err
	}
	if progress.SweepUpperRequestID == "" {
		return nil, &progress, nil
	}
	query, column, err := membershipBillingScanQuery(DB.WithContext(ctx), lane, now)
	if err != nil {
		return nil, nil, err
	}
	var records []MembershipBillingRecord
	err = query.Where(column+" > ? OR ("+column+" = ? AND request_id > ?)", progress.CursorAt, progress.CursorAt, progress.CursorRequestID).
		Where(column+" < ? OR ("+column+" = ? AND request_id <= ?)", progress.SweepUpperAt, progress.SweepUpperAt, progress.SweepUpperRequestID).
		Order(column + ", request_id").Limit(limit).Find(&records).Error
	return records, &progress, err
}

// AdvanceMembershipBillingScan accepts only the last durably handled row, never
// the last row merely fetched. Reset is permitted only after an empty page.
func AdvanceMembershipBillingScan(ctx context.Context, progress *MembershipBillingScanProgress, last *MembershipBillingRecord, now int64) error {
	if progress == nil || progress.Revision <= 0 || now <= 0 {
		return ErrMembershipInvalidInput
	}
	_, column, err := membershipBillingScanQuery(DB, progress.Lane, now)
	if err != nil {
		return err
	}
	updates := map[string]any{"revision": progress.Revision + 1, "updated_at": now}
	if last == nil {
		updates["cursor_at"], updates["cursor_request_id"] = int64(0), ""
		updates["sweep_upper_at"], updates["sweep_upper_request_id"] = int64(0), ""
	} else {
		at := last.CreatedAt
		if column == "review_after" {
			at = last.ReviewAfter
		}
		if at < progress.CursorAt || (at == progress.CursorAt && last.RequestID <= progress.CursorRequestID) || at > progress.SweepUpperAt || (at == progress.SweepUpperAt && last.RequestID > progress.SweepUpperRequestID) {
			return ErrMembershipConflict
		}
		updates["cursor_at"], updates["cursor_request_id"] = at, last.RequestID
	}
	return membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&MembershipBillingScanProgress{}).Where("lane = ? AND revision = ?", progress.Lane, progress.Revision).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMembershipConflict
		}
		return nil
	})
}

// DeferMembershipBillingRecovery changes scheduling only. It cannot discard
// saved evidence or reopen a completed financial adjustment.
func DeferMembershipBillingRecovery(ctx context.Context, requestID string, revision int64, code string, conflict bool, now int64) error {
	if requestID == "" || revision <= 0 || code == "" || len(code) > 64 || now <= 0 {
		return ErrMembershipInvalidInput
	}
	return membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		// Scheduling never touches funds. Keep Ledger -> Recovery order, but
		// do not block unrelated requests on this member's account lock.
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).Select("state", "billing_scope").First(&record, "request_id = ?", requestID).Error; err != nil {
			return err
		}
		if record.Scope != MembershipBillingScopeRelay {
			return ErrMembershipConflict
		}
		var recovery MembershipBillingRecovery
		if err := lockForUpdate(tx).First(&recovery, "request_id = ?", requestID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if recovery.Revision != revision || recovery.State == "resolved" {
			return nil // A concurrent result already superseded this attempt.
		}
		seconds := int64(60) << min(max(recovery.AttemptCount, 0), 4)
		updates := map[string]any{"next_attempt_at": now + min(seconds, 900), "attempt_count": recovery.AttemptCount + 1, "last_checked_at": now, "last_error_code": code, "revision": revision + 1, "updated_at": now}
		if conflict && record.State == "reserved" {
			updates["state"] = "conflict"
		}
		return tx.Model(&recovery).Updates(updates).Error
	})
}
