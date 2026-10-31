package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRuntimeSchemaSnapshotDoesNotBlockReadersDuringRefresh(t *testing.T) {
	var snapshot runtimeSchemaSnapshot
	db := invoiceSchemaTestDB()
	snapshot.refresh(context.Background(), db, func(context.Context, *gorm.DB) error { return nil })
	require.True(t, snapshot.ready(context.Background(), db))

	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		snapshot.refresh(context.Background(), db, func(ctx context.Context, scoped *gorm.DB) error {
			close(started)
			<-release
			assert.Same(t, ctx, scoped.Statement.Context)
			return context.DeadlineExceeded
		})
	}()
	<-started
	assert.True(t, snapshot.ready(context.Background(), db))
	close(release)
	<-done
	assert.False(t, snapshot.ready(context.Background(), db), "a failed refresh must revoke readiness")
}

func TestRuntimeSchemaSnapshotFailsClosedForOtherDatabaseAndExpiry(t *testing.T) {
	var snapshot runtimeSchemaSnapshot
	first, second := invoiceSchemaTestDB(), invoiceSchemaTestDB()
	snapshot.refresh(context.Background(), first, func(context.Context, *gorm.DB) error { return nil })
	assert.False(t, snapshot.ready(context.Background(), second))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, snapshot.ready(canceled, first))
	snapshot.state.Store(&runtimeSchemaSnapshotState{db: first, ready: true, expiresAt: time.Now().Add(-time.Second)})
	assert.False(t, snapshot.ready(context.Background(), first))
}

func TestRuntimeReadSchemaPrewarmUsesDomainChecks(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	ctx := context.Background()
	RefreshRuntimeReadSchemas(ctx, db)
	assert.False(t, ShopReadSchemaReady(ctx, db), "an uninstalled shop stays unavailable")
	assert.True(t, InvoiceReadSchemaReady(ctx, db))
	assert.True(t, ImageAsyncSchemaReady(ctx, db))
	assert.True(t, RequestTicketReadSchemaReady(ctx, db))
	require.NoError(t, db.Migrator().DropTable("request_ticket_events"))
	RefreshRuntimeReadSchemas(ctx, db)
	assert.False(t, RequestTicketReadSchemaReady(ctx, db), "refresh revokes tickets when their storage is damaged")
	assert.True(t, InvoiceReadSchemaReady(ctx, db), "ticket damage does not block unrelated domains")
}
