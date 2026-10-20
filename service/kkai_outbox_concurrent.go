package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
)

// RunConcurrent claims each available event for immediate execution, with no
// worker-count ceiling. Each event retains the normal lease and acknowledgement
// lifecycle, and cancellation joins every started operation before returning.
// beforeClaim acquires the caller's drain/leader fence; a nil release pauses
// polling. Its release covers the claim through final acknowledgement.
func (p *KKAIOutboxProcessor) RunConcurrent(ctx context.Context, pollInterval time.Duration, beforeClaim func(context.Context) (context.Context, func(), error)) error {
	if !p.valid() || ctx == nil || pollInterval <= 0 || beforeClaim == nil {
		return ErrKKAIOutboxInvalidConfiguration
	}
	topics := p.registeredTopics()
	if len(topics) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	var operations sync.WaitGroup
	defer func() {
		cancel()
		operations.Wait()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		claimCtx, release, err := beforeClaim(ctx)
		if err == nil && release != nil {
			events, claimErr := p.claim(claimCtx, 1, p.now(), topics)
			err = claimErr
			if err == nil && len(events) > 0 {
				operations.Add(1)
				go func() {
					defer operations.Done()
					defer func() {
						if recover() != nil {
							logger.LogWarn(ctx, "concurrent outbox event %d execution panicked", events[0].ID)
						}
					}()
					defer release()
					if _, processErr := p.processClaimedEvent(claimCtx, events[0]); processErr != nil && ctx.Err() == nil {
						logger.LogWarn(ctx, "concurrent outbox event %d failed: %v", events[0].ID, processErr)
					}
				}()
				continue
			}
		}
		if release != nil {
			release()
		}
		if err != nil && ctx.Err() == nil {
			logger.LogWarn(ctx, "concurrent outbox claim failed: %v", err)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
