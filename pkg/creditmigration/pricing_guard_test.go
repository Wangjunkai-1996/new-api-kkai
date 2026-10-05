package creditmigration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingInventoryFixture() PricingInventory {
	return PricingInventory{
		MigrationID: "pricing-migration-test", SourceEpoch: LegacyPricingEpoch,
		Source: PricingConfig{
			Price: 0.075, USDExchangeRate: 1, QuotaDisplayType: "CNY",
			AmountDiscount: map[int]float64{1000: 0.97}, TopupGroupRatio: map[string]float64{"default": 1, "vip": 1},
			GroupRatio:      map[string]float64{"default": 0.4, "enterprise": 0.6},
			GroupGroupRatio: map[string]map[string]float64{"vip": {"enterprise": 0.8}},
		},
		ModelsComplete: true, Models: []PricingEntry{
			{Key: "claude-sonnet", Path: "ModelRatio", Kind: "ratio", Unit: "usd_per_1m_tokens", Basis: "divide_by_7", OldValue: "10.5", TargetValue: "1.5", Evidence: "model price inventory", Classified: true},
			{Key: "gpt-task", Path: "billing_expr", Kind: "expression", Unit: "usd_per_1m_tokens", Basis: "manual", OldValue: "p*21+c*105", TargetValue: "p * 3 + c * 15", Evidence: "expression vector", Classified: true},
		},
		SubscriptionsComplete: true, Subscriptions: []PricingEntry{{Key: "pro", Kind: "subscription", Unit: "usd_credit", Basis: "manual", OldValue: "1000", TargetValue: "75", Evidence: "plan review", Classified: true}},
		PaymentsComplete: true, Payments: []PricingEntry{{Key: "epay", Kind: "payment", Unit: "cny_per_usd_credit", Basis: "manual", OldValue: "0.075", TargetValue: "1", Evidence: "gateway review", Classified: true}},
		Compatibility: []PricingCompatibility{
			{Name: "history_logs", SourceEpoch: LegacyPricingEpoch, Strategy: "retain raw and normalize in view", Handled: true},
			{Name: "topup_webhook", SourceEpoch: LegacyPricingEpoch, Strategy: "source order epoch", Handled: true},
			{Name: "topup_rebate", SourceEpoch: LegacyPricingEpoch, Strategy: "idempotent mapped delta", Handled: true},
			{Name: "kkai_ledger", SourceEpoch: LegacyPricingEpoch, Strategy: "preserve immutable record", Handled: true},
		},
	}
}

func TestBuildTargetPricingConfig(t *testing.T) {
	source := pricingInventoryFixture().Source
	source.ImplicitGroupRatio = map[string]float64{"enabled-legacy-group": 1}
	target, err := BuildTargetPricingConfig(source)
	require.NoError(t, err)
	assert.Equal(t, TargetPrice, target.Price)
	assert.Equal(t, TargetUSDExchangeRate, target.USDExchangeRate)
	assert.Equal(t, TargetQuotaDisplay, target.QuotaDisplayType)
	assert.Empty(t, target.AmountDiscount)
	assert.Equal(t, 0.2, target.GroupRatio["default"])
	assert.Equal(t, 0.5, target.GroupRatio["enabled-legacy-group"])
	assert.Equal(t, 0.3, target.GroupRatio["enterprise"])
	assert.Equal(t, 0.4, target.GroupGroupRatio["vip"]["enterprise"])
	assert.Equal(t, 1.0, target.TopupGroupRatio["default"])
}

func TestPricingPlanBlocksUnclassifiedAndApplyRequiresEvidence(t *testing.T) {
	plan, err := BuildPricingPlan(pricingInventoryFixture())
	require.NoError(t, err)
	require.NoError(t, ValidatePricingPlan(plan))
	evidence := PricingApplyEvidence{PlanHash: plan.PlanHash, MaintenanceLocked: true, WritesStopped: true, BackupReadable: true, Observed: plan.Source}
	require.NoError(t, ValidatePricingApplyGuard(plan, evidence))

	evidence.Observed.Price = 0.076
	assert.ErrorIs(t, ValidatePricingApplyGuard(plan, evidence), ErrPlanDrift)

	bad := pricingInventoryFixture()
	bad.Models[0].Classified = false
	_, err = BuildPricingPlan(bad)
	assert.ErrorIs(t, err, ErrPricingInventoryIncomplete)

	bad = pricingInventoryFixture()
	bad.Compatibility = bad.Compatibility[:3]
	_, err = BuildPricingPlan(bad)
	assert.ErrorIs(t, err, ErrPricingInventoryIncomplete)
}
