package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const backgroundLeaderLeaseName = "newapi-background-writer"

func newApplicationBackgroundJobs(workerID string) (*service.BackgroundJobRegistry, error) {
	registry := service.NewBackgroundJobRegistry()
	syncInterval := time.Duration(common.SyncFrequency) * time.Second
	if syncInterval <= 0 {
		return nil, errors.New("SYNC_FREQUENCY must be positive")
	}
	if err := registry.Register(service.BackgroundJob{
		Name:       "runtime-cache-sync",
		Interval:   syncInterval,
		RunOnStart: false,
		WritesData: false,
		Run:        syncRuntimeCaches,
	}); err != nil {
		return nil, err
	}

	writeJobs := []service.BackgroundJob{
		{
			Name:                "system-instance-report",
			Interval:            service.SystemInstanceReportInterval,
			RunOnStart:          true,
			WritesData:          true,
			RequiresLeaderLease: true,
			Run: func(context.Context) error {
				return service.ReportCurrentSystemInstance()
			},
		},
		{
			Name:                "system-task-maintenance",
			Interval:            service.SystemTaskMaintenanceInterval,
			RunOnStart:          true,
			WritesData:          true,
			RequiresLeaderLease: true,
			Wakeup:              service.SystemTaskWakeup(),
			Run: func(ctx context.Context) error {
				return service.RunSystemTaskMaintenance(ctx, workerID)
			},
		},
		{
			Name:                "codex-credential-refresh",
			Interval:            service.CodexCredentialRefreshInterval,
			RunOnStart:          true,
			WritesData:          true,
			RequiresLeaderLease: true,
			Run:                 service.RunCodexCredentialAutoRefresh,
		},
		{
			Name:                "subscription-maintenance",
			Interval:            service.SubscriptionMaintenanceInterval,
			RunOnStart:          true,
			WritesData:          true,
			RequiresLeaderLease: true,
			Run:                 service.RunSubscriptionMaintenance,
		},
		{
			Name:                     "quota-dashboard-flush",
			Interval:                 positiveMinutes(common.DataExportInterval),
			RunOnStart:               true,
			RunOnShutdown:            true,
			WritesData:               true,
			FlushesProcessLocalState: true,
			Run: func(context.Context) error {
				if common.DataExportEnabled {
					return model.SaveQuotaDataCache()
				}
				return nil
			},
		},
		{
			Name:                "performance-metric-flush",
			Interval:            positiveMinutes(perf_metrics_setting.GetFlushIntervalMinutes()),
			RunOnShutdown:       true,
			WritesData:          true,
			RequiresLeaderLease: true,
			Run:                 perfmetrics.RunMaintenance,
		},
	}
	for _, job := range writeJobs {
		if err := registry.Register(job); err != nil {
			return nil, err
		}
	}

	if raw := strings.TrimSpace(os.Getenv("CHANNEL_UPDATE_FREQUENCY")); raw != "" {
		minutes, err := strconv.Atoi(raw)
		if err != nil || minutes <= 0 {
			return nil, errors.New("CHANNEL_UPDATE_FREQUENCY must be a positive integer")
		}
		if err := registry.Register(service.BackgroundJob{
			Name:                "channel-balance-refresh",
			Interval:            time.Duration(minutes) * time.Minute,
			WritesData:          true,
			RequiresLeaderLease: true,
			Run:                 controller.RunAutomaticChannelBalanceUpdate,
		}); err != nil {
			return nil, err
		}
	}

	common.BatchUpdateEnabled = false
	if raw := strings.TrimSpace(os.Getenv("BATCH_UPDATE_ENABLED")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, errors.New("BATCH_UPDATE_ENABLED must be a boolean")
		}
		if enabled {
			return nil, errors.New("BATCH_UPDATE_ENABLED is incompatible with lease-based multi-node execution")
		}
	}

	if err := service.RegisterKKAIRuntimeBackgroundJobs(registry, workerID); err != nil {
		return nil, err
	}
	return registry, nil
}

func syncRuntimeCaches(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var syncErrors []error
	if err := model.SyncOptionsOnce(); err != nil {
		syncErrors = append(syncErrors, fmt.Errorf("options: %w", err))
	}
	if common.MemoryCacheEnabled {
		if err := model.SyncChannelCacheOnce(); err != nil {
			syncErrors = append(syncErrors, err)
		}
	}
	if err := authz.ReloadPolicy(); err != nil {
		syncErrors = append(syncErrors, fmt.Errorf("authz: %w", err))
	}
	model.RefreshPricing()
	return errors.Join(syncErrors...)
}

func positiveMinutes(minutes int) time.Duration {
	if minutes <= 0 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

func backgroundWorkerID() string {
	digest := sha256.Sum256([]byte(common.NodeName))
	return fmt.Sprintf("node-%x-%s", digest[:6], common.GetRandomString(8))
}

func currentBackgroundJobRuntime(workerID string) service.BackgroundJobRuntime {
	runtime := service.BackgroundJobRuntime{
		Role:                  common.CurrentNodeRole(),
		WriteJobsEnabled:      common.CanRunWriteBackgroundJobs(),
		LocalWriteJobsEnabled: common.CanRunProcessLocalBackgroundJobs(),
	}
	if !runtime.WriteJobsEnabled {
		return runtime
	}
	runtime.HolderID = workerID
	runtime.Lease = service.NewKKAILeaderLeaseStore(model.DB, backgroundLeaderLeaseName)
	runtime.LeaseTTL = 45 * time.Second
	runtime.LeaseRetryInterval = 5 * time.Second
	return runtime
}
