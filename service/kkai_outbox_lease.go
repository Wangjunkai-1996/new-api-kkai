package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const kkaiOutboxLockedByMaxLength = 128

type kkaiOutboxLease struct {
	handlerCtx      context.Context
	cancelHandler   context.CancelFunc
	cancelHeartbeat context.CancelFunc
	done            chan struct{}
	lost            chan error
	stopOnce        sync.Once
	stopErr         error
}

func (p *KKAIOutboxProcessor) startKKAIOutboxLease(parent context.Context, event model.KKAIOutboxEvent) *kkaiOutboxLease {
	handlerCtx, cancelHandler := context.WithCancel(parent)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	lease := &kkaiOutboxLease{
		handlerCtx:      handlerCtx,
		cancelHandler:   cancelHandler,
		cancelHeartbeat: cancelHeartbeat,
		done:            make(chan struct{}),
		lost:            make(chan error, 1),
	}
	go func() {
		defer close(lease.done)
		ticker := time.NewTicker(p.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-handlerCtx.Done():
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := p.renewKKAIOutboxLease(renewCtx, event)
				cancel()
				if err == nil {
					continue
				}
				leaseLost := fmt.Errorf("%w: event %d fence %s: %v", ErrKKAIOutboxLockLost, event.ID, event.LockedBy, err)
				lease.lost <- leaseLost
				cancelHandler()
				return
			}
		}
	}()
	return lease
}

func (l *kkaiOutboxLease) context() context.Context {
	return l.handlerCtx
}

func (l *kkaiOutboxLease) stop() error {
	l.stopOnce.Do(func() {
		l.cancelHeartbeat()
		<-l.done
		l.cancelHandler()
		select {
		case l.stopErr = <-l.lost:
		default:
		}
	})
	return l.stopErr
}

func (p *KKAIOutboxProcessor) renewKKAIOutboxLease(ctx context.Context, event model.KKAIOutboxEvent) error {
	lockedAt := p.now().Unix()
	update := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", event.ID, model.KKAIOutboxStatusPending, event.LockedBy).
		Update("locked_at", lockedAt)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected > 0 {
		return nil
	}

	var current struct {
		Status   string
		LockedBy string
	}
	if err := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Select("status", "locked_by").Where("id = ?", event.ID).Take(&current).Error; err != nil {
		return err
	}
	if current.Status != model.KKAIOutboxStatusPending || current.LockedBy != event.LockedBy {
		return ErrKKAIOutboxLockLost
	}
	return nil
}

func (p *KKAIOutboxProcessor) newKKAIOutboxFence() string {
	suffix := common.NewRequestId()
	maxWorkerLength := kkaiOutboxLockedByMaxLength - len(suffix) - 1
	workerID := p.workerID
	if len(workerID) > maxWorkerLength {
		workerID = workerID[:maxWorkerLength]
	}
	return workerID + ":" + suffix
}
