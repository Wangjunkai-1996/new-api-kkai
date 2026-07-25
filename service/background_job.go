package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

var (
	ErrInvalidBackgroundJob   = errors.New("invalid background job declaration")
	ErrDuplicateBackgroundJob = errors.New("duplicate background job")
	ErrInvalidJobRuntime      = errors.New("invalid background job runtime")
)

type BackgroundJob struct {
	Name                     string
	Interval                 time.Duration
	RunOnStart               bool
	RunOnShutdown            bool
	WritesData               bool
	RequiresLeaderLease      bool
	FlushesProcessLocalState bool
	Wakeup                   <-chan struct{}
	Run                      func(context.Context) error
}

type BackgroundJobLease interface {
	TryAcquire(context.Context, string, time.Duration) (bool, error)
	Renew(context.Context, string, time.Duration) (bool, error)
	Release(context.Context, string) error
}

type BackgroundJobRuntime struct {
	Role                  common.NodeRole
	WriteJobsEnabled      bool
	LocalWriteJobsEnabled bool
	HolderID              string
	Lease                 BackgroundJobLease
	LeaseTTL              time.Duration
	LeaseRetryInterval    time.Duration
}

type BackgroundJobRegistry struct {
	mu   sync.RWMutex
	jobs map[string]BackgroundJob
}

type BackgroundJobDescriptor struct {
	Name                     string
	Interval                 time.Duration
	RunOnStart               bool
	RunOnShutdown            bool
	WritesData               bool
	RequiresLeaderLease      bool
	FlushesProcessLocalState bool
}

func NewBackgroundJobRegistry() *BackgroundJobRegistry {
	return &BackgroundJobRegistry{jobs: make(map[string]BackgroundJob)}
}

func (r *BackgroundJobRegistry) Register(job BackgroundJob) error {
	if r == nil || !leaderLeaseNamePattern.MatchString(job.Name) || job.Interval <= 0 || job.Run == nil {
		return ErrInvalidBackgroundJob
	}
	if job.WritesData {
		if job.RequiresLeaderLease == job.FlushesProcessLocalState {
			return ErrInvalidBackgroundJob
		}
	} else if job.RequiresLeaderLease || job.FlushesProcessLocalState {
		return ErrInvalidBackgroundJob
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[job.Name]; exists {
		return ErrDuplicateBackgroundJob
	}
	r.jobs[job.Name] = job
	return nil
}

func (r *BackgroundJobRegistry) Run(ctx context.Context, runtime BackgroundJobRuntime) error {
	if r == nil {
		return ErrInvalidJobRuntime
	}
	readJobs, localWriteJobs, leaderWriteJobs := r.partitionedJobs()
	if runtime.LocalWriteJobsEnabled && len(localWriteJobs) > 0 {
		if runtime.Role != common.NodeRoleServing && runtime.Role != common.NodeRoleLeader {
			return ErrInvalidJobRuntime
		}
	}
	if runtime.WriteJobsEnabled && len(leaderWriteJobs) > 0 {
		if err := validateBackgroundJobRuntime(runtime); err != nil {
			return err
		}
	}
	jobs := make([]BackgroundJob, 0, len(readJobs)+len(localWriteJobs))
	jobs = append(jobs, readJobs...)
	if runtime.LocalWriteJobsEnabled {
		jobs = append(jobs, localWriteJobs...)
	}
	var jobsWG sync.WaitGroup
	for _, job := range jobs {
		jobsWG.Add(1)
		go func(job BackgroundJob) {
			defer jobsWG.Done()
			runBackgroundJobLoop(ctx, job)
		}(job)
	}

	if runtime.WriteJobsEnabled && len(leaderWriteJobs) > 0 {
		runLeaderBackgroundJobs(ctx, runtime, leaderWriteJobs)
	} else {
		<-ctx.Done()
	}
	jobsWG.Wait()
	if runtime.LocalWriteJobsEnabled {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		runBackgroundShutdownJobs(shutdownCtx, localWriteJobs)
		cancel()
	}
	return nil
}

func (r *BackgroundJobRegistry) partitionedJobs() ([]BackgroundJob, []BackgroundJob, []BackgroundJob) {
	r.mu.RLock()
	jobs := make([]BackgroundJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	r.mu.RUnlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })

	readJobs := make([]BackgroundJob, 0, len(jobs))
	localWriteJobs := make([]BackgroundJob, 0, len(jobs))
	leaderWriteJobs := make([]BackgroundJob, 0, len(jobs))
	for _, job := range jobs {
		switch {
		case job.FlushesProcessLocalState:
			localWriteJobs = append(localWriteJobs, job)
		case job.WritesData:
			leaderWriteJobs = append(leaderWriteJobs, job)
		default:
			readJobs = append(readJobs, job)
		}
	}
	return readJobs, localWriteJobs, leaderWriteJobs
}

func (r *BackgroundJobRegistry) Descriptors() []BackgroundJobDescriptor {
	if r == nil {
		return nil
	}
	readJobs, localWriteJobs, leaderWriteJobs := r.partitionedJobs()
	jobs := make([]BackgroundJob, 0, len(readJobs)+len(localWriteJobs)+len(leaderWriteJobs))
	jobs = append(jobs, readJobs...)
	jobs = append(jobs, localWriteJobs...)
	jobs = append(jobs, leaderWriteJobs...)
	descriptors := make([]BackgroundJobDescriptor, 0, len(jobs))
	for _, job := range jobs {
		descriptors = append(descriptors, BackgroundJobDescriptor{
			Name:                     job.Name,
			Interval:                 job.Interval,
			RunOnStart:               job.RunOnStart,
			RunOnShutdown:            job.RunOnShutdown,
			WritesData:               job.WritesData,
			RequiresLeaderLease:      job.RequiresLeaderLease,
			FlushesProcessLocalState: job.FlushesProcessLocalState,
		})
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Name < descriptors[j].Name })
	return descriptors
}

func validateBackgroundJobRuntime(runtime BackgroundJobRuntime) error {
	if runtime.Role != common.NodeRoleLeader || runtime.Lease == nil ||
		!leaderLeaseNamePattern.MatchString(runtime.HolderID) || runtime.LeaseTTL <= 0 || runtime.LeaseRetryInterval <= 0 {
		return ErrInvalidJobRuntime
	}
	return nil
}

func runBackgroundJobLoop(ctx context.Context, job BackgroundJob) {
	if job.RunOnStart {
		runBackgroundJob(ctx, job)
	}
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-job.Wakeup:
			runBackgroundJob(ctx, job)
		case <-ticker.C:
			runBackgroundJob(ctx, job)
		}
	}
}

func runBackgroundJob(ctx context.Context, job BackgroundJob) {
	if err := executeBackgroundJob(ctx, job); err != nil && !errors.Is(err, context.Canceled) {
		logger.LogWarn(context.Background(), fmt.Sprintf("background job %s failed: %v", job.Name, err))
	}
}

func runBackgroundShutdownJobs(ctx context.Context, jobs []BackgroundJob) {
	for _, job := range jobs {
		if job.RunOnShutdown {
			runBackgroundJob(ctx, job)
		}
	}
}

func executeBackgroundJob(ctx context.Context, job BackgroundJob) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return job.Run(ctx)
}
