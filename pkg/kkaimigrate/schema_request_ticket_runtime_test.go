package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRequestTicketRuntimeSchemaRequiresCanonicalReceiptAndTicketStorage(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				db = newShopSchemaDatabaseFixture(t, dialect)
			}
			ctx := context.Background()
			require.NoError(t, db.Migrator().CreateTable(&AppliedMigration{}))
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[RequestTicketSchemaVersion-1]))
			assert.ErrorIs(t, CheckRequestTicketRuntimeSchema(ctx, db), ErrSchemaNotReady, "DDL without canonical receipts cannot enable tickets")
			for _, item := range planItemsForDialect(dialect, RequestTicketSchemaVersion) {
				require.NoError(t, db.Create(&AppliedMigration{
					Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item),
				}).Error)
			}
			require.NoError(t, CheckRequestTicketRuntimeSchema(ctx, db), "v16 alone enables tickets")
			for _, item := range planItemsForDialect(dialect, ShopSchemaVersion) {
				if item.Version > RequestTicketSchemaVersion {
					require.NoError(t, db.Create(&AppliedMigration{
						Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item),
					}).Error)
				}
			}
			require.NoError(t, CheckRequestTicketRuntimeSchema(ctx, db), "later canonical receipts do not require unrelated physical tables")

			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", RequestTicketSchemaVersion).Update("checksum", "forged").Error)
			assert.ErrorIs(t, CheckRequestTicketRuntimeSchema(ctx, db), ErrChecksumMismatch)
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", RequestTicketSchemaVersion).Update("checksum", RequestTicketPlanChecksum()).Error)
			require.NoError(t, CheckRequestTicketRuntimeSchema(ctx, db))

			require.NoError(t, db.Migrator().DropTable("users"))
			assert.NoError(t, CheckRequestTicketRuntimeSchema(ctx, db), "unrelated storage cannot gate ticket requests")
			assert.ErrorIs(t, CheckRequestTicketSchema(ctx, db), ErrSchemaNotReady, "maintenance audit still validates other domains")

			require.NoError(t, db.Migrator().DropIndex("request_ticket_events", "idx_request_ticket_events_unread"))
			require.NoError(t, db.Exec("CREATE INDEX idx_request_ticket_events_unread ON request_ticket_events (user_id, tenant_id, read_at, id)").Error)
			assert.ErrorIs(t, CheckRequestTicketRuntimeSchema(ctx, db), ErrSchemaNotReady, "ticket ownership indexes must retain their correct column order")
			require.NoError(t, db.Migrator().DropIndex("request_ticket_events", "idx_request_ticket_events_unread"))
			require.NoError(t, db.Exec("CREATE INDEX idx_request_ticket_events_unread ON request_ticket_events (tenant_id, user_id, read_at, id)").Error)
			require.NoError(t, CheckRequestTicketRuntimeSchema(ctx, db))

			require.NoError(t, db.Exec("ALTER TABLE request_tickets DROP COLUMN title").Error)
			assert.ErrorIs(t, CheckRequestTicketRuntimeSchema(ctx, db), ErrSchemaNotReady, "ticket storage drift must fail closed")
		})
	}
}
