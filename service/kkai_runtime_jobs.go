package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const KKAIRiskStreamSecretEnvironmentVariable = "KKAI_RISK_STREAM_SECRET"

var kkaiRiskStreamRuntimeState struct {
	sync.RWMutex
	consumer *RiskStreamConsumer
}

// KKAIRiskStreamRuntimeStatus returns the latest in-memory stream telemetry.
// It never probes Redis and is therefore safe for periodic instance reporting.
func KKAIRiskStreamRuntimeStatus() (RiskStreamConsumerStatus, bool) {
	kkaiRiskStreamRuntimeState.RLock()
	consumer := kkaiRiskStreamRuntimeState.consumer
	kkaiRiskStreamRuntimeState.RUnlock()
	if consumer == nil {
		return RiskStreamConsumerStatus{}, false
	}
	return consumer.snapshotStatus(), true
}

func setKKAIRiskStreamRuntimeConsumer(consumer *RiskStreamConsumer) {
	kkaiRiskStreamRuntimeState.Lock()
	kkaiRiskStreamRuntimeState.consumer = consumer
	kkaiRiskStreamRuntimeState.Unlock()
}

func RegisterKKAIRuntimeBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	riskOutboxHandler := NewRiskActionOutboxHandler()
	rebateHandler, err := NewTopUpRebateOutboxHandlerFromEnvironment()
	if err != nil {
		return err
	}
	var topUpCompleted KKAIOutboxHandler
	if rebateHandler != nil {
		topUpCompleted = rebateHandler.Handle
	}
	outboxWorker, err := newKKAIOutboxRuntimeWorker(model.DB, workerID, kkaiOutboxRuntimeHandlers{
		taskBillingAudit:           TaskBillingAuditHandler{}.Handle,
		taskBillingCacheReconcile:  TaskBillingCacheReconcileHandler{}.Handle,
		imageBillingCacheReconcile: ImageBillingCacheReconcileHandler{}.Handle,
		imageAccounting:            ImageGenerationAccountingHandler{}.Handle,
		taskBillingRecovery:        TaskBillingRecoveryHandler{}.Handle,
		taskAccounting:             TaskAccountingHandler{}.Handle,
		riskActionCommitted:        riskOutboxHandler.Handle,
		topUpCompleted:             topUpCompleted,
	})
	if err != nil {
		return err
	}
	if err := registerKKAIOutboxDeliveryJob(registry, outboxWorker); err != nil {
		return err
	}
	if err := RegisterVideoStudioBackgroundJobs(registry, workerID); err != nil {
		return err
	}
	if err := RegisterImageStudioBackgroundJobs(registry, workerID); err != nil {
		return err
	}

	consumer, err := newKKAIRiskStreamConsumerFromEnvironment(workerID)
	if err != nil {
		return err
	}
	if consumer == nil {
		setKKAIRiskStreamRuntimeConsumer(nil)
		return nil
	}
	setKKAIRiskStreamRuntimeConsumer(consumer)
	return registry.Register(BackgroundJob{
		Name:                "kkai-risk-stream",
		Interval:            time.Second,
		RunOnStart:          true,
		WritesData:          true,
		RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			_, err := consumer.ProcessOnce(ctx)
			return err
		},
	})
}

func registerKKAIOutboxDeliveryJob(registry *BackgroundJobRegistry, worker *kkaiOutboxRuntimeWorker) error {
	if registry == nil || !worker.valid() {
		return ErrInvalidBackgroundJob
	}
	return registry.Register(BackgroundJob{
		Name:                "kkai-outbox-delivery",
		Interval:            2 * time.Second,
		RunOnStart:          true,
		WritesData:          true,
		RequiresLeaderLease: true,
		Run:                 worker.ProcessOnce,
	})
}

func newKKAIRiskStreamConsumerFromEnvironment(consumerID string) (*RiskStreamConsumer, error) {
	secret := os.Getenv(KKAIRiskStreamSecretEnvironmentVariable)
	if strings.TrimSpace(secret) == "" {
		return nil, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return nil, errors.New("KKAI risk stream requires Redis")
	}
	store := NewRedisRiskStreamStore(common.RDB)
	return NewRiskStreamConsumer(store, NewRiskActionService(model.DB), DecideKKAIRiskStreamEvent, secret, consumerID)
}
