package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const WalletPaymentReceiptRetryTaskType = "wallet_payment_receipt_retry"

type walletPaymentReceiptRetryHandler struct{}

func (walletPaymentReceiptRetryHandler) Type() string { return WalletPaymentReceiptRetryTaskType }
func (walletPaymentReceiptRetryHandler) Enabled() bool {
	return common.CanRunWriteBackgroundJobs() && model.WalletPaymentSchemaAvailable()
}
func (walletPaymentReceiptRetryHandler) Interval() time.Duration { return 15 * time.Second }
func (walletPaymentReceiptRetryHandler) NewPayload() any         { return nil }
func (walletPaymentReceiptRetryHandler) Run(ctx context.Context, task *model.SystemTask, runner string) {
	if !common.CanRunWriteBackgroundJobs() {
		_ = model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusFailed, nil, "write background jobs disabled")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	counts, err := RetryDueWalletPaymentReceipts(ctx, 100)
	status := model.SystemTaskStatusSucceeded
	message := ""
	if err != nil {
		status = model.SystemTaskStatusFailed
		message = "wallet receipt processing unavailable"
	}
	_ = model.FinishSystemTask(task.TaskID, runner, status, counts, message)
}
func init() { RegisterSystemTaskHandler(walletPaymentReceiptRetryHandler{}) }

// Receipt recovery is independent of order status, current browser flow and
// historical run controls. A committed paid fact survives a crash before credit.
func RetryDueWalletPaymentReceipts(ctx context.Context, limit int) (map[string]int, error) {
	counts := map[string]int{"credited": 0, "review_required": 0}
	if err := CheckWalletPaymentRuntimeSchema(ctx, model.DB); err != nil {
		return counts, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var receipts []model.WalletPaymentReceipt
	err := model.DB.Where("verification_status = ? AND processing_status IN ? AND top_up_id IS NOT NULL AND next_retry_at > ? AND next_retry_at <= ?", "verified", []string{"pending", "review"}, 0, time.Now().Unix()).Order("next_retry_at,id").Limit(limit).Find(&receipts).Error
	if err != nil {
		return counts, err
	}
	for _, receipt := range receipts {
		if ctx.Err() != nil {
			return counts, ctx.Err()
		}
		_, err := model.RetryVerifiedWalletTopUpReceipt(receipt.ID)
		if err != nil {
			counts["review_required"]++
		} else {
			counts["credited"]++
		}
	}
	return counts, nil
}
