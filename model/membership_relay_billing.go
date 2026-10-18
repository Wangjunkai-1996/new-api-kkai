package model

import "gorm.io/gorm"

// SettleMembershipRelayBilling commits synchronous request statistics together
// with the funding ledger. Task and Image Studio keep their existing accounting
// transactions and therefore call SettleMembershipBillingTx directly instead.
func SettleMembershipRelayBilling(requestID string, actualQuota int64, channelID int, billableUsage bool) (*MembershipBillingRecord, error) {
	var record *MembershipBillingRecord
	err := membershipTransaction(func(tx *gorm.DB) error {
		var err error
		record, err = settleMembershipRelayBillingTx(tx, requestID, actualQuota, channelID, billableUsage)
		return err
	})
	if err != nil {
		return nil, err
	}
	MembershipBillingAfterCommit(record)
	return record, nil
}

func settleMembershipRelayBillingTx(tx *gorm.DB, requestID string, actualQuota int64, channelID int, billableUsage bool) (*MembershipBillingRecord, error) {
	record, err := SettleMembershipBillingTx(tx, requestID, actualQuota)
	if err != nil {
		return nil, err
	}
	if record.Scope != MembershipBillingScopeRelay {
		return nil, ErrMembershipConflict
	}
	if !record.Applied || !billableUsage {
		return record, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", record.UserID).Updates(map[string]any{"used_quota": gorm.Expr("used_quota + ?", record.ChargedQuota), "request_count": gorm.Expr("request_count + ?", 1)}).Error; err != nil {
		return nil, err
	}
	if channelID > 0 && record.ChargedQuota > 0 {
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Update("used_quota", gorm.Expr("used_quota + ?", record.ChargedQuota)).Error; err != nil {
			return nil, err
		}
	}
	return record, nil
}
