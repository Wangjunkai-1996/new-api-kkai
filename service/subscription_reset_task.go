package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	SubscriptionMaintenanceInterval = time.Minute
	subscriptionResetBatchSize      = 300
	subscriptionCleanupInterval     = 30 * time.Minute
)

var (
	subscriptionResetRunning atomic.Bool
	subscriptionCleanupLast  atomic.Int64
)

func RunSubscriptionMaintenance(ctx context.Context) error {
	if !subscriptionResetRunning.CompareAndSwap(false, true) {
		return nil
	}
	defer subscriptionResetRunning.Store(false)

	totalReset := 0
	totalExpired := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := model.ExpireDueSubscriptions(subscriptionResetBatchSize)
		if err != nil {
			return fmt.Errorf("expire subscriptions: %w", err)
		}
		if n == 0 {
			break
		}
		totalExpired += n
		if n < subscriptionResetBatchSize {
			break
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := model.ResetDueSubscriptions(subscriptionResetBatchSize)
		if err != nil {
			return fmt.Errorf("reset subscriptions: %w", err)
		}
		if n == 0 {
			break
		}
		totalReset += n
		if n < subscriptionResetBatchSize {
			break
		}
	}
	lastCleanup := time.Unix(subscriptionCleanupLast.Load(), 0)
	if time.Since(lastCleanup) >= subscriptionCleanupInterval {
		if _, err := model.CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600); err != nil {
			return fmt.Errorf("cleanup subscription pre-consume records: %w", err)
		}
		subscriptionCleanupLast.Store(time.Now().Unix())
	}
	if common.DebugEnabled && (totalReset > 0 || totalExpired > 0) {
		logger.LogDebug(ctx, "subscription maintenance: reset_count=%d, expired_count=%d", totalReset, totalExpired)
	}
	return nil
}
