package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type TaskBillingCacheReconcileHandler struct {
	reconcile func(context.Context, int64) error
}

func (h TaskBillingCacheReconcileHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := model.TaskBillingCacheReconcilePayload{}
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid task billing cache reconcile payload: %w", err))
	}
	if payload.TaskID <= 0 {
		return PermanentKKAIOutboxError(errors.New("invalid task billing cache reconcile payload: task_id must be positive"))
	}
	reconcile := h.reconcile
	if reconcile == nil {
		reconcile = model.ReconcileTaskBillingQuotaCache
	}
	return reconcile(ctx, payload.TaskID)
}
