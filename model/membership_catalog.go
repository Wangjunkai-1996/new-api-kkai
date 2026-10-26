package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func membershipCatalogTx(tx *gorm.DB, releaseID int64) (*MembershipCatalog, error) {
	if releaseID == 0 {
		return &MembershipCatalog{Tiers: []MembershipTier{}, Groups: []MembershipGroup{}}, nil
	}
	var release MembershipPricingRelease
	if err := tx.First(&release, releaseID).Error; err != nil {
		return nil, err
	}
	var catalog MembershipCatalog
	if err := common.UnmarshalJsonStr(release.Payload, &catalog); err != nil {
		return nil, err
	}
	catalog.ReleaseID = release.ID
	return &catalog, nil
}

func currentMembershipCatalogTx(tx *gorm.DB) (*MembershipCatalog, error) {
	var state MembershipCatalogState
	err := tx.First(&state, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	catalog, err := membershipCatalogTx(tx, state.ReleaseID)
	if err != nil {
		return nil, err
	}
	catalog.SalesEnabled = state.SalesEnabled
	catalog.TopupDiscountsDisabled = state.TopupDiscountsDisabled
	return catalog, nil
}

func GetMembershipCatalog() (*MembershipCatalog, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	catalog, err := currentMembershipCatalogTx(DB)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(catalog.Groups))
	current := ratio_setting.GetGroupRatioCopy()
	for i := range catalog.Groups {
		group := &catalog.Groups[i]
		known[group.Key] = true
		_, exists := current[group.Key]
		group.Enabled = group.Enabled && exists
		group.DisplayName = setting.GetGroupDisplayName(group.Key)
	}
	for key := range current {
		if !known[key] {
			catalog.Groups = append(catalog.Groups, MembershipGroup{Key: key, DisplayName: setting.GetGroupDisplayName(key), Enabled: true})
		}
	}
	sort.Slice(catalog.Groups, func(i, j int) bool { return catalog.Groups[i].Key < catalog.Groups[j].Key })
	return catalog, nil
}

func PublishMembershipCatalog(input MembershipCatalog, expectedReleaseID int64, actorID int) (*MembershipCatalog, error) {
	if actorID <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	// Copy maps before materializing omitted coefficients; never mutate a caller's draft.
	encoded, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	var catalog MembershipCatalog
	if err := common.Unmarshal(encoded, &catalog); err != nil {
		return nil, err
	}
	if err := ValidateMembershipCatalog(&catalog); err != nil {
		return nil, err
	}
	err = membershipTransaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&MembershipCatalogState{ID: 1}).Error; err != nil {
			return err
		}
		var state MembershipCatalogState
		if err := lockForUpdate(tx).First(&state, 1).Error; err != nil {
			return err
		}
		if state.ReleaseID != expectedReleaseID {
			return ErrMembershipConflict
		}
		now := common.GetTimestamp()
		retained := map[string]bool{}
		var accounts []MembershipAccount
		if err := tx.Where("status = ? AND expires_at > ? AND amount_used < amount_total", MembershipActive, now).Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			retained[account.TierID] = true
		}
		var pending []MembershipOrder
		if err := tx.Where("status = ? AND expires_at > ?", MembershipOrderPending, now).Find(&pending).Error; err != nil {
			return err
		}
		for _, order := range pending {
			retained[order.TierID] = true
		}
		for _, tier := range catalog.Tiers {
			delete(retained, tier.ID)
		}
		if len(retained) != 0 {
			return fmt.Errorf("%w: tiers referenced by active memberships or pending orders must be retained", ErrMembershipInvalidInput)
		}
		catalog.TopupDiscountsDisabled = state.TopupDiscountsDisabled || catalog.TopupDiscountsDisabled || catalog.SalesEnabled
		catalog.ReleaseID = 0
		payload, err := common.Marshal(catalog)
		if err != nil {
			return err
		}
		release := MembershipPricingRelease{Payload: string(payload), ActorID: actorID, CreatedAt: now}
		if err := tx.Create(&release).Error; err != nil {
			return err
		}
		for _, group := range catalog.Groups {
			var record MembershipGroupRecord
			err := tx.Where("group_key = ?", group.Key).First(&record).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				record = MembershipGroupRecord{GroupKey: group.Key, CreatedAt: now}
			} else if err != nil {
				return err
			}
			if record.FirstReleaseID != 0 && !group.Published {
				return fmt.Errorf("%w: group %s cannot unpublish existing terms", ErrMembershipInvalidInput, group.Key)
			}
			if group.Published && record.FirstReleaseID == 0 {
				record.FirstReleaseID = release.ID
			}
			record.Enabled, record.UpdatedAt = group.Enabled, now
			if err := tx.Save(&record).Error; err != nil {
				return err
			}
		}
		// Removed keys are tombstones, never deleted or assigned a fresh identity.
		keys := make([]string, 0, len(catalog.Groups))
		for _, group := range catalog.Groups {
			keys = append(keys, group.Key)
		}
		query := tx.Model(&MembershipGroupRecord{}).Where("1 = 1")
		if len(keys) > 0 {
			query = query.Where("group_key NOT IN ?", keys)
		}
		if err := query.Updates(map[string]any{"enabled": false, "updated_at": now}).Error; err != nil {
			return err
		}
		updated := tx.Model(&MembershipCatalogState{}).Where("id = ? AND release_id = ?", 1, expectedReleaseID).Updates(map[string]any{"release_id": release.ID, "sales_enabled": catalog.SalesEnabled, "topup_discounts_disabled": catalog.TopupDiscountsDisabled})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrMembershipConflict
		}
		catalog.ReleaseID = release.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &catalog, nil
}

