package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	MembershipBillingScopeRelay      = "relay"
	MembershipBillingScopeImage      = "image"
	MembershipBillingScopeTask       = "task"
	MembershipBillingScopeMidjourney = "midjourney"
)

func ReserveMembershipBilling(input MembershipReservationInput) (*MembershipBillingRecord, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	var result *MembershipBillingRecord
	err := membershipTransaction(func(tx *gorm.DB) error {
		var err error
		result, err = ReserveMembershipBillingTx(tx, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	MembershipBillingAfterCommit(result)
	return result, nil
}

// ReserveMembershipBillingTx allows task/image acceptance to share the funding
// transaction. The caller MUST invoke MembershipBillingAfterCommit after commit.
func ReserveMembershipBillingTx(tx *gorm.DB, input MembershipReservationInput) (*MembershipBillingRecord, error) {
	if !MembershipEnabled() {
		return nil, ErrMembershipDisabled
	}
	if input.Scope == "" {
		input.Scope = MembershipBillingScopeRelay
	}
	if input.Scope != MembershipBillingScopeRelay && input.Scope != MembershipBillingScopeImage && input.Scope != MembershipBillingScopeTask && input.Scope != MembershipBillingScopeMidjourney {
		return nil, ErrMembershipInvalidInput
	}
	if tx == nil || input.UserID <= 0 || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 || input.GroupKey == "" || len(input.GroupKey) > 64 || input.SubscriptionQuota < 0 || input.SubscriptionQuota > math.MaxInt32 || input.WalletQuota < 0 || input.WalletQuota > math.MaxInt32 {
		return nil, ErrMembershipInvalidInput
	}
	// Persist only billing facts, never the API token secret.
	fingerprintInput := input
	fingerprintInput.TokenKey = ""
	encoded, err := common.Marshal(fingerprintInput)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	user, _, err := lockMembershipUserTx(tx, input.UserID)
	if err != nil {
		return nil, err
	}
	var existing MembershipBillingRecord
	err = lockForUpdate(tx).First(&existing, "request_id = ?", input.RequestID).Error
	if err == nil {
		if existing.UserID != input.UserID || existing.RequestHash != fingerprint {
			return nil, ErrMembershipConflict
		}
		if existing.State == "released" {
			return nil, ErrMembershipConflict
		}
		if err := membershipRemainingTx(tx, &existing); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := checkMembershipGroupTx(tx, input.GroupKey); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	if legacy, err := hasLegacyMembershipTx(tx, input.UserID, now); err != nil {
		return nil, err
	} else if legacy {
		return nil, ErrMembershipQuoteChanged
	}
	account, err := membershipAccountTx(tx, input.UserID, now)
	if err != nil {
		return nil, err
	}
	active := account != nil && account.ActiveAt(now)
	if active {
		if account.ID != input.ExpectedAccountID || account.Revision != input.ExpectedRevision {
			return nil, ErrMembershipQuoteChanged
		}
		var current MembershipCatalogState
		if err := lockForShare(tx).First(&current, 1).Error; err != nil {
			return nil, err
		}
		if current.ReleaseID != input.ExpectedReleaseID {
			return nil, ErrMembershipQuoteChanged
		}
	} else if input.ExpectedAccountID != 0 || input.ExpectedRevision != 0 || input.ExpectedReleaseID != 0 {
		return nil, ErrMembershipQuoteChanged
	}
	record := &MembershipBillingRecord{RequestID: input.RequestID, Scope: input.Scope, UserID: input.UserID, TokenID: input.TokenID, Source: MembershipWalletSource, GroupKey: input.GroupKey, Snapshot: input.WalletSnapshot, RequestHash: fingerprint, ReservedQuota: input.WalletQuota, State: "reserved", CreatedAt: now, Applied: true}
	if active && account.Available() >= input.SubscriptionQuota {
		record.Source, record.AccountID, record.AccountRevision, record.Snapshot, record.ReservedQuota = MembershipSource, account.ID, account.Revision, input.SubscriptionSnapshot, input.SubscriptionQuota
		update := tx.Model(&MembershipAccount{}).Where("id = ? AND revision = ? AND amount_total - amount_used - amount_reserved >= ? AND status = ? AND expires_at > ?", account.ID, account.Revision, input.SubscriptionQuota, MembershipActive, now).Update("amount_reserved", gorm.Expr("amount_reserved + ?", input.SubscriptionQuota))
		if update.Error != nil {
			return nil, update.Error
		}
		if update.RowsAffected != 1 {
			return nil, ErrMembershipQuoteChanged
		}
		record.RemainingQuota = account.Available() - record.ReservedQuota
	} else {
		if user.Quota < input.WalletQuota {
			return nil, ErrMembershipInsufficientQuota
		}
		if err := reserveTaskWallet(tx, input.UserID, int(input.WalletQuota)); err != nil {
			return nil, err
		}
		record.RemainingQuota = user.Quota - record.ReservedQuota
	}
	if !input.SkipToken && input.TokenID > 0 && record.ReservedQuota > 0 {
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", input.TokenID, input.UserID).First(&token).Error; err != nil {
			return nil, err
		}
		if input.TokenKey != "" && token.Key != input.TokenKey {
			return nil, ErrMembershipInvalidInput
		}
		if !token.UnlimitedQuota && int64(token.RemainQuota) < record.ReservedQuota {
			return nil, ErrTaskBillingInsufficientToken
		}
		if token.UsedQuota < 0 || int64(token.UsedQuota) > math.MaxInt32-record.ReservedQuota {
			return nil, ErrTaskBillingInsufficientToken
		}
		if !token.UnlimitedQuota {
			token.RemainQuota -= int(record.ReservedQuota)
		}
		token.UsedQuota += int(record.ReservedQuota)
		token.AccessedTime = now
		if err := tx.Save(&token).Error; err != nil {
			return nil, err
		}
		record.TokenReserved = record.ReservedQuota
		record.TokenQuotaDebited = !token.UnlimitedQuota
	}
	if err := tx.Create(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func GetMembershipBillingRecord(requestID string) (*MembershipBillingRecord, error) {
	var record MembershipBillingRecord
	if err := DB.First(&record, "request_id = ?", requestID).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func SettleMembershipBilling(requestID string, actualQuota int64) (*MembershipBillingRecord, error) {
	var result *MembershipBillingRecord
	err := membershipTransaction(func(tx *gorm.DB) error {
		var err error
		result, err = SettleMembershipBillingTx(tx, requestID, actualQuota)
		return err
	})
	if err != nil {
		return nil, err
	}
	MembershipBillingAfterCommit(result)
	return result, nil
}

func ReleaseMembershipBilling(requestID string) (*MembershipBillingRecord, error) {
	var result *MembershipBillingRecord
	err := membershipTransaction(func(tx *gorm.DB) error {
		var err error
		result, err = ReleaseMembershipBillingTx(tx, requestID)
		return err
	})
	if err != nil {
		return nil, err
	}
	MembershipBillingAfterCommit(result)
	return result, nil
}

func SettleMembershipBillingTx(tx *gorm.DB, requestID string, actualQuota int64) (*MembershipBillingRecord, error) {
	return completeMembershipBillingTx(tx, requestID, actualQuota, false)
}

// Release is for confirmed zero-cost failure. Callers must not use it while an
// upstream outcome is unknown; the owning task's recovery state gates this call.
func ReleaseMembershipBillingTx(tx *gorm.DB, requestID string) (*MembershipBillingRecord, error) {
	return completeMembershipBillingTx(tx, requestID, 0, true)
}

func completeMembershipBillingTx(tx *gorm.DB, requestID string, actualQuota int64, release bool) (*MembershipBillingRecord, error) {
	if tx == nil || requestID == "" || actualQuota < 0 {
		return nil, ErrMembershipInvalidInput
	}
	var initial MembershipBillingRecord
	if err := tx.First(&initial, "request_id = ?", requestID).Error; err != nil {
		return nil, err
	}
	_, _, err := lockMembershipUserTx(tx, initial.UserID)
	if err != nil {
		return nil, err
	}
	var record MembershipBillingRecord
	if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
		return nil, err
	}
	if record.State != "reserved" {
		if (release && record.State == "released") || (!release && record.State == "settled" && record.ActualQuota == actualQuota) {
			if err := membershipRemainingTx(tx, &record); err != nil {
				return nil, err
			}
			return &record, nil
		}
		return nil, ErrMembershipConflict
	}
	charged := min(actualQuota, record.ReservedQuota)
	refund := record.ReservedQuota - charged
	if record.Source == MembershipSource {
		var account MembershipAccount
		if err := lockForUpdate(tx).First(&account, record.AccountID).Error; err != nil {
			return nil, err
		}
		if account.UserID != record.UserID || account.AmountReserved < record.ReservedQuota || account.AmountUsed > account.AmountTotal-charged {
			return nil, ErrMembershipConflict
		}
		account.AmountReserved -= record.ReservedQuota
		account.AmountUsed += charged
		if account.Status == MembershipActive {
			if account.ExpiresAt <= common.GetTimestamp() {
				account.Status = MembershipExpired
			} else if account.AmountUsed == account.AmountTotal {
				account.Status = MembershipExhausted
			}
		}
		if err := tx.Save(&account).Error; err != nil {
			return nil, err
		}
		record.RemainingQuota = account.Available()
	} else if record.Source == MembershipWalletSource {
		if refund > 0 {
			if err := increaseUserQuotaWithDB(tx, record.UserID, refund); err != nil {
				return nil, err
			}
		}
		if err := membershipRemainingTx(tx, &record); err != nil {
			return nil, err
		}
	} else {
		return nil, ErrMembershipInvalidInput
	}
	if record.TokenReserved > 0 && refund > 0 {
		var token Token
		err := lockForUpdate(tx).Where("id = ? AND user_id = ?", record.TokenID, record.UserID).First(&token).Error
		// Deleting a token must not prevent the original payer receiving a refund.
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			// Token administrators may reset usage or change the quota while an
			// authorization is in flight. Never let that block the payer refund.
			usedRefund := min(refund, max(0, int64(token.UsedQuota)))
			record.TokenUsedRefundUnapplied = refund - usedRefund
			token.UsedQuota -= int(usedRefund)
			if record.TokenQuotaDebited {
				quotaRefund := min(refund, max(0, int64(math.MaxInt32)-int64(token.RemainQuota)))
				record.TokenRefundUnapplied = refund - quotaRefund
				token.RemainQuota += int(quotaRefund)
			}
			if err := tx.Save(&token).Error; err != nil {
				return nil, err
			}
		}
	}
	record.ActualQuota, record.ChargedQuota, record.ExcessQuota = actualQuota, charged, actualQuota-charged
	record.State, record.SettledAt, record.Applied = "settled", common.GetTimestamp(), true
	if release {
		record.State = "released"
	}
	if err := tx.Save(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func membershipRemainingTx(tx *gorm.DB, record *MembershipBillingRecord) error {
	if record.Source == MembershipSource {
		var account MembershipAccount
		if err := tx.First(&account, record.AccountID).Error; err != nil {
			return err
		}
		record.RemainingQuota = account.Available()
		return nil
	}
	var user User
	if err := tx.Select("id", "quota").First(&user, record.UserID).Error; err != nil {
		return err
	}
	record.RemainingQuota = user.Quota
	return nil
}

func MembershipBillingAfterCommit(record *MembershipBillingRecord) {
	if record == nil || !record.Applied {
		return
	}
	if err := InvalidateUserCache(record.UserID); err != nil {
		common.SysError(fmt.Sprintf("membership user cache invalidation user=%d: %v", record.UserID, err))
	}
	if record.TokenID > 0 {
		var token Token
		if DB.Select("id", "key").First(&token, record.TokenID).Error == nil {
			if err := cacheDeleteToken(token.Key); err != nil {
				common.SysError(fmt.Sprintf("membership token cache invalidation token=%d: %v", record.TokenID, err))
			}
		}
	}
	if record.TokenRefundUnapplied > 0 || record.TokenUsedRefundUnapplied > 0 {
		common.SysError(fmt.Sprintf("membership token refund adjusted after administrative change request=%s token=%d unapplied_quota=%d unapplied_usage=%d", record.RequestID, record.TokenID, record.TokenRefundUnapplied, record.TokenUsedRefundUnapplied))
	}
	if record.ExcessQuota > 0 {
		common.SysError(fmt.Sprintf("membership billing overrun request=%s user=%d source=%s authorized=%d actual=%d charged=%d platform_excess=%d", record.RequestID, record.UserID, record.Source, record.ReservedQuota, record.ActualQuota, record.ChargedQuota, record.ExcessQuota))
	}
}
