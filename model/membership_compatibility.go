package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func hasLegacyMembershipTx(tx *gorm.DB, userID int, now int64) (bool, error) {
	available, err := membershipTableAvailable(tx, "user_subscriptions")
	if err != nil || !available {
		return false, err
	}
	var count int64
	err = tx.Model(&UserSubscription{}).Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now).Count(&count).Error
	return count > 0, err
}

// ShouldUseMembershipBilling preserves existing subscription contracts while the
// new engine is enabled. A contradictory dual entitlement fails closed.
func ShouldUseMembershipBilling(userID int) (bool, error) {
	if !MembershipEnabled() {
		return false, nil
	}
	if userID <= 0 {
		return false, ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return false, err
	}
	return ShouldUseMembershipBillingTx(DB, userID)
}

// ShouldUseMembershipBillingTx checks compatibility in the caller's acceptance
// transaction, without escaping to the global connection or a cache.
func ShouldUseMembershipBillingTx(tx *gorm.DB, userID int) (bool, error) {
	if !MembershipEnabled() {
		return false, nil
	}
	if tx == nil || userID <= 0 {
		return false, ErrMembershipInvalidInput
	}
	now := common.GetTimestamp()
	legacy, err := hasLegacyMembershipTx(tx, userID, now)
	if err != nil {
		return false, err
	}
	if !legacy {
		return true, nil
	}
	account, err := membershipAccountTx(tx, userID, now)
	if err != nil {
		return false, err
	}
	if account != nil && account.ActiveAt(now) {
		return false, fmt.Errorf("%w: conflicting legacy and membership entitlements", ErrMembershipConflict)
	}
	return false, nil
}

// Legacy creation and new member activation share the user row lock, including
// direct admin binding and callbacks for orders created before the cutover.
func checkLegacySubscriptionCreationTx(tx *gorm.DB, userID int) error {
	if !MembershipEnabled() {
		available, err := membershipTableAvailable(tx, "kkai_membership_users")
		if err != nil || !available {
			return err
		}
	}
	_, state, err := lockMembershipUserTx(tx, userID)
	if err != nil {
		return err
	}
	if state.PendingOrderID > 0 {
		var pending MembershipOrder
		if err := tx.First(&pending, state.PendingOrderID).Error; err != nil {
			return err
		}
		if pending.Status == MembershipOrderPending && pending.ExpiresAt > common.GetTimestamp() {
			return fmt.Errorf("%w: close the pending membership checkout first", ErrMembershipConflict)
		}
	}
	account, err := membershipAccountTx(tx, userID, common.GetTimestamp())
	if err != nil {
		return err
	}
	if account != nil && account.ActiveAt(common.GetTimestamp()) {
		return fmt.Errorf("%w: active membership cannot be combined with a legacy subscription", ErrMembershipConflict)
	}
	return nil
}

// syncMembershipGroupKeysTx preserves canonical identities when the ordinary
// group-ratio editor removes a key. Re-adding the same key restores that same
// identity; a new group with the same display name must use a different key.
func syncMembershipGroupKeysTx(tx *gorm.DB, value string) error {
	if !MembershipEnabled() {
		available, err := membershipTableAvailable(tx, "kkai_membership_groups")
		if err != nil || !available {
			return err
		}
	}
	var groups map[string]float64
	if err := common.UnmarshalJsonStr(value, &groups); err != nil {
		return err
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&MembershipCatalogState{ID: 1}).Error; err != nil {
		return err
	}
	var state MembershipCatalogState
	if err := lockForUpdate(tx).First(&state, 1).Error; err != nil {
		return err
	}
	// Include removed pre-catalogue keys so their identities become tombstones too.
	previousGroups := ratio_setting.GetGroupRatioCopy()
	keys := ratio_setting.GetGroupRatioCopy()
	for key := range groups {
		keys[key] = 0
	}
	var known []MembershipGroupRecord
	if err := tx.Find(&known).Error; err != nil {
		return err
	}
	for _, group := range known {
		keys[group.GroupKey] = 0
	}
	now := common.GetTimestamp()
	for key := range keys {
		var record MembershipGroupRecord
		err := tx.First(&record, "group_key = ?", key).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !exists {
			record = MembershipGroupRecord{GroupKey: key, CreatedAt: now}
		}
		_, present := groups[key]
		// A key removed from routing stays disabled. Existing catalogue-disabled
		// keys stay disabled when only their numeric base multiplier changes.
		_, previouslyRoutable := previousGroups[key]
		if !present || !exists || !previouslyRoutable {
			record.Enabled = present
		}
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

// HasTable hides database errors. A second introspection with an error result
// distinguishes an optional legacy schema from a failed database connection.
func membershipTableAvailable(tx *gorm.DB, table string) (bool, error) {
	if tx == nil {
		return false, ErrMembershipSchemaNotReady
	}
	if tx.Migrator().HasTable(table) {
		return true, nil
	}
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	for _, name := range tables {
		if name == table {
			return true, nil
		}
	}
	return false, nil
}
