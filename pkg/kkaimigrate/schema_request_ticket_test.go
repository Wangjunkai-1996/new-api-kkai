package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareRequestTicketExpandFixture(t *testing.T, db *gorm.DB) Options {
	t.Helper()
	prepareResellerApplicationExpandFixture(t, db)
	options := Options{Offline: db.Dialector.Name() == DialectMySQL}
	_, err := ApplyResellerApplicationExpand(context.Background(), db, options)
	require.NoError(t, err)
	return options
}

func TestRequestTicketExpandPreservesHistoryAndBusinessData(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectPostgres, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				db = newImageAsyncDatabaseFixture(t, dialect)
			}
			options := prepareRequestTicketExpandFixture(t, db)
			ctx := context.Background()
			require.ErrorIs(t, CheckRequestTicketSchema(ctx, db), ErrSchemaNotReady)
			var before []AppliedMigration
			require.NoError(t, db.Order("version").Find(&before).Error)
			application := tenantV12ResellerApplication{ApplicantUserID: 101, BusinessName: "Existing", Status: "pending"}
			require.NoError(t, db.Create(&application).Error)
			dryOptions := options
			dryOptions.DryRun = true
			dry, err := ApplyRequestTicketExpand(ctx, db, dryOptions)
			require.NoError(t, err)
			require.Len(t, dry.Pending, 1)
			assert.EqualValues(t, 16, dry.Pending[0].Version)
			assert.False(t, db.Migrator().HasTable("request_tickets"))
			assert.False(t, db.Migrator().HasTable("request_ticket_events"))
			_, err = ApplyRequestTicketExpand(ctx, db, options)
			require.NoError(t, err)
			require.NoError(t, CheckRequestTicketSchema(ctx, db))
			require.NoError(t, CheckTenantOnlineSchema(ctx, db))
			require.NoError(t, CheckResellerApplicationSchema(ctx, db))
			if RuntimeMaxVersion >= 16 {
				require.NoError(t, CheckRequired(ctx, db))
			}
			var existing tenantV12ResellerApplication
			require.NoError(t, db.First(&existing, application.ID).Error)
			assert.Equal(t, application.BusinessName, existing.BusinessName)
			ticket := requestTicketV16{TenantID: 0, UserID: 101, Title: "Support Unicode 截图", Description: "A request", Status: "pending", Version: 1, CreatedAt: 1, UpdatedAt: 1}
			require.NoError(t, db.Create(&ticket).Error)
			require.Positive(t, ticket.ID)
			event := requestTicketEventV16{TenantID: 0, TicketID: ticket.ID, UserID: 101, ActorID: 1, Status: "accepted", Feedback: "感谢建议", CreatedAt: 2}
			require.NoError(t, db.Create(&event).Error)
			require.Positive(t, event.ID)
			_, err = ApplyRequestTicketExpand(ctx, db, options)
			require.NoError(t, err)
			var after []AppliedMigration
			require.NoError(t, db.Order("version").Find(&after).Error)
			require.Len(t, after, len(before)+1)
			assert.Equal(t, before, after[:len(before)])
			assert.EqualValues(t, 16, after[len(before)].Version)
			var saved requestTicketEventV16
			require.NoError(t, db.First(&saved, event.ID).Error)
			assert.Equal(t, event, saved)
			state, err := loadValidatedState(ctx, db, RequestTicketSchemaVersion)
			require.NoError(t, err)
			for _, item := range state.prefix {
				assert.NotEqualValues(t, 12, item.Version)
				assert.NotEqualValues(t, 13, item.Version)
			}
		})
	}
}

func TestRequestTicketExpandRequiresCanonicalSourceAndAuthorization(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareResellerApplicationExpandFixture(t, db)
	ctx := context.Background()
	_, err := ApplyRequestTicketExpand(ctx, db, Options{})
	assert.ErrorIs(t, err, ErrSchemaNotReady, "v15 must be applied independently")
	_, err = ApplyResellerApplicationExpand(ctx, db, Options{})
	require.NoError(t, err)
	denied := errors.New("request ticket controller proof denied")
	_, err = ApplyRequestTicketExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	assert.ErrorIs(t, err, denied)
	assert.False(t, db.Migrator().HasTable("request_tickets"))
	require.NoError(t, CheckResellerApplicationSchema(ctx, db))
}

func TestRequestTicketGateRejectsUnreceiptedTablesAndPhysicalDrift(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareRequestTicketExpandFixture(t, db)
	ctx := context.Background()
	item := migrationSet()[RequestTicketSchemaVersion-1]
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, item))
	assert.ErrorIs(t, CheckRequestTicketSchema(ctx, db), ErrSchemaNotReady, "DDL without a canonical receipt cannot enable tickets")
	_, err := ApplyRequestTicketExpand(ctx, db, Options{})
	require.NoError(t, err, "an exact interrupted expand is resumable")
	require.NoError(t, CheckRequestTicketSchema(ctx, db))
	require.NoError(t, db.Exec("DROP INDEX idx_request_ticket_events_unread").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_request_ticket_events_unread ON request_ticket_events(user_id, tenant_id, read_at, id)").Error)
	assert.ErrorIs(t, CheckRequestTicketSchema(ctx, db), ErrSchemaNotReady)
	_, err = ApplyRequestTicketExpand(ctx, db, Options{})
	assert.ErrorIs(t, err, ErrSchemaNotReady, "reruns must not silently repair drift")
}

func TestRequestTicketReceiptFailureRollsBackTransactionalDDL(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				db = newImageAsyncDatabaseFixture(t, dialect)
			}
			prepareRequestTicketExpandFixture(t, db)
			interrupted := errors.New("request ticket receipt write failed")
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:request-receipt-failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "kkai_schema_migrations" {
					tx.AddError(interrupted)
				}
			}))
			_, err := ApplyRequestTicketExpand(context.Background(), db, Options{})
			assert.ErrorIs(t, err, interrupted)
			assert.False(t, db.Migrator().HasTable("request_tickets"))
			assert.False(t, db.Migrator().HasTable("request_ticket_events"))
			require.NoError(t, CheckResellerApplicationSchema(context.Background(), db))
		})
	}
}