func ValidateMembershipCatalog(catalog *MembershipCatalog) error {
	if catalog == nil {
		return ErrMembershipInvalidInput
	}
	for name, limit := range catalog.ModelLimits {
		if strings.TrimSpace(name) == "" || len(name) > 256 || limit.MaxInputTokens < 0 || limit.MaxInputTokens > math.MaxInt32 || limit.MaxOutputTokens < 0 || limit.MaxOutputTokens > math.MaxInt32 || limit.MaxToolCalls < 0 || limit.MaxToolCalls > math.MaxInt32 || (limit.MaxInputTokens == 0 && limit.MaxOutputTokens == 0 && limit.MaxToolCalls == 0 && len(limit.TaskUsageLimits) == 0) || strings.TrimSpace(limit.Evidence) == "" || len(limit.Evidence) > 2048 {
			return fmt.Errorf("%w: model %s requires finite request-wide bounds and their evidence", ErrMembershipInvalidInput, name)
		}
		for key, maximum := range limit.TaskUsageLimits {
			if strings.TrimSpace(key) == "" || len(key) > 256 || math.IsNaN(maximum) || math.IsInf(maximum, 0) || maximum < 0 || maximum > math.MaxInt32 {
				return fmt.Errorf("%w: model %s task fact %s requires a finite nonnegative bound", ErrMembershipInvalidInput, name, key)
			}
		}
	}
	tiers, ranks, groups := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, group := range catalog.Groups {
		if strings.TrimSpace(group.Key) == "" || len(group.Key) > 64 || groups[group.Key] {
			return fmt.Errorf("%w: invalid or duplicate group %s", ErrMembershipInvalidInput, group.Key)
		}
		if group.Enabled && !ratio_setting.ContainsGroupRatio(group.Key) {
			return fmt.Errorf("%w: unknown billing group %s", ErrMembershipInvalidInput, group.Key)
		}
		groups[group.Key] = true
	}
	enabled := 0
	for i := range catalog.Tiers {
		tier := &catalog.Tiers[i]
		if strings.TrimSpace(tier.ID) == "" || len(tier.ID) > 64 || tiers[tier.ID] || tier.Rank <= 0 || ranks[tier.Rank] || strings.TrimSpace(tier.Name) == "" {
			return fmt.Errorf("%w: invalid tier %s", ErrMembershipInvalidInput, tier.ID)
		}
		tiers[tier.ID], ranks[tier.Rank] = true, true
		tier.Currency = strings.ToUpper(strings.TrimSpace(tier.Currency))
		if (tier.Currency != "USD" && tier.Currency != "CNY") || tier.PriceMinor < 0 || tier.Quota < 0 || tier.PriceMinor > 999_999_999 {
			return fmt.Errorf("%w: invalid price or quota for %s", ErrMembershipInvalidInput, tier.ID)
		}
		if tier.Enabled {
			enabled++
			if tier.PriceMinor <= 0 || tier.Quota <= 0 {
				return fmt.Errorf("%w: enabled tier %s needs a positive price and quota", ErrMembershipInvalidInput, tier.ID)
			}
		}
		if tier.Discounts == nil {
			tier.Discounts = map[string]float64{}
		}
		for key, coefficient := range tier.Discounts {
			if !groups[key] || math.IsNaN(coefficient) || math.IsInf(coefficient, 0) || coefficient <= 0 || coefficient > 1 {
				return fmt.Errorf("%w: invalid coefficient for %s / %s", ErrMembershipInvalidInput, key, tier.ID)
			}
		}
		for _, group := range catalog.Groups {
			if _, ok := tier.Discounts[group.Key]; !ok {
				tier.Discounts[group.Key] = 1
			}
		}
	}
	if catalog.SalesEnabled && enabled == 0 {
		return fmt.Errorf("%w: no enabled membership tier", ErrMembershipInvalidInput)
	}
	sort.Slice(catalog.Tiers, func(i, j int) bool { return catalog.Tiers[i].Rank < catalog.Tiers[j].Rank })
	for _, group := range catalog.Groups {
		previous := 1.0
		for _, tier := range catalog.Tiers {
			coefficient := tier.Discounts[group.Key]
			if coefficient > previous {
				return fmt.Errorf("%w: group %s, tier %s is more expensive than the lower tier", ErrMembershipInvalidInput, group.Key, tier.ID)
			}
			previous = coefficient
		}
	}
	return nil
}

