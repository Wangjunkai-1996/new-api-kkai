package creditmigration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Nil Before means the key must not exist; a pointer to "" is a present empty
// value. This distinction is retained in receipts for exact restoration.
type OptionChange struct {
	Key    string  `json:"key"`
	Before *string `json:"before"`
	After  string  `json:"after"`
}

type optionValue struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

var creditOptionKeys = map[string]bool{
	"Price": true, "USDExchangeRate": true, "general_setting.quota_display_type": true,
	"DisplayInCurrencyEnabled": true, "payment_setting.amount_discount": true, "payment_setting.amount_options": true,
	"TopupGroupRatio": true, "GroupRatio": true, "GroupGroupRatio": true,
	"group_ratio_setting.group_ratio": true, "group_ratio_setting.group_group_ratio": true,
	"ModelRatio": true, "ModelPrice": true, "CompletionRatio": true, "CacheRatio": true,
	"CacheCreationRatio": true, "CreateCacheRatio": true, "ImageRatio": true, "AudioRatio": true, "AudioCompletionRatio": true,
	"billing_setting.billing_expr": true, "billing_setting.billing_mode": true,
	"SelfUseModeEnabled":        true,
	"tool_price_setting.prices": true, "ImagePricingPolicy": true,
	"QuotaForNewUser": true, "QuotaForInviter": true, "QuotaForInvitee": true, "QuotaRemindThreshold": true,
	"checkin_setting.min_quota": true, "checkin_setting.max_quota": true,
	"MinTopUp": true, "StripeUnitPrice": true, "StripeMinTopUp": true,
	"WaffoUnitPrice": true, "WaffoMinTopUp": true, "WaffoPancakeUnitPrice": true,
	"WaffoPancakeMinTopUp": true, "CreemProducts": true,
}

func pricingOptionValues(config PricingConfig) map[string]string {
	values := map[string]string{
		"Price":                              strconv.FormatFloat(config.Price, 'g', -1, 64),
		"USDExchangeRate":                    strconv.FormatFloat(config.USDExchangeRate, 'g', -1, 64),
		"general_setting.quota_display_type": config.QuotaDisplayType,
	}
	for key, value := range map[string]any{
		"payment_setting.amount_discount": config.AmountDiscount, "TopupGroupRatio": config.TopupGroupRatio,
		"GroupRatio": config.GroupRatio, "GroupGroupRatio": config.GroupGroupRatio,
	} {
		encoded, _ := common.Marshal(value)
		values[key] = string(encoded)
	}
	return values
}

func validatePricingOptions(plan PricingPlan) error {
	byKey := map[string]OptionChange{}
	for _, change := range plan.Options {
		if !creditOptionKeys[change.Key] {
			return fmt.Errorf("%w: unreviewed option %q", ErrPricingInventoryIncomplete, change.Key)
		}
		if _, duplicate := byKey[change.Key]; duplicate {
			return fmt.Errorf("%w: duplicate option %q", ErrPricingInventoryIncomplete, change.Key)
		}
		byKey[change.Key] = change
	}
	oldValues, targetValues := pricingOptionValues(plan.Source), pricingOptionValues(plan.Target)
	for key, target := range targetValues {
		change, exists := byKey[key]
		if !exists || change.Before == nil || !optionValuesEqual(*change.Before, oldValues[key]) || !optionValuesEqual(change.After, target) {
			return fmt.Errorf("%w: option %s does not bind the reviewed source and target", ErrPricingInventoryIncomplete, key)
		}
	}
	for alias, canonical := range map[string]string{"group_ratio_setting.group_ratio": "GroupRatio", "group_ratio_setting.group_group_ratio": "GroupGroupRatio"} {
		if change, exists := byKey[alias]; exists && (!optionValuesEqual(change.After, targetValues[canonical]) || (change.Before != nil && !optionValuesEqual(*change.Before, oldValues[canonical]))) {
			return fmt.Errorf("%w: conflicting registered option %s", ErrPricingInventoryIncomplete, alias)
		}
	}
	if change, exists := byKey["DisplayInCurrencyEnabled"]; exists && change.After != "true" {
		return fmt.Errorf("%w: legacy display must agree with USD", ErrPricingInventoryIncomplete)
	}
	reviewedModels := map[string]map[string]bool{}
	for _, entry := range plan.Models {
		path := entry.Path
		if path == "billing_expr" {
			path = "billing_setting.billing_expr"
		}
		if reviewedModels[path] == nil {
			reviewedModels[path] = map[string]bool{}
		}
		reviewedModels[path][entry.Key] = true
		change, exists := byKey[path]
		if !exists || change.Before == nil {
			return fmt.Errorf("%w: model %s has no bound option %s", ErrPricingInventoryIncomplete, entry.Key, path)
		}
		var before, after map[string]any
		if common.UnmarshalJsonStr(*change.Before, &before) != nil || common.UnmarshalJsonStr(change.After, &after) != nil {
			return fmt.Errorf("%w: model option %s must be a reviewed object", ErrPricingInventoryIncomplete, path)
		}
		oldJSON, _ := common.Marshal(before[entry.Key])
		newJSON, _ := common.Marshal(after[entry.Key])
		// Expressions are JSON strings in options and plain strings in inventory.
		oldValue, newValue := string(oldJSON), string(newJSON)
		if value, ok := before[entry.Key].(string); ok {
			oldValue = value
		}
		if value, ok := after[entry.Key].(string); ok {
			newValue = value
		}
		if !optionValuesEqual(oldValue, entry.OldValue) || !optionValuesEqual(newValue, entry.TargetValue) {
			return fmt.Errorf("%w: model %s option differs from reviewed price", ErrPricingInventoryIncomplete, entry.Key)
		}
	}
	for _, path := range []string{"ModelRatio", "ModelPrice", "billing_setting.billing_expr", "tool_price_setting.prices", "ImagePricingPolicy"} {
		change, exists := byKey[path]
		if !exists {
			continue
		}
		var before, after map[string]any
		if change.Before == nil || common.UnmarshalJsonStr(*change.Before, &before) != nil || common.UnmarshalJsonStr(change.After, &after) != nil {
			return fmt.Errorf("%w: %s requires an explicit pricing object adapter", ErrPricingInventoryIncomplete, path)
		}
		for key := range before {
			if !reviewedModels[path][key] {
				return fmt.Errorf("%w: %s.%s was not individually reviewed", ErrPricingInventoryIncomplete, path, key)
			}
		}
		for key := range after {
			if !reviewedModels[path][key] {
				return fmt.Errorf("%w: new %s.%s was not reviewed", ErrPricingInventoryIncomplete, path, key)
			}
		}
	}
	for _, key := range []string{"QuotaForNewUser", "QuotaForInviter", "QuotaForInvitee", "QuotaRemindThreshold", "checkin_setting.min_quota", "checkin_setting.max_quota"} {
		change, exists := byKey[key]
		if !exists {
			continue
		}
		if change.Before == nil {
			return fmt.Errorf("%w: quota option %s requires a known source value", ErrPricingInventoryIncomplete, key)
		}
		old, err := strconv.ParseInt(*change.Before, 10, 64)
		if err != nil || old < 0 {
			return fmt.Errorf("%w: quota option %s is invalid", ErrPricingInventoryIncomplete, key)
		}
		converted, _ := Convert(old)
		if change.After != strconv.FormatInt(converted, 10) {
			return fmt.Errorf("%w: quota option %s has incorrect conversion", ErrPricingInventoryIncomplete, key)
		}
	}
	return validateMonetaryTransforms(byKey)
}

