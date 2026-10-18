package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func MidjourneyMembershipRequestID(taskID int) string {
	return "mj:" + strconv.Itoa(taskID)
}

// GetMidjourneyMembershipRecord also recognizes pending authorizations after
// the sales/runtime flag is disabled. Historical installations need no tables.
func GetMidjourneyMembershipRecord(taskID int) (*MembershipBillingRecord, error) {
	if !MembershipEnabled() {
		available, err := membershipTableAvailable(DB, (MembershipBillingRecord{}).TableName())
		if err != nil || !available {
			return nil, err
		}
	}
	record, err := GetMembershipBillingRecord(MidjourneyMembershipRequestID(taskID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil && record.Scope != MembershipBillingScopeMidjourney {
		return nil, ErrMembershipConflict
	}
	return record, err
}

// CreateMidjourneyMembershipTask persists the local identity and authorization
// together, before any upstream I/O. A rolled-back attempt leaves neither row.
func CreateMidjourneyMembershipTask(task *Midjourney, input MembershipReservationInput) (*MembershipBillingRecord, error) {
	if task == nil || task.Id != 0 || task.UserId != input.UserID || task.ChannelId <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	draft := *task
	draft.Status, draft.Progress, draft.MjId, draft.FinishTime = "SUBMITTING", "0%", "", 0
	if draft.SubmitTime == 0 {
		draft.SubmitTime = time.Now().UnixMilli()
	}
	var persisted Midjourney
	var record *MembershipBillingRecord
	err := membershipTransaction(func(tx *gorm.DB) error {
		persisted = draft
		if _, _, err := lockMembershipUserTx(tx, input.UserID); err != nil {
			return err
		}
		if err := tx.Create(&persisted).Error; err != nil {
			return err
		}
		input.RequestID = MidjourneyMembershipRequestID(persisted.Id)
		input.Scope = MembershipBillingScopeMidjourney
		var err error
		record, err = ReserveMembershipBillingTx(tx, input)
		if err != nil {
			return err
		}
		persisted.Quota, persisted.TokenId, persisted.BillingChannelId = int(record.ReservedQuota), record.TokenID, persisted.ChannelId
		return tx.Model(&persisted).Select("quota", "token_id", "billing_channel_id").Updates(&persisted).Error
	})
	if err != nil {
		return nil, err
	}
	*task = persisted
	MembershipBillingAfterCommit(record)
	return record, nil
}

// UpdateMidjourneyMembershipTask applies trusted submission/poll evidence. A
// terminal task and its original funding source always commit in one transaction.
func UpdateMidjourneyMembershipTask(task *Midjourney) (*MembershipBillingRecord, error) {
	if task == nil || task.Id <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	requestID := MidjourneyMembershipRequestID(task.Id)
	initial, err := GetMembershipBillingRecord(requestID)
	if err != nil {
		return nil, err
	}
	update := *task
	var stored Midjourney
	var result *MembershipBillingRecord
	err = membershipTransaction(func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var record MembershipBillingRecord
		if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&stored, task.Id).Error; err != nil {
			return err
		}
		if record.Scope != MembershipBillingScopeMidjourney || stored.UserId != record.UserID || update.UserId != stored.UserId || update.ChannelId != stored.ChannelId || (stored.MjId != "" && update.MjId != stored.MjId) {
			return ErrMembershipConflict
		}
		if record.State != "reserved" {
			result = &record
			return nil
		}
		firstBinding := stored.MjId == "" && update.MjId != ""
		// Immutable identity and funding fields come only from the locked row.
		update.Id, update.UserId, update.ChannelId = stored.Id, stored.UserId, stored.ChannelId
		update.Action, update.TokenId, update.BillingChannelId = stored.Action, stored.TokenId, stored.BillingChannelId
		update.Quota, update.SubmitTime = stored.Quota, stored.SubmitTime
		if update.Status != "SUCCESS" && update.Status != "FAILURE" && update.Progress == "100%" {
			// The legacy poll query uses progress as its completion marker.
			// Progress alone cannot stop recovery before a terminal status.
			update.Progress = stored.Progress
			if update.Progress == "100%" {
				update.Progress = "0%"
			}
		}
		stored = update
		if stored.Status == "SUCCESS" || stored.Status == "FAILURE" {
			result, err = completeMidjourneyMembershipTx(tx, &stored, &record, record.ReservedQuota, stored.Status == "FAILURE")
			return err
		}
		if firstBinding {
			// HTTP submission finished, but async rendering may legitimately take
			// much longer. One hour only exposes a review, never a refund.
			record.ReviewAfter = common.GetTimestamp() + 60*60
			if err := tx.Model(&record).Update("review_after", record.ReviewAfter).Error; err != nil {
				return err
			}
		}
		result = &record
		return tx.Model(&stored).Select("*").Updates(&stored).Error
	})
	if err != nil {
		return nil, err
	}
	*task = stored
	MembershipBillingAfterCommit(result)
	return result, nil
}

// The caller already holds user, ledger and task locks in that order.
func completeMidjourneyMembershipTx(tx *gorm.DB, task *Midjourney, record *MembershipBillingRecord, actualQuota int64, release bool) (*MembershipBillingRecord, error) {
	var result *MembershipBillingRecord
	var err error
	if release {
		result, err = ReleaseMembershipBillingTx(tx, record.RequestID)
	} else {
		result, err = SettleMembershipBillingTx(tx, record.RequestID, actualQuota)
	}
	if err != nil {
		return nil, err
	}
	task.Quota, task.Progress = int(result.ChargedQuota), "100%"
	if task.FinishTime == 0 {
		task.FinishTime = time.Now().UnixMilli()
	}
	if err := tx.Model(task).Select("*").Updates(task).Error; err != nil {
		return nil, err
	}
	if !release && result.Applied {
		if err := tx.Model(&User{}).Where("id = ?", result.UserID).Updates(map[string]any{"used_quota": gorm.Expr("used_quota + ?", result.ChargedQuota), "request_count": gorm.Expr("request_count + 1")}).Error; err != nil {
			return nil, err
		}
		if result.ChargedQuota > 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", task.GetBillingChannelId()).Update("used_quota", gorm.Expr("used_quota + ?", result.ChargedQuota)).Error; err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func resolveMidjourneyMembershipBillingTx(tx *gorm.DB, record *MembershipBillingRecord, input MembershipBillingResolutionInput) (*MembershipBillingRecord, error) {
	if record.Scope != MembershipBillingScopeMidjourney || record.State != "reserved" || !record.prepareMembershipReview(common.GetTimestamp()) {
		return nil, ErrMembershipConflict
	}
	taskID, err := strconv.Atoi(strings.TrimPrefix(record.RequestID, "mj:"))
	if err != nil || taskID <= 0 || record.RequestID != MidjourneyMembershipRequestID(taskID) {
		return nil, ErrMembershipConflict
	}
	var task Midjourney
	if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
		return nil, err
	}
	if task.UserId != record.UserID || (record.ReviewChannelID > 0 && task.ChannelId != record.ReviewChannelID) {
		return nil, ErrMembershipConflict
	}
	// A verified charge does not prove that a usable image was delivered.
	task.Status, task.Description = "UNKNOWN", "Billing reconciled"
	if input.Action == "release" {
		task.Status, task.FailReason = "FAILURE", "Cancelled after billing review"
	}
	return completeMidjourneyMembershipTx(tx, &task, record, input.ActualQuota, input.Action == "release")
}
