package creditmigration

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	LegacyPricingEpoch    PricingEpoch = "legacy_075"
	USDPricingEpoch       PricingEpoch = "usd_credit_v1"
	TargetPrice                        = 1.0
	TargetUSDExchangeRate              = 7.0
	TargetQuotaDisplay                 = "USD"
)

type PricingEpoch string

var (
	ErrPricingInventoryIncomplete = errors.New("credit migration pricing inventory is incomplete")
	ErrPricingPlanDrift           = errors.New("credit migration pricing plan drift")
)

// PricingConfig is the part of the runtime configuration whose units change
// during the currency migration. It is separate from the wallet row plan so
// a balance apply can never silently imply that prices were reviewed.
type PricingConfig struct {
	Price              float64                       `json:"price"`
	USDExchangeRate    float64                       `json:"usd_exchange_rate"`
	QuotaDisplayType   string                        `json:"quota_display_type"`
	AmountDiscount     map[int]float64               `json:"amount_discount"`
	TopupGroupRatio    map[string]float64            `json:"topup_group_ratio"`
	GroupRatio         map[string]float64            `json:"group_ratio"`
	ImplicitGroupRatio map[string]float64            `json:"implicit_group_ratio,omitempty"`
	GroupGroupRatio    map[string]map[string]float64 `json:"group_group_ratio"`
}

type PricingEntry struct {
	Key         string `json:"key"`
	Path        string `json:"path"` // ModelRatio, ModelPrice, billing_expr, subscription, payment, ...
	Kind        string `json:"kind"`
	Unit        string `json:"unit"`
	Basis       string `json:"basis"`
	OldValue    string `json:"old_value"`
	TargetValue string `json:"target_value"`
	Evidence    string `json:"evidence"`
	Classified  bool   `json:"classified"`
}

type PricingCompatibility struct {
	Name        string       `json:"name"`
	SourceEpoch PricingEpoch `json:"source_epoch"`
	Strategy    string       `json:"strategy"`
	Handled     bool         `json:"handled"`
}

type PricingInventory struct {
	MigrationID string        `json:"migration_id"`
	SourceEpoch PricingEpoch  `json:"source_epoch"`
	Source      PricingConfig `json:"source"`

	ModelsComplete        bool                   `json:"models_complete"`
	Models                []PricingEntry         `json:"models"`
	SubscriptionsComplete bool                   `json:"subscriptions_complete"`
	Subscriptions         []PricingEntry         `json:"subscriptions"`
	PaymentsComplete      bool                   `json:"payments_complete"`
	Payments              []PricingEntry         `json:"payments"`
	Compatibility         []PricingCompatibility `json:"compatibility"`
	Options               []OptionChange         `json:"options"`
}

type PricingPlan struct {
	MigrationID   string                 `json:"migration_id"`
	SourceEpoch   PricingEpoch           `json:"source_epoch"`
	TargetEpoch   PricingEpoch           `json:"target_epoch"`
	Source        PricingConfig          `json:"source"`
	Target        PricingConfig          `json:"target"`
	Models        []PricingEntry         `json:"models"`
	Subscriptions []PricingEntry         `json:"subscriptions"`
	Payments      []PricingEntry         `json:"payments"`
	Compatibility []PricingCompatibility `json:"compatibility"`
	PlanHash      string                 `json:"plan_hash"`
	Options       []OptionChange         `json:"options"`
}

type PricingApplyEvidence struct {
	PlanHash          string
	MaintenanceLocked bool
	WritesStopped     bool
	BackupReadable    bool
	Observed          PricingConfig
}

func BuildTargetPricingConfig(source PricingConfig) (PricingConfig, error) {
	if err := validatePricingConfig(source); err != nil {
		return PricingConfig{}, err
	}
	target := PricingConfig{
		Price: TargetPrice, USDExchangeRate: TargetUSDExchangeRate, QuotaDisplayType: TargetQuotaDisplay,
		AmountDiscount: map[int]float64{}, TopupGroupRatio: map[string]float64{},
		GroupRatio: map[string]float64{}, GroupGroupRatio: map[string]map[string]float64{},
	}
	for name := range source.TopupGroupRatio {
		target.TopupGroupRatio[name] = 1
	}
	for name, ratio := range source.GroupRatio {
		target.GroupRatio[name] = ratio / 2
	}
	for name, ratio := range source.ImplicitGroupRatio {
		target.GroupRatio[name] = ratio / 2
	}
	for userGroup, ratios := range source.GroupGroupRatio {
		target.GroupGroupRatio[userGroup] = map[string]float64{}
		for usingGroup, ratio := range ratios {
			target.GroupGroupRatio[userGroup][usingGroup] = ratio / 2
		}
	}
	return target, nil
}