func optionValuesEqual(a, b string) bool {
	if a == b {
		return true
	}
	var left, right any
	if common.UnmarshalJsonStr(a, &left) != nil || common.UnmarshalJsonStr(b, &right) != nil {
		return false
	}
	x, _ := common.Marshal(left)
	y, _ := common.Marshal(right)
	return string(x) == string(y)
}

func checkOptions(db *gorm.DB, changes []OptionChange, after bool) error {
	for _, change := range changes {
		var current optionValue
		err := db.Table("options").Where(clause.Eq{Column: "key", Value: change.Key}).Take(&current).Error
		want := change.Before
		if after {
			want = &change.After
		}
		if want == nil && errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: option %s cannot be verified: %v", ErrPlanDrift, change.Key, err)
		}
		if want == nil || current.Value != *want {
			return fmt.Errorf("%w: option %s changed", ErrPlanDrift, change.Key)
		}
	}
	return nil
}

func writeOptions(db *gorm.DB, changes []OptionChange, restore bool) error {
	for _, change := range changes {
		value := &change.After
		if restore {
			value = change.Before
		}
		if value == nil {
			if err := db.Table("options").Where(clause.Eq{Column: "key", Value: change.Key}).Delete(&optionValue{}).Error; err != nil {
				return err
			}
		} else if err := db.Table("options").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&optionValue{Key: change.Key, Value: *value}).Error; err != nil {
			return err
		}
	}
	return nil
}

func verifyOptionCoverage(db *gorm.DB, plan PricingPlan) error {
	if len(plan.Options) == 0 {
		return fmt.Errorf("%w: reviewed option before/after values are required", ErrPricingInventoryIncomplete)
	}
	if err := validatePricingOptions(plan); err != nil {
		return err
	}
	var existing []optionValue
	if err := db.Table("options").Find(&existing).Error; err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, change := range plan.Options {
		covered[change.Key] = true
	}
	for _, option := range existing {
		if option.Key == common.CreditEpochOption {
			return fmt.Errorf("%w: a credit cutover already exists", ErrPlanDrift)
		}
		if creditOptionKeys[option.Key] && !covered[option.Key] {
			return fmt.Errorf("%w: stored option %s is not reviewed", ErrPricingInventoryIncomplete, option.Key)
		}
		// Unknown monetary configuration must receive a reviewed adapter, never
		// disappear behind an unrestricted prefix allowlist.
		if !covered[option.Key] && (strings.HasPrefix(option.Key, "billing_setting.") || strings.HasPrefix(option.Key, "tool_price_setting.")) {
			return fmt.Errorf("%w: unknown billing option %s", ErrPricingInventoryIncomplete, option.Key)
		}
	}
	return checkOptions(db, plan.Options, false)
}

func optionReceiptImages(changes []OptionChange) (string, string, error) {
	before, after := map[string]*string{}, map[string]*string{}
	for _, change := range changes {
		before[change.Key] = change.Before
		value := change.After
		after[change.Key] = &value
	}
	b, err := common.Marshal(before)
	if err != nil {
		return "", "", err
	}
	a, err := common.Marshal(after)
	return string(b), string(a), err
}

func receiptOptionChanges(receipt Receipt) ([]OptionChange, error) {
	var before, after map[string]*string
	if err := common.UnmarshalJsonStr(receipt.OptionsBefore, &before); err != nil {
		return nil, err
	}
	if err := common.UnmarshalJsonStr(receipt.OptionsAfter, &after); err != nil {
		return nil, err
	}
	if len(before) != len(after) {
		return nil, ErrInvalidPlan
	}
	changes := make([]OptionChange, 0, len(before))
	for key, value := range before {
		newValue, exists := after[key]
		if !exists || newValue == nil {
			return nil, ErrInvalidPlan
		}
		changes = append(changes, OptionChange{Key: key, Before: value, After: *newValue})
	}
	return changes, nil
}
