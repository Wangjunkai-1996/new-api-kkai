package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
)

type imageWorkerLeaseContextKey struct{}

// WithImageWorkerLease captures a leadership term, not a perpetual permission.
// Each business write must validate this identity again inside its transaction.
func WithImageWorkerLease(ctx context.Context, lease model.KKAIJobLease) context.Context {
	return context.WithValue(ctx, imageWorkerLeaseContextKey{}, lease)
}

func ImageWorkerFence(ctx context.Context, event model.KKAIOutboxEvent) (model.ImageExecutionFence, error) {
	if ctx == nil || ctx.Err() != nil {
		return model.ImageExecutionFence{}, ErrKKAIOutboxLockLost
	}
	lease, ok := ctx.Value(imageWorkerLeaseContextKey{}).(model.KKAIJobLease)
	if !ok || lease.LeaseName == "" || lease.Holder == "" || lease.Fence <= 0 {
		return model.ImageExecutionFence{}, ErrKKAIOutboxLockLost
	}
	return model.ImageExecutionFence{
		EventID: event.ID, LockedBy: event.LockedBy, LockTimeout: 2 * time.Minute,
		LeaderName: lease.LeaseName, LeaderHolder: lease.Holder, LeaderFence: lease.Fence,
	}, nil
}
