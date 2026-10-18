package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipMaximumBoundsAllTierBranches(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		want             float64
	}{
		{"lower usage can be more expensive", `p < 50 ? tier("small", p*100+c*20) : tier("large",p+c)`, 12000},
		{"decreasing price", `tier("base",1000-p)`, 1000},
		{"fixed branch", `p < 50 ? tier("once",fixed(0.2)) : tier("tokens",p+c)`, 200000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &BillingSnapshot{ExprString: tc.expression, ExprVersion: 1, QuotaPerUnit: 1_000_000}
			bound, err := MaximumTokenQuota(snapshot, TokenParams{P: 100, C: 100, Len: 100}, 1)
			require.NoError(t, err)
			assert.Equal(t, tc.want, bound)
		})
	}
}

func TestMembershipTaskMaximumUsesVerifiedWorkloadBounds(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		bounds           map[string]UsageBound
		maximum          float64
		fail             bool
	}{
		{"duration and output count", `tier("video", u("seconds") * u("image_count") * 0.2)`, map[string]UsageBound{"seconds": {Maximum: 10}, "image_count": {Maximum: 3}}, 3000000, false},
		{"shorter result can cost more", `tier("video", 100-u("seconds"))`, map[string]UsageBound{"seconds": {Maximum: 10}}, 50000000, false},
		{"all enum branches", `u("quality") == "high" ? tier("high", u("seconds")*2) : tier("low", u("seconds"))`, map[string]UsageBound{"seconds": {Maximum: 10}}, 10000000, false},
		{"unbounded token usage", `tier("tokens", u("total_tokens")*0.1)`, nil, 0, true},
		{"dynamic usage key", `u("cost_" + u("quality"))`, nil, 0, true},
		{"zero denominator", `10/u("seconds")`, map[string]UsageBound{"seconds": {Maximum: 10}}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &BillingSnapshot{TaskUsageBilling: true, ExprString: tc.expression, QuotaPerUnit: 500000}
			maximum, err := MaximumTaskQuota(snapshot, tc.bounds)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.maximum, maximum)
		})
	}
}

func TestMembershipRequestPricingPreservesIntegerTimePredicates(t *testing.T) {
	expression := `hour("UTC") % 2 >= 0 ? tier("valid",p*2) : tier("invalid",p*3)`
	frozen, err := FreezeRequestPricing(expression, RequestInput{})
	require.NoError(t, err)
	assert.NotContains(t, frozen, "hour")
	cost, _, err := RunExpr(frozen, TokenParams{P: 100})
	require.NoError(t, err)
	assert.Equal(t, 200.0, cost)
}

func TestMembershipMaximumRejectsUnboundedDenominator(t *testing.T) {
	snapshot := &BillingSnapshot{ExprString: `tier("base", 100/(p-10))`, ExprVersion: 1, QuotaPerUnit: 1_000_000}
	_, err := MaximumTokenQuota(snapshot, TokenParams{P: 100}, 1)
	require.ErrorContains(t, err, "zero")
}

func TestMembershipRequestPricingFreezesPredicateWithoutPersistingSecret(t *testing.T) {
	original := `tier("base",p*3+c*15)*(header("x-plan") == "private-plan-value" ? 2 : 1)`
	frozen, err := FreezeRequestPricing(original, RequestInput{Headers: map[string]string{"x-plan": "private-plan-value"}})
	require.NoError(t, err)
	assert.NotContains(t, frozen, "private-plan-value")
	assert.NotContains(t, frozen, "header")
	cost, _, err := RunExprWithRequest(frozen, TokenParams{P: 100, C: 20}, RequestInput{Headers: map[string]string{"x-plan": "changed"}})
	require.NoError(t, err)
	assert.Equal(t, 1200.0, cost)
}

func TestMembershipRequestPricingRetainsTokenTierSelection(t *testing.T) {
	expression := `len > 100 && param("fast") == true ? tier("fast",p*6) : tier("normal",p*3)`
	frozen, err := FreezeRequestPricing(expression, RequestInput{Body: []byte(`{"fast":true,"secret":"not-for-ledger"}`)})
	require.NoError(t, err)
	assert.NotContains(t, frozen, "param")
	assert.NotContains(t, frozen, "not-for-ledger")
	for _, tc := range []struct{ tokens, want float64 }{{50, 150}, {200, 1200}} {
		cost, _, err := RunExpr(frozen, TokenParams{P: tc.tokens, Len: tc.tokens})
		require.NoError(t, err)
		assert.Equal(t, tc.want, cost)
	}
}
