package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestElapsedSecondsKeepsDurationAtLeastFRT(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(173100 * time.Millisecond)

	require.Equal(t, int64(174), elapsedSeconds(start, now, 174000))
	require.Equal(t, int64(174), elapsedSeconds(start, start.Add(173900*time.Millisecond), 0))
	require.Equal(t, int64(0), elapsedSeconds(start, start, 0))
}