func membershipAccountTx(tx *gorm.DB, userID int, now int64) (*MembershipAccount, error) {
	var state MembershipUserState
	if err := tx.First(&state, "user_id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if state.CurrentAccountID == 0 {
		return nil, nil
	}
	var account MembershipAccount
	if err := tx.First(&account, state.CurrentAccountID).Error; err != nil {
		return nil, err
	}
	if account.UserID != userID {
		return nil, ErrMembershipConflict
	}
	if account.Status == MembershipActive {
		if account.ExpiresAt <= now {
			account.Status = MembershipExpired
		} else if account.AmountUsed >= account.AmountTotal {
			account.Status = MembershipExhausted
		}
	}
	return &account, nil
}

// MembershipDiscount resolves a tier's discount from one published catalog.
func MembershipDiscount(catalog *MembershipCatalog, tierID, groupKey string) (float64, error) {
	published := false
	for _, group := range catalog.Groups {
		if group.Key == groupKey {
			published = group.Published
			break
		}
	}
	for _, tier := range catalog.Tiers {
		if tier.ID == tierID {
			if d, ok := tier.Discounts[groupKey]; ok && published {
				return d, nil
			}
			return 1, nil
		}
	}
	return 0, fmt.Errorf("%w: tier missing from current pricing release", ErrMembershipConflict)
}

// ResolveMembershipRatio preserves an existing group price when it is cheaper.
// A coefficient of one grants no extra benefit and keeps the wallet price.
func ResolveMembershipRatio(baseRatio, userRatio, coefficient float64) float64 {
	if coefficient < 1 {
		return math.Min(userRatio, baseRatio*coefficient)
	}
	return userRatio
}

