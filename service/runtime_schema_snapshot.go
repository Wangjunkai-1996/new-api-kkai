package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

type runtimeSchemaSnapshot struct {
	state atomic.Pointer[runtimeSchemaSnapshotState]
}

type runtimeSchemaSnapshotState struct {
	db        *gorm.DB
	ready     bool
	expiresAt time.Time
}

var shopReadSchema runtimeSchemaSnapshot
var invoiceReadSchema runtimeSchemaSnapshot
var imageAsyncReadSchema runtimeSchemaSnapshot
var requestTicketReadSchema runtimeSchemaSnapshot

func (snapshot *runtimeSchemaSnapshot) ready(ctx context.Context, db *gorm.DB) bool {
	if ctx == nil || ctx.Err() != nil || db == nil {
		return false
	}
	state := snapshot.state.Load()
	return state != nil && state.db == db && state.ready && time.Now().Before(state.expiresAt)
}

func (snapshot *runtimeSchemaSnapshot) refresh(ctx context.Context, db *gorm.DB, check func(context.Context, *gorm.DB) error) {
	if ctx == nil || ctx.Err() != nil || db == nil {
		snapshot.state.Store(&runtimeSchemaSnapshotState{db: db})
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := check(checkCtx, db.WithContext(checkCtx))
	snapshot.state.Store(&runtimeSchemaSnapshotState{
		db:        db,
		ready:     err == nil && checkCtx.Err() == nil,
		expiresAt: time.Now().Add(90 * time.Second),
	})
}

// RefreshRuntimeReadSchemas runs before HTTP starts and periodically on every node.
func RefreshRuntimeReadSchemas(ctx context.Context, db *gorm.DB) {
	shopReadSchema.refresh(ctx, db, kkaimigrate.CheckShopRuntimeSchema)
	invoiceReadSchema.refresh(ctx, db, kkaimigrate.CheckInvoiceRuntimeSchema)
	imageAsyncReadSchema.refresh(ctx, db, kkaimigrate.CheckImageAsyncRuntimeSchema)
	requestTicketReadSchema.refresh(ctx, db, kkaimigrate.CheckRequestTicketRuntimeSchema)
}

func ShopReadSchemaReady(ctx context.Context, db *gorm.DB) bool {
	return shopReadSchema.ready(ctx, db)
}

func InvoiceReadSchemaReady(ctx context.Context, db *gorm.DB) bool {
	return invoiceReadSchema.ready(ctx, db)
}

func RequestTicketReadSchemaReady(ctx context.Context, db *gorm.DB) bool {
	return requestTicketReadSchema.ready(ctx, db)
}
