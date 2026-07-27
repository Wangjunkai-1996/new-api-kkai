package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const defaultVideoOutboxConcurrency = 2

type VideoOutboxWorker struct {
	processors []*KKAIOutboxProcessor
}

func NewVideoOutboxWorker(
	db *gorm.DB,
	workerID string,
	store VideoAssetStore,
	media VideoMediaProcessor,
	fetcher VideoArchiveSourceFetcher,
	tempDir string,
	concurrency int,
) (*VideoOutboxWorker, error) {
	workerID = strings.TrimSpace(workerID)
	if concurrency == 0 {
		concurrency = defaultVideoOutboxConcurrency
	}
	if db == nil || workerID == "" || concurrency < 1 || concurrency > 8 {
		return nil, ErrInvalidVideoOutboxEvent
	}
	pipeline, err := NewVideoAssetPipeline(db, store, media, fetcher, tempDir)
	if err != nil {
		return nil, err
	}
	worker := &VideoOutboxWorker{processors: make([]*KKAIOutboxProcessor, 0, concurrency)}
	for index := 0; index < concurrency; index++ {
		processor := NewKKAIOutboxProcessor(db, fmt.Sprintf("%s-video-%d", workerID, index+1))
		processor.lockTimeout = 30 * time.Second
		if err := pipeline.Register(processor); err != nil {
			return nil, err
		}
		worker.processors = append(worker.processors, processor)
	}
	return worker, nil
}

func (worker *VideoOutboxWorker) ProcessOnce(ctx context.Context) error {
	if worker == nil || len(worker.processors) == 0 {
		return ErrInvalidVideoOutboxEvent
	}
	errorsByProcessor := make([]error, len(worker.processors))
	var wait sync.WaitGroup
	for index, processor := range worker.processors {
		wait.Add(1)
		go func(index int, processor *KKAIOutboxProcessor) {
			defer wait.Done()
			_, errorsByProcessor[index] = processor.ProcessBatch(ctx, 1)
		}(index, processor)
	}
	wait.Wait()
	return errors.Join(errorsByProcessor...)
}
