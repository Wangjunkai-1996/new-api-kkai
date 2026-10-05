package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyCreditToUSD(t *testing.T) {
	for _, tc := range []struct{ old, want int64 }{
		{0, 0}, {133000000, 9975000}, {7, 1}, {14, 1}, {20, 2}, {-20, -2},
		{math.MaxInt64, 691752902764108186}, {math.MinInt64, -691752902764108186},
	} {
		assert.Equal(t, tc.want, LegacyCreditToUSD(tc.old))
	}
}

func TestParseCreditEpochRejectsInvalidMarker(t *testing.T) {
	epoch, err := ParseCreditEpochCutover("")
	assert.NoError(t, err)
	assert.Nil(t, epoch)
	for _, raw := range []string{"{", "{}", `{"version":1,"numerator":3,"denominator":40}`} {
		_, err := ParseCreditEpochCutover(raw)
		assert.ErrorIs(t, err, ErrCreditEpochInvalid)
	}
}

func TestHistoricalCreditQuotaRespectsPerTableBoundary(t *testing.T) {
	marker, err := Marshal(CreditEpochCutover{Version: 1, MigrationID: "history", SourceEpoch: CreditEpochLegacy, TargetEpoch: CreditEpochUSD, Numerator: 3, Denominator: 40, PlanHash: "p", PricingPlanHash: "q", AppliedAt: 1, LegacyTopUpPolicy: "hold", LegacyMaxIDs: map[string]int64{"tasks": 10, "kkai_image_generations": 5}})
	require.NoError(t, err)
	OptionMapRWMutex.Lock()
	previous := OptionMap
	OptionMap = map[string]string{CreditEpochOption: string(marker)}
	OptionMapRWMutex.Unlock()
	t.Cleanup(func() { OptionMapRWMutex.Lock(); OptionMap = previous; OptionMapRWMutex.Unlock() })
	for _, test := range []struct {
		table string
		id    int64
		want  int
	}{
		{"tasks", 10, 49875000},
		{"tasks", 11, 665000000},
		{"kkai_image_generations", 5, 49875000},
		{"kkai_image_generations", 6, 665000000},
	} {
		got, err := HistoricalCreditQuota(test.table, test.id, 665000000)
		require.NoError(t, err)
		assert.Equal(t, test.want, got)
	}
	_, err = HistoricalCreditQuota("unknown", 1, 1330)
	require.ErrorIs(t, err, ErrCreditEpochInvalid)
}
