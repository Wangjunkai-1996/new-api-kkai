package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalletQuotaValidationPreservesInt64AndRejectsInexactNewAmounts(t *testing.T) {
	for _, quota := range []int64{math.MaxInt32 + 1, 1 << 40, 1<<53 - 1} {
		require.NoError(t, ValidateWalletQuota(quota), "64-bit wallet values must not be truncated to int32")
	}
	for _, quota := range []int64{1 << 53, math.MaxInt64} {
		require.Error(t, ValidateWalletQuota(quota), "new amounts must survive JSON number round trips exactly")
	}
}