func BuildPricingPlan(inventory PricingInventory) (PricingPlan, error) {
	if err := validatePricingInventory(inventory); err != nil {
		return PricingPlan{}, err
	}
	target, err := BuildTargetPricingConfig(inventory.Source)
	if err != nil {
		return PricingPlan{}, err
	}
	plan := PricingPlan{
		MigrationID: inventory.MigrationID, SourceEpoch: inventory.SourceEpoch, TargetEpoch: USDPricingEpoch,
		Source: inventory.Source, Target: target,
		Models:        append([]PricingEntry(nil), inventory.Models...),
		Subscriptions: append([]PricingEntry(nil), inventory.Subscriptions...),
		Payments:      append([]PricingEntry(nil), inventory.Payments...),
		Compatibility: append([]PricingCompatibility(nil), inventory.Compatibility...),
		Options:       append([]OptionChange(nil), inventory.Options...),
	}
	plan.PlanHash = hashPricingPlan(plan)
	return plan, nil
}

func ValidatePricingPlan(plan PricingPlan) error {
	if err := validatePricingInventory(PricingInventory{
		MigrationID: plan.MigrationID, SourceEpoch: plan.SourceEpoch, Source: plan.Source,
		ModelsComplete: true, Models: plan.Models, SubscriptionsComplete: true, Subscriptions: plan.Subscriptions,
		PaymentsComplete: true, Payments: plan.Payments, Compatibility: plan.Compatibility,
	}); err != nil {
		return err
	}
	target, err := BuildTargetPricingConfig(plan.Source)
	if err != nil || !pricingConfigsEqual(target, plan.Target) || plan.TargetEpoch != USDPricingEpoch {
		return fmt.Errorf("%w: target config does not match strict USD migration", ErrPricingPlanDrift)
	}
	if len(plan.Options) > 0 {
		if err := validatePricingOptions(plan); err != nil {
			return err
		}
	}
	if plan.PlanHash == "" || hashPricingPlan(plan) != plan.PlanHash {
		return fmt.Errorf("%w: pricing plan hash is stale", ErrPricingPlanDrift)
	}
	return nil
}

func ValidatePricingApplyGuard(plan PricingPlan, evidence PricingApplyEvidence) error {
	if err := ValidatePricingPlan(plan); err != nil {
		return err
	}
	if evidence.PlanHash != plan.PlanHash || !evidence.MaintenanceLocked || !evidence.WritesStopped || !evidence.BackupReadable {
		return ErrMaintenanceRequired
	}
	if !pricingConfigsEqual(plan.Source, evidence.Observed) {
		return ErrPlanDrift
	}
	return nil
}

func validatePricingInventory(inventory PricingInventory) error {
	if strings.TrimSpace(inventory.MigrationID) == "" || inventory.SourceEpoch != LegacyPricingEpoch {
		return ErrPricingInventoryIncomplete
	}
	if err := validatePricingConfig(inventory.Source); err != nil {
		return err
	}
	if !inventory.ModelsComplete || len(inventory.Models) == 0 {
		return fmt.Errorf("%w: ModelRatio/ModelPrice/billing_expr inventory is incomplete", ErrPricingInventoryIncomplete)
	}
	if err := validatePricingEntries("model", inventory.Models); err != nil {
		return err
	}
	if !inventory.SubscriptionsComplete || len(inventory.Subscriptions) == 0 {
		return fmt.Errorf("%w: subscription inventory is incomplete", ErrPricingInventoryIncomplete)
	}
	if err := validatePricingEntries("subscription", inventory.Subscriptions); err != nil {
		return err
	}
	if !inventory.PaymentsComplete || len(inventory.Payments) == 0 {
		return fmt.Errorf("%w: payment inventory is incomplete", ErrPricingInventoryIncomplete)
	}
	if err := validatePricingEntries("payment", inventory.Payments); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, item := range inventory.Compatibility {
		if seen[item.Name] || strings.TrimSpace(item.Name) == "" || item.SourceEpoch != LegacyPricingEpoch || strings.TrimSpace(item.Strategy) == "" || !item.Handled {
			return fmt.Errorf("%w: compatibility surface %q is incomplete", ErrPricingInventoryIncomplete, item.Name)
		}
		seen[item.Name] = true
	}
	for _, name := range []string{"history_logs", "topup_webhook", "topup_rebate", "kkai_ledger"} {
		if !seen[name] {
			return fmt.Errorf("%w: compatibility surface %q is missing", ErrPricingInventoryIncomplete, name)
		}
	}
	return nil
}

