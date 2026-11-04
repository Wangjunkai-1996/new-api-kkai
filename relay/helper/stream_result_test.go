package helper

import (
	"context"
	"fmt"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestStreamResultSoftErrorsPreserveProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"client cancellation", fmt.Errorf("write: %w", context.Canceled), false},
		{"deadline", fmt.Errorf("read: %w", context.DeadlineExceeded), true},
		{"provider text is not context cancellation", fmt.Errorf("context canceled"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := relaycommon.NewStreamStatus()
			result := newStreamResult(status)
			result.Error(tc.err)
			assert.Equal(t, tc.want, status.HasErrors())
			assert.False(t, result.IsStopped(), "soft errors leave the upstream drain active")
		})
	}
}
