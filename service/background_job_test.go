package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

type fakeBackgroundJobLease struct {
	mu           sync.Mutex
	acquire      bool
	renew        bool
	acquireCalls int
	renewCalls   int
	releaseCalls int
}

func (l *fakeBackgroundJobLease) TryAcquire(context.Context, string, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acquireCalls++
	return l.acquire, nil
}

func (l *fakeBackgroundJobLease) Renew(context.Context, string, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.renewCalls++
	return l.renew, nil
}

func (l *fakeBackgroundJobLease) Release(context.Context, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releaseCalls++
	return nil
}

func TestBackgroundJobRegistryRejectsUnsafeDeclarations(t *testing.T) {
	registry := NewBackgroundJobRegistry()
	require.ErrorIs(t, registry.Register(BackgroundJob{}), ErrInvalidBackgroundJob)
	require.ErrorIs(t, registry.Register(BackgroundJob{
		Name:       "unsafe-writer",
		Interval:   time.Minute,
		WritesData: true,
		Run:        func(context.Context) error { return nil },
	}), ErrInvalidBackgroundJob)

	valid := BackgroundJob{
		Name:                "safe-writer",
		Interval:            time.Minute,
		WritesData:          true,
		RequiresLeaderLease: true,
		Run:                 func(context.Context) error { return nil },
	}
	require.NoError(t, registry.Register(valid))
	require.ErrorIs(t, registry.Register(valid), ErrDuplicateBackgroundJob)
}

func TestBackgroundJobRegistryRunsOnlyReadJobsWithoutLeaderCapability(t *testing.T) {
	for _, role := range []common.NodeRole{common.NodeRoleStandbyReadonly, common.NodeRoleServing, common.NodeRoleLeader} {
		t.Run(string(role), func(t *testing.T) {
			registry := NewBackgroundJobRegistry()
			readRan := make(chan struct{}, 1)
			writeRan := make(chan struct{}, 1)
			require.NoError(t, registry.Register(BackgroundJob{
				Name:       "cache-sync",
				Interval:   time.Hour,
				RunOnStart: true,
				WritesData: false,
				Run: func(context.Context) error {
					readRan <- struct{}{}
					return nil
				},
			}))
			require.NoError(t, registry.Register(BackgroundJob{
				Name:                "database-writer",
				Interval:            time.Hour,
				RunOnStart:          true,
				WritesData:          true,
				RequiresLeaderLease: true,
				Run: func(context.Context) error {
					writeRan <- struct{}{}
					return nil
				},
			}))

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				registry.Run(ctx, BackgroundJobRuntime{Role: role, WriteJobsEnabled: false})
				close(done)
			}()
			select {
			case <-readRan:
			case <-time.After(time.Second):
				t.Fatal("read job did not start")
			}
			cancel()
			<-done
			select {
			case <-writeRan:
				t.Fatal("write job ran without leader capability")
			default:
			}
		})
	}
}

func TestBackgroundJobRegistryCancelsWritersWhenLeaseIsLost(t *testing.T) {
	registry := NewBackgroundJobRegistry()
	started := make(chan struct{})
	stopped := make(chan struct{})
	require.NoError(t, registry.Register(BackgroundJob{
		Name:                "lease-bound-writer",
		Interval:            time.Hour,
		RunOnStart:          true,
		WritesData:          true,
		RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		},
	}))
	lease := &fakeBackgroundJobLease{acquire: true, renew: false}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		registry.Run(ctx, BackgroundJobRuntime{
			Role:               common.NodeRoleLeader,
			WriteJobsEnabled:   true,
			HolderID:           "blue-runner",
			Lease:              lease,
			LeaseTTL:           30 * time.Millisecond,
			LeaseRetryInterval: time.Hour,
		})
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("write job did not start after lease acquisition")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("write job was not cancelled after lease loss")
	}
	cancel()
	<-done
	require.Equal(t, 1, lease.acquireCalls)
	require.GreaterOrEqual(t, lease.renewCalls, 1)
}
