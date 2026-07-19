package controller

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuotaTotalForDisplayDoesNotOverflowInt64(t *testing.T) {
	total := quotaTotalForDisplay(math.MaxInt64, math.MaxInt64)

	require.Equal(t, float64(math.MaxInt64)+float64(math.MaxInt64), total)
	require.Positive(t, total)
}
