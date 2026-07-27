package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	defaultKKAIOutboxConcurrency = 3
	defaultKKAIOutboxBatchLimit  = 50
)

type kkaiOutboxRuntimeHandlers struct {
	taskBillingAudit          KKAIOutboxHandler
	taskBillingCacheReconcile KKAIOutboxHandler
	taskBillingRecovery       KKAIOutboxHandler
	taskAccounting            KKAIOutboxHandler
	riskActionCommitted       KKAIOutboxHandler
	topUpCompleted            KKAIOutboxHandler
}

type kkaiOutboxRuntimeWorker struct {
	processors []*KKAIOutboxProcessor
}

type kkaiOutboxProcessorResult struct {
	index  int
	result *KKAIOutboxBatchResult
	err    error
}

func newKKAIOutboxRuntimeWorker(
	db *gorm.DB,
	workerID string,
	handlers kkaiOutboxRuntimeHandlers,
) (*kkaiOutboxRuntimeWorker, error) {
	if db == nil || !leaderLeaseNamePattern.MatchString(workerID) ||
		handlers.taskBillingAudit == nil || handlers.taskBillingCacheReconcile == nil ||
		handlers.taskBillingRecovery == nil || handlers.taskAccounting == nil ||
		handlers.riskActionCommitted == nil {
		return nil, ErrKKAIOutboxInvalidConfiguration
	}
	type topicHandler struct {
		topic   string
		handler KKAIOutboxHandler
	}
	lanes := []struct {
		name     string
		handlers []topicHandler
	}{
		{
			name: "billing",
			handlers: []topicHandler{
				{topic: model.KKAIOutboxTopicTaskBillingAudit, handler: handlers.taskBillingAudit},
				{topic: model.KKAIOutboxTopicTaskBillingCacheReconcile, handler: handlers.taskBillingCacheReconcile},
				{topic: KKAIOutboxTopicTaskBillingRecovery, handler: handlers.taskBillingRecovery},
				{topic: KKAIOutboxTopicTaskAccounting, handler: handlers.taskAccounting},
			},
		},
		{
			name: "risk",
			handlers: []topicHandler{
				{topic: KKAIOutboxTopicRiskActionCommitted, handler: handlers.riskActionCommitted},
			},
		},
		{
			name: "webhook",
			handlers: []topicHandler{
				{topic: model.KKAIOutboxTopicTopUpCompleted, handler: handlers.topUpCompleted},
			},
		},
	}
	worker := &kkaiOutboxRuntimeWorker{processors: make([]*KKAIOutboxProcessor, 0, len(lanes))}
	for _, lane := range lanes {
		processor := NewKKAIOutboxProcessor(db, fmt.Sprintf("%s-outbox-%s", workerID, lane.name))
		for _, registration := range lane.handlers {
			if registration.handler == nil {
				continue
			}
			if err := processor.Register(registration.topic, registration.handler); err != nil {
				return nil, err
			}
		}
		worker.processors = append(worker.processors, processor)
	}
	if len(worker.processors) != defaultKKAIOutboxConcurrency {
		return nil, ErrKKAIOutboxInvalidConfiguration
	}
	return worker, nil
}

func (worker *kkaiOutboxRuntimeWorker) ProcessOnce(ctx context.Context) error {
	if !worker.valid() {
		return ErrKKAIOutboxInvalidConfiguration
	}
	errorsByProcessor := make([]error, len(worker.processors))
	ready := make([]bool, len(worker.processors))
	for index := range ready {
		ready[index] = true
	}
	results := make(chan kkaiOutboxProcessorResult, len(worker.processors))
	active := 0
	claimed := 0
	for {
		if ctx.Err() == nil {
			for index, processor := range worker.processors {
				if !ready[index] || claimed+active >= defaultKKAIOutboxBatchLimit {
					continue
				}
				ready[index] = false
				active++
				startKKAIOutboxProcessor(ctx, index, processor, results)
			}
		}
		if active == 0 {
			break
		}
		processorResult := <-results
		active--
		if processorResult.err != nil {
			errorsByProcessor[processorResult.index] = errors.Join(
				errorsByProcessor[processorResult.index],
				processorResult.err,
			)
			continue
		}
		if processorResult.result == nil || processorResult.result.Claimed == 0 {
			continue
		}
		claimed += processorResult.result.Claimed
		if claimed < defaultKKAIOutboxBatchLimit && ctx.Err() == nil {
			ready[processorResult.index] = true
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		errorsByProcessor = append(errorsByProcessor, ctxErr)
	}
	return errors.Join(errorsByProcessor...)
}

func (worker *kkaiOutboxRuntimeWorker) valid() bool {
	if worker == nil || len(worker.processors) != defaultKKAIOutboxConcurrency {
		return false
	}
	for _, processor := range worker.processors {
		if processor == nil {
			return false
		}
	}
	return true
}

func startKKAIOutboxProcessor(
	ctx context.Context,
	index int,
	processor *KKAIOutboxProcessor,
	results chan<- kkaiOutboxProcessorResult,
) {
	go func() {
		result, err := runKKAIOutboxProcessor(ctx, processor)
		results <- kkaiOutboxProcessorResult{index: index, result: result, err: err}
	}()
}

func runKKAIOutboxProcessor(
	ctx context.Context,
	processor *KKAIOutboxProcessor,
) (result *KKAIOutboxBatchResult, processErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			processErr = fmt.Errorf("KKAI outbox processor panic: %v", recovered)
		}
	}()
	return processor.ProcessBatch(ctx, 1)
}
