package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDisplayBillingExpr(t *testing.T) {
	const actual = `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 2) : tier("long", p * 20 + c * 100 + cr * 2)`
	for _, test := range []struct {
		name, actual, display string
		valid                 bool
	}{
		{"different tier prices", actual, `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1) : tier("long", p * 20 + c * 100 + cr * 3)`, true},
		{"explicit zero", `tier("base", p * 10 + cr * 2)`, `tier("base", p * 10 + cr * 0)`, true},
		{"zero actual", `tier("base", p * 10 + cr * 0)`, `tier("base", p * 10 + cr * 2)`, true},
		{"reversed price", `tier("base", p * 10 + 2 * cr)`, `tier("base", p * 10 + 0.1 * cr)`, true},
		{"request multiplier", `tier("base", p * 10 + cr * 2) * (header("fast") == "yes" ? 2 : 1)`, `tier("base", p * 10 + cr * 1) * (header("fast") == "yes" ? 2 : 1)`, true},
		{"input changed", `tier("base", p * 10 + cr * 2)`, `tier("base", p * 20 + cr * 1)`, false},
		{"condition changed", actual, `len <= 270000 ? tier("standard", p * 10 + c * 50 + cr * 1) : tier("long", p * 20 + c * 100 + cr * 2)`, false},
		{"cache in condition", `cr * 2 > 100 ? tier("a", cr * 2) : tier("b", cr * 2)`, `cr * 1 > 100 ? tier("a", cr * 1) : tier("b", cr * 1)`, false},
		{"request multiplier changed", `tier("base", p * 10 + cr * 2) * (header("fast") == "yes" ? 2 : 1)`, `tier("base", p * 10 + cr * 1) * (header("fast") == "yes" ? 3 : 1)`, false},
		{"negative", `tier("base", p * 10 + cr * 2)`, `tier("base", p * 10 + cr * -1)`, false},
		{"nonfinite", `tier("base", p * 10 + cr * 2)`, `tier("base", p * 10 + cr * 1e999)`, false},
		{"missing category", `tier("base", p * 10)`, `tier("base", p * 10 + cr * 1)`, false},
		{"inside maximum", `tier("base", max(p * 10, cr * 2))`, `tier("base", max(p * 10, cr * 1))`, false},
		{"duplicate tier names", `len > 100 ? tier("a", cr * 2) : tier("a", cr * 4)`, `len > 100 ? tier("a", cr * 1) : tier("a", cr * 3)`, false},
		{"normalized duplicate tier names", `len > 100 ? tier("a", cr * 2) : tier(" A ", cr * 4)`, `len > 100 ? tier("a", cr * 1) : tier(" A ", cr * 3)`, false},
		{"task pricing", `tier("base", u("count") + cr * 2)`, `tier("base", u("count") + cr * 1)`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDisplayBillingExpr(test.actual, test.display)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	// Display validation must not mutate the cached program used for charging.
	cost, trace, err := RunExpr(actual, TokenParams{Len: 100, CR: 100})
	require.NoError(t, err)
	assert.Equal(t, float64(200), cost)
	assert.Equal(t, "standard", trace.MatchedTier)
}
