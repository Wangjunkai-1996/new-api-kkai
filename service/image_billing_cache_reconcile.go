package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type ImageBillingCacheReconcileHandler struct {
	reconcile func(context.Context, int64) error
}

func (h ImageBillingCacheReconcileHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := model.ImageBillingCacheReconcilePayload{}
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid image billing cache reconcile payload: %w", err))
	}
	if payload.GenerationID <= 0 {
		return PermanentKKAIOutboxError(errors.New("invalid image billing cache reconcile payload: generation_id must be positive"))
	}
	reconcile := h.reconcile
	if reconcile == nil {
		reconcile = func(ctx context.Context, generationID int64) error {
			return model.ReconcileImageGenerationBillingCache(ctx, model.DB, generationID)
		}
	}
	return reconcile(ctx, payload.GenerationID)
}
