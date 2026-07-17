package relay

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageSyncAdmissionGateLoadLevels(t *testing.T) {
	testCases := []struct {
		requested int
		admitted  int
	}{
		{requested: 1, admitted: 1},
		{requested: 4, admitted: 4},
		{requested: 8, admitted: 8},
		{requested: 10, admitted: 8},
		{requested: 20, admitted: 8},
	}
	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("concurrency_%d", testCase.requested), func(t *testing.T) {
			gate := newImageSyncAdmissionGate(8, 4)
			admitted := 0
			for i := 0; i < testCase.requested; i++ {
				if _, ok := gate.TryAcquire(fmt.Sprintf("account-%d", i)); ok {
					admitted++
				}
			}
			require.Equal(t, testCase.admitted, admitted)
		})
	}
}