func validatePricingConfig(config PricingConfig) error {
	for group, ratio := range config.ImplicitGroupRatio {
		if _, explicit := config.GroupRatio[group]; explicit || strings.TrimSpace(group) == "" || ratio != 1 {
			return fmt.Errorf("%w: implicit group %q must bind the existing runtime fallback of 1", ErrPricingInventoryIncomplete, group)
		}
	}
	switch config.QuotaDisplayType {
	case "USD", "CNY", "TOKENS", "CUSTOM":
	default:
		return fmt.Errorf("%w: quota display type is missing or unknown", ErrPricingInventoryIncomplete)
	}
	for name, value := range map[string]float64{"Price": config.Price, "USDExchangeRate": config.USDExchangeRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("%w: %s is invalid", ErrPricingInventoryIncomplete, name)
		}
	}
	for amount, discount := range config.AmountDiscount {
		if amount <= 0 || math.IsNaN(discount) || math.IsInf(discount, 0) || discount <= 0 || discount > 1 {
			return fmt.Errorf("%w: amount discount %d is invalid", ErrPricingInventoryIncomplete, amount)
		}
	}
	for _, ratios := range []map[string]float64{config.TopupGroupRatio, config.GroupRatio} {
		for group, ratio := range ratios {
			if strings.TrimSpace(group) == "" || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
				return fmt.Errorf("%w: group ratio %q is invalid", ErrPricingInventoryIncomplete, group)
			}
		}
	}
	for userGroup, ratios := range config.GroupGroupRatio {
		for usingGroup, ratio := range ratios {
			if strings.TrimSpace(userGroup) == "" || strings.TrimSpace(usingGroup) == "" || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
				return fmt.Errorf("%w: special group ratio %q/%q is invalid", ErrPricingInventoryIncomplete, userGroup, usingGroup)
			}
		}
	}
	return nil
}

func validatePricingEntries(kind string, entries []PricingEntry) error {
	seen := map[string]bool{}
	for _, item := range entries {
		key := item.Key + "\x00" + item.Path
		if seen[key] || strings.TrimSpace(item.Key) == "" || (kind == "model" && strings.TrimSpace(item.Path) == "") || !validPricingKind(kind, item.Kind) || strings.TrimSpace(item.Unit) == "" || !validPricingBasis(item.Basis) || strings.TrimSpace(item.OldValue) == "" || strings.TrimSpace(item.TargetValue) == "" || strings.TrimSpace(item.Evidence) == "" || !item.Classified {
			return fmt.Errorf("%w: unclassified or duplicate %s pricing item %q", ErrPricingInventoryIncomplete, kind, key)
		}
		seen[key] = true
	}
	return nil
}

func validPricingKind(inventoryKind, itemKind string) bool {
	switch inventoryKind {
	case "model":
		for _, allowed := range []string{"ratio", "fixed", "expression", "tool", "image", "video", "plugin"} {
			if itemKind == allowed {
				return true
			}
		}
	case "subscription":
		return itemKind == "subscription"
	case "payment":
		return itemKind == "payment"
	}
	return false
}

func validPricingBasis(basis string) bool {
	switch basis {
	case "divide_by_7", "keep", "manual", "manual_review":
		return true
	default:
		return false
	}
}