func GetMembershipQuote(userID int, groupKey string, baseRatio, userRatio float64) (*MembershipQuote, error) {
	if userID <= 0 || groupKey == "" || math.IsNaN(baseRatio) || math.IsInf(baseRatio, 0) || baseRatio < 0 || math.IsNaN(userRatio) || math.IsInf(userRatio, 0) || userRatio < 0 {
		return nil, ErrMembershipInvalidInput
	}
	quote := &MembershipQuote{Discount: 1, MembershipRatio: userRatio, WalletRatio: userRatio}
	if !MembershipEnabled() {
		return quote, nil
	}
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	if err := checkMembershipGroupTx(DB, groupKey); err != nil {
		return nil, err
	}
	account, err := membershipAccountTx(DB, userID, common.GetTimestamp())
	if err != nil {
		return nil, err
	}
	if account == nil || !account.ActiveAt(common.GetTimestamp()) {
		return quote, nil
	}
	catalog, err := currentMembershipCatalogTx(DB)
	if err != nil {
		return nil, err
	}
	quote.AccountID, quote.Revision, quote.ReleaseID, quote.TierID, quote.Active = account.ID, account.Revision, catalog.ReleaseID, account.TierID, true
	quote.Discount, err = MembershipDiscount(catalog, account.TierID, groupKey)
	if err != nil {
		return nil, err
	}
	for _, tier := range catalog.Tiers {
		if tier.ID == account.TierID {
			quote.TierName = tier.Name
			quote.TierStyle = tier.Style
			break
		}
	}
	quote.MembershipRatio = ResolveMembershipRatio(baseRatio, userRatio, quote.Discount)
	return quote, nil
}

func GetMembershipSelf(userID int) (*MembershipSelf, error) {
	if userID <= 0 {
		return nil, ErrMembershipInvalidInput
	}
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	account, err := membershipAccountTx(DB, userID, now)
	if err != nil {
		return nil, err
	}
	result := &MembershipSelf{ServerTime: now, Account: account, Discounts: map[string]float64{}}
	if account == nil {
		return result, nil
	}
	catalog, err := membershipCatalogTx(DB, account.ReleaseID)
	if err != nil {
		return nil, err
	}
	for i := range catalog.Tiers {
		if catalog.Tiers[i].ID == account.TierID {
			tier := catalog.Tiers[i]
			result.Tier = &tier
			break
		}
	}
	if account.ActiveAt(now) {
		catalog, err = currentMembershipCatalogTx(DB)
		if err != nil {
			return nil, err
		}
	}
	for key := range ratio_setting.GetGroupRatioCopy() {
		d := 1.0
		if account.ActiveAt(now) {
			d, err = MembershipDiscount(catalog, account.TierID, key)
			if err != nil {
				return nil, err
			}
		}
		result.Discounts[key] = d
	}
	if result.Tier != nil {
		result.Tier.Discounts = result.Discounts
	}
	return result, nil
}

func MembershipTopupDiscountsDisabled() bool {
	if DB == nil {
		return false
	}
	// A failed catalogue read must never reinstate previously removed discounts.
	if err := CheckMembershipSchema(); err != nil {
		if !MembershipEnabled() {
			available, inspectErr := membershipTableAvailable(DB, "kkai_membership_catalog")
			if inspectErr == nil && !available {
				return false
			}
		}
		common.SysError("membership top-up discount state unavailable: " + err.Error())
		return true
	}
	var state MembershipCatalogState
	err := DB.First(&state, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		common.SysError("membership top-up discount state read: " + err.Error())
		return true
	}
	return state.TopupDiscountsDisabled
}

// checkMembershipGroupTx rejects disabled routing identities before accepting a
// new authorization. Accepted requests still settle their original snapshots.
func checkMembershipGroupTx(tx *gorm.DB, groupKey string) error {
	if !ratio_setting.ContainsGroupRatio(groupKey) {
		return fmt.Errorf("%w: billing group is unavailable", ErrMembershipInvalidInput)
	}
	var group MembershipGroupRecord
	err := tx.First(&group, "group_key = ?", groupKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !group.Enabled {
		return fmt.Errorf("%w: billing group is disabled", ErrMembershipInvalidInput)
	}
	return nil
}
