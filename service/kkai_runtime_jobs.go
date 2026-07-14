package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const KKAIRiskStreamSecretEnvironmentVariable = "KKAI_RISK_STREAM_SECRET"

func RegisterKKAIRuntimeBackgroundJobs(registry *BackgroundJobRegistry, workerID string) error {
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	outbox := NewKKAIOutboxProcessor(model.DB, workerID)
	riskOutboxHandler := NewRiskActionOutboxHandler()
	if err := outbox.Register(KKAIOutboxTopicRiskActionCommitted, riskOutboxHandler.Handle); err != nil {
		return err
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
		return nil
	}
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