func pricingConfigsEqual(left, right PricingConfig) bool {
	if len(left.ImplicitGroupRatio) != len(right.ImplicitGroupRatio) {
		return false
	}
	for group, ratio := range left.ImplicitGroupRatio {
		if right.ImplicitGroupRatio[group] != ratio {
			return false
		}
	}
	if left.Price != right.Price || left.USDExchangeRate != right.USDExchangeRate || left.QuotaDisplayType != right.QuotaDisplayType || len(left.AmountDiscount) != len(right.AmountDiscount) || len(left.TopupGroupRatio) != len(right.TopupGroupRatio) || len(left.GroupRatio) != len(right.GroupRatio) || len(left.GroupGroupRatio) != len(right.GroupGroupRatio) {
		return false
	}
	for key, value := range left.AmountDiscount {
		if right.AmountDiscount[key] != value {
			return false
		}
	}
	for key, value := range left.TopupGroupRatio {
		if right.TopupGroupRatio[key] != value {
			return false
		}
	}
	for key, value := range left.GroupRatio {
		if right.GroupRatio[key] != value {
			return false
		}
	}
	for key, values := range left.GroupGroupRatio {
		if len(values) != len(right.GroupGroupRatio[key]) {
			return false
		}
		for using, value := range values {
			if right.GroupGroupRatio[key][using] != value {
				return false
			}
		}
	}
	return true
}

func hashPricingPlan(plan PricingPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s|%s\n", plan.MigrationID, plan.SourceEpoch, plan.TargetEpoch, strconv.FormatFloat(plan.Source.Price, 'g', -1, 64), strconv.FormatFloat(plan.Target.Price, 'g', -1, 64))
	writePricingConfig(&b, "source", plan.Source)
	writePricingConfig(&b, "target", plan.Target)
	writePricingEntries(&b, "model", plan.Models)
	writePricingEntries(&b, "subscription", plan.Subscriptions)
	writePricingEntries(&b, "payment", plan.Payments)
	for _, item := range sortedCompatibility(plan.Compatibility) {
		fmt.Fprintf(&b, "compat|%s|%s|%s|%t\n", item.Name, item.SourceEpoch, item.Strategy, item.Handled)
	}
	optionsJSON, _ := Marshal(plan.Options)
	b.Write(optionsJSON)
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func writePricingConfig(b *strings.Builder, prefix string, config PricingConfig) {
	fmt.Fprintf(b, "%s|rate=%s|display=%s\n", prefix, strconv.FormatFloat(config.USDExchangeRate, 'g', -1, 64), config.QuotaDisplayType)
	amounts := make([]int, 0, len(config.AmountDiscount))
	for amount := range config.AmountDiscount {
		amounts = append(amounts, amount)
	}
	sort.Ints(amounts)
	for _, amount := range amounts {
		fmt.Fprintf(b, "%s|discount|%d=%s\n", prefix, amount, strconv.FormatFloat(config.AmountDiscount[amount], 'g', -1, 64))
	}
	writeRatioMap(b, prefix+"|topup", config.TopupGroupRatio)
	writeRatioMap(b, prefix+"|group", config.GroupRatio)
	writeRatioMap(b, prefix+"|implicit_group", config.ImplicitGroupRatio)
	users := make([]string, 0, len(config.GroupGroupRatio))
	for user := range config.GroupGroupRatio {
		users = append(users, user)
	}
	sort.Strings(users)
	for _, user := range users {
		writeRatioMap(b, prefix+"|special|"+user, config.GroupGroupRatio[user])
	}
}

func writeRatioMap(b *strings.Builder, prefix string, values map[string]float64) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "%s|%s=%s\n", prefix, key, strconv.FormatFloat(values[key], 'g', -1, 64))
	}
}

func writePricingEntries(b *strings.Builder, kind string, entries []PricingEntry) {
	copyEntries := append([]PricingEntry(nil), entries...)
	sort.Slice(copyEntries, func(i, j int) bool {
		return copyEntries[i].Key+copyEntries[i].Path < copyEntries[j].Key+copyEntries[j].Path
	})
	for _, item := range copyEntries {
		fmt.Fprintf(b, "%s|%s|%s|%s|%s|%s|%s|%s|%t\n", kind, item.Key, item.Path, item.Kind, item.Unit, item.Basis, item.OldValue, item.TargetValue, item.Classified)
	}
}

func sortedCompatibility(entries []PricingCompatibility) []PricingCompatibility {
	result := append([]PricingCompatibility(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
