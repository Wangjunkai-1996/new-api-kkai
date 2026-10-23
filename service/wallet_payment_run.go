package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const WalletPaymentHistoryTaskType = "wallet_payment_history"

type walletHistoryHandler struct{}

func (walletHistoryHandler) Type() string { return WalletPaymentHistoryTaskType }
func (walletHistoryHandler) Enabled() bool {
	return common.CanRunWriteBackgroundJobs() && model.WalletPaymentSchemaAvailable()
}
func (walletHistoryHandler) Interval() time.Duration { return 30 * time.Second }
func (walletHistoryHandler) NewPayload() any         { return nil }
func init()                                          { RegisterSystemTaskHandler(walletHistoryHandler{}) }

// Each segment reads persisted runs; task deduplication never loses a run's
// payload. Normal payment/receipt jobs have their own independent task type.
func (walletHistoryHandler) Run(ctx context.Context, task *model.SystemTask, runner string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	err := CheckWalletPaymentRuntimeSchema(ctx, model.DB)
	if !common.CanRunWriteBackgroundJobs() {
		err = model.ErrWalletPaymentSchema
	}
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusFailed, nil, "wallet payment schema or writer unavailable")
		return
	}
	runs, err := model.FindActiveWalletRuns()
	if err == nil {
		for _, run := range runs {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			if e := runWalletHistorySegment(ctx, run, task.TaskID, runner, ReconcileWalletTopUp); e != nil {
				err = e
				break
			}
		}
	}
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status = model.SystemTaskStatusFailed
		message = "wallet history segment interrupted"
	}
	_ = model.FinishSystemTask(task.TaskID, runner, status, nil, message)
}

func runWalletHistorySegment(ctx context.Context, run *model.WalletTopUpRun, taskID, runner string, reconcile func(context.Context, string) (string, error)) error {
	active, err := model.WalletRunTaskLeaseActive(taskID, runner)
	if err != nil {
		return err
	}
	if !active {
		return model.ErrSystemTaskLockLost
	}

	if run.DesiredState == "paused" {
		_, err := model.UpdateWalletTopUpRun(run.ID, run.ControlVersion, map[string]any{"status": "paused"}, model.WalletRunTaskLease{TaskID: taskID, Runner: runner})
		return err
	}
	var selection model.WalletRunSelection
	if common.UnmarshalJsonStr(run.ScopeJSON, &selection) != nil || selection.BatchSize < 1 || selection.BatchSize > 100 || selection.QueriesPerSecond < 1 || selection.QueriesPerSecond > 10 {
		return model.ErrResellerFinanceInvalidInput
	}
	claimed, err := model.UpdateWalletTopUpRun(run.ID, run.ControlVersion, map[string]any{"status": "running", "last_task_id": taskID}, model.WalletRunTaskLease{TaskID: taskID, Runner: runner})
	if err != nil {
		return err
	}
	run = claimed
	orders, err := model.FindWalletRunOrders(selection, run.Cursor, run.MaxOrderID)
	if err != nil {
		return err
	}
	for i, order := range orders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		current, e := model.GetWalletTopUpRun(run.ID)
		if e != nil {
			return e
		}
		if current.ControlVersion != run.ControlVersion || current.DesiredState != "running" {
			return finishWalletRunPause(current, taskID, runner)
		}
		active, e := model.WalletRunTaskLeaseActive(taskID, runner)
		if e != nil {
			return e
		}
		if !active {
			return model.ErrSystemTaskLockLost
		}
		// Recheck the live order after selection: a local end or timeout must
		// never become a manual historical provider query while a run waits.
		liveOrder, e := model.FindTopUpByTradeNo(order.TradeNo)
		if e != nil {
			return e
		}
		category := "closed"
		if !model.WalletTopUpLocalEnded(liveOrder, time.Now().Unix()) {
			category, e = reconcile(ctx, order.TradeNo)
		}
		if e != nil {
			category = "retry_failed"
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		advanced, e := model.CheckpointWalletRun(run, taskID, runner, int64(order.Id), category)
		if e != nil {
			return e
		}
		if !advanced {
			live, e := model.WalletRunTaskLeaseActive(taskID, runner)
			if e != nil {
				return e
			}
			if !live {
				return model.ErrSystemTaskLockLost
			}
			latest, e := model.GetWalletTopUpRun(run.ID)
			if e != nil {
				return e
			}
			return finishWalletRunPause(latest, taskID, runner)
		}
		if i+1 < len(orders) {
			timer := time.NewTimer(time.Second / time.Duration(selection.QueriesPerSecond))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	status := "queued"
	if len(orders) < selection.BatchSize {
		status = "completed"
	}
	_, err = model.UpdateWalletTopUpRun(run.ID, run.ControlVersion, map[string]any{"status": status}, model.WalletRunTaskLease{TaskID: taskID, Runner: runner})
	return err
}
func finishWalletRunPause(run *model.WalletTopUpRun, taskID, runner string) error {
	if run.DesiredState != "paused" || run.Status == "paused" {
		return nil
	}
	_, err := model.UpdateWalletTopUpRun(run.ID, run.ControlVersion, map[string]any{"status": "paused"}, model.WalletRunTaskLease{TaskID: taskID, Runner: runner})
	return err
}
