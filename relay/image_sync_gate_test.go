package relay

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageSyncAdmissionGateEnforcesGlobalLimit(t *testing.T) {
	gate := newImageSyncAdmissionGate(8, 4)
	leases := make([]*imageSyncLease, 0, 8)
	for account := 0; account < 2; account++ {
		for i := 0; i < 4; i++ {
			lease, ok := gate.TryAcquire(string(rune('a' + account)))
			require.True(t, ok)
			leases = append(leases, lease)
		}
	}

	_, ok := gate.TryAcquire("third-account")
	require.False(t, ok)
	for _, lease := range leases {
		lease.Release()
	}
	_, ok = gate.TryAcquire("third-account")
	require.True(t, ok)
}

func TestImageSyncAdmissionGateEnforcesAccountLimit(t *testing.T) {
	gate := newImageSyncAdmissionGate(8, 4)
	for i := 0; i < 4; i++ {
		_, ok := gate.TryAcquire("account")
		require.True(t, ok)
	}
	_, ok := gate.TryAcquire("account")
	require.False(t, ok)
	_, ok = gate.TryAcquire("other-account")
	require.True(t, ok)
}

func TestImageSyncAdmissionGateNeverQueues(t *testing.T) {
	gate := newImageSyncAdmissionGate(8, 4)
	var admitted atomic.Int32
	var rejected atomic.Int32
	var wg sync.WaitGroup
	release := make(chan struct{})
	attempted := make(chan struct{}, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, ok := gate.TryAcquire("account")
			attempted <- struct{}{}
			if !ok {
				rejected.Add(1)
				return
			}
			admitted.Add(1)
			<-release
			lease.Release()
		}()
	}
	for i := 0; i < 20; i++ {
		<-attempted
	}
	close(release)
	wg.Wait()

	require.Equal(t, int32(20), admitted.Load()+rejected.Load())
	require.Equal(t, int32(4), admitted.Load())
}

func TestImageSyncLeaseReleaseIsIdempotent(t *testing.T) {
	gate := newImageSyncAdmissionGate(1, 1)
	lease, ok := gate.TryAcquire("account")
	require.True(t, ok)
	lease.Release()
	lease.Release()
	_, ok = gate.TryAcquire("account")
	require.True(t, ok)
}
