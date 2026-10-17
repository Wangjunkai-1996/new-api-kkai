package service

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type imageExecutionContextKey struct{}

type imageExecutionContext struct {
	generationID int64
	fence        model.ImageExecutionFence
}

func WithImageExecutionFence(ctx context.Context, generationID int64, fence model.ImageExecutionFence) context.Context {
	return context.WithValue(ctx, imageExecutionContextKey{}, imageExecutionContext{generationID: generationID, fence: fence})
}

func ImageExecutionFenceFromContext(ctx context.Context) (int64, model.ImageExecutionFence, bool) {
	if ctx == nil {
		return 0, model.ImageExecutionFence{}, false
	}
	execution, ok := ctx.Value(imageExecutionContextKey{}).(imageExecutionContext)
	return execution.generationID, execution.fence, ok
}

func requireImageExecutionOwnershipTx(ctx context.Context, tx *gorm.DB, generationID int64) error {
	boundID, fence, ok := ImageExecutionFenceFromContext(ctx)
	if !ok {
		return nil
	}
	if generationID != boundID {
		return model.ErrImageExecutionFenceLost
	}
	_, err := model.RequireImageExecutionFenceTx(tx, generationID, fence)
	return err
}
