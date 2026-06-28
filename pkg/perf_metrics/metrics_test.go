package perfmetrics

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestQueryLastEventsByGroupReturnsRollingLatestSignals(t *testing.T) {
	common.RedisEnabled = false
	ResetForTest()
	t.Cleanup(ResetForTest)

	baseTs := time.Now().Add(-2 * time.Hour).Unix()
	for i := 0; i < 70; i++ {
		recordRecentGroupEvent(Sample{
			Group:     "default",
			LatencyMs: int64(i),
			TtftMs:    int64(i),
			HasTtft:   true,
			Success:   i%10 != 0,
		}, baseTs+int64(i))
	}

	result := QueryLastEventsByGroup([]string{"default"})

	require.Len(t, result.Groups, 1)
	events := result.Groups[0].Events
	require.Len(t, events, recentGroupEventLimit)
	require.Equal(t, int64(10), events[0].LatencyMs)
	require.Equal(t, int64(69), events[len(events)-1].LatencyMs)
	require.True(t, events[0].Ts < events[len(events)-1].Ts)
}

func TestQueryRecentEventsByGroupStillFiltersByWindow(t *testing.T) {
	common.RedisEnabled = false
	ResetForTest()
	t.Cleanup(ResetForTest)

	now := time.Now().Unix()
	recordRecentGroupEvent(Sample{
		Group:     "default",
		LatencyMs: 100,
		Success:   true,
	}, now-3600)
	recordRecentGroupEvent(Sample{
		Group:     "default",
		LatencyMs: 200,
		Success:   true,
	}, now-60)

	windowResult := QueryRecentEventsByGroup(5, []string{"default"})
	rollingResult := QueryLastEventsByGroup([]string{"default"})

	require.Len(t, windowResult.Groups, 1)
	require.Len(t, windowResult.Groups[0].Events, 1)
	require.Equal(t, int64(200), windowResult.Groups[0].Events[0].LatencyMs)
	require.Len(t, rollingResult.Groups, 1)
	require.Len(t, rollingResult.Groups[0].Events, 2)
}
