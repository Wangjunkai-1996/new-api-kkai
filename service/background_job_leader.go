package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
)

func runLeaderBackgroundJobs(ctx context.Context, runtime BackgroundJobRuntime, jobs []BackgroundJob) {
	for {
		acquired, err := runtime.Lease.TryAcquire(ctx, runtime.HolderID, runtime.LeaseTTL)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("background leader lease acquire failed: %v", err))
		}
		if err == nil && acquired {
			if runLeadershipTerm(ctx, runtime, jobs) {
				return
			}
		}
		if !waitForBackgroundRetry(ctx, runtime.LeaseRetryInterval) {
			return
		}
	}
}

func runLeadershipTerm(parent context.Context, runtime BackgroundJobRuntime, jobs []BackgroundJob) bool {
	termCtx, cancel := context.WithCancel(parent)
	var jobsWG sync.WaitGroup
	for _, job := range jobs {
		jobsWG.Add(1)
		go func(job BackgroundJob) {
			defer jobsWG.Done()
			runBackgroundJobLoop(termCtx, job)
		}(job)
	}

	renewInterval := runtime.LeaseTTL / 3
	if renewInterval <= 0 {
		renewInterval = time.Millisecond
	}
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-parent.Done():
			cancel()
			jobsWG.Wait()
			releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
			runBackgroundShutdownJobs(releaseCtx, jobs)
			if err := runtime.Lease.Release(releaseCtx, runtime.HolderID); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("background leader lease release failed: %v", err))
			}
			releaseCancel()
			return true
		case <-ticker.C:
			renewed, err := runtime.Lease.Renew(parent, runtime.HolderID, runtime.LeaseTTL)
			if err == nil && renewed {
				continue
			}
			cancel()
			jobsWG.Wait()
			if err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("background leader lease renewal failed: %v", err))
			} else {
				logger.LogWarn(context.Background(), "background leader lease lost")
			}
			return false
		}
	}
}

func waitForBackgroundRetry(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
