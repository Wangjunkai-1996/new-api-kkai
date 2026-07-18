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
	outbox := NewKKAIOutboxProcessor(model.DB, workerID)
	riskOutboxHandler := NewRiskActionOutboxHandler()
	if err := outbox.Register(KKAIOutboxTopicRiskActionCommitted, riskOutboxHandler.Handle); err != nil {
		return err
	}
	rebateHandler, err := NewTopUpRebateOutboxHandlerFromEnvironment()
	if err != nil {
		return err
	}
	if rebateHandler != nil {
		if err := outbox.Register(model.KKAIOutboxTopicTopUpCompleted, rebateHandler.Handle); err != nil {
			return err
		}
	}
	if err := registry.Register(BackgroundJob{
		Name:                "kkai-outbox-delivery",
		Interval:            2 * time.Second,
		RunOnStart:          true,
		WritesData:          true,
		RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			_, err := outbox.ProcessBatch(ctx, 50)
			return err
		},
	}); err != nil {
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
