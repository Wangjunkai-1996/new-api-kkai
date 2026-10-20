package kkaimigrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Each configured DSN must permit creating a disposable database/schema. All
// fixtures live in a freshly named namespace; existing application tables are never changed.
func TestImageAsyncRealDatabaseContract(t *testing.T) {
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			db := newImageAsyncDatabaseFixture(t, dialect)
			ctx := context.Background()
			_, err := applyThroughVersion(ctx, db, Options{}, RC41ArchitectureSchemaVersion, ImageAsyncSchemaVersion)
			require.NoError(t, err)
			var baseline []AppliedMigration
			require.NoError(t, db.Order("version").Find(&baseline).Error)
			_, err = ApplyImageAsyncExpand(ctx, db, Options{DryRun: true})
			require.NoError(t, err)
			assert.False(t, db.Migrator().HasTable(&model.KKAIImageExecution{}))
			_, err = ApplyImageAsyncExpand(ctx, db, Options{})
			require.NoError(t, err)
			require.NoError(t, CheckImageAsyncSchema(ctx, db))
			_, err = ApplyImageAsyncExpand(ctx, db, Options{})
			require.NoError(t, err)
			var preserved []AppliedMigration
			require.NoError(t, db.Where("version <= ?", RC41ArchitectureSchemaVersion).Order("version").Find(&preserved).Error)
			assert.Equal(t, baseline, preserved)
			require.NoError(t, db.Table("users").Create(map[string]any{"id": 1, "quota": 1000}).Error)
			abort := errors.New("acceptance insert aborted")
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := model.LockImageStudioCapacityTx(tx); err != nil {
					return err
				}
				generation := model.KKAIImageGeneration{UserID: 1, TokenID: 1, ModelProfileID: 1, Model: "image-test", Prompt: "test", Parameters: "{}", RequestHash: "hash", RequestID: "atomic-db-test", Status: model.ImageGenerationStatusSubmitting, BillingState: model.ImageGenerationBillingStatePending, RequestedCount: 1}
				if err := tx.Create(&generation).Error; err != nil {
					return err
				}
				if _, err := model.ReserveImageGenerationBillingTx(tx, generation.ID, model.TaskBillingSourceWallet, 300); err != nil {
					return err
				}
				return abort
			})
			require.ErrorIs(t, err, abort)
			var quota int64
			require.NoError(t, db.Table("users").Where("id = ?", 1).Pluck("quota", &quota).Error)
			assert.EqualValues(t, 1000, quota)
			for _, table := range []string{"kkai_image_generations", "kkai_outbox"} {
				var count int64
				require.NoError(t, db.Table(table).Count(&count).Error)
				assert.Zero(t, count, table)
			}
			now := time.Now().Unix()
			leader := model.KKAIJobLease{LeaseName: "image-test-leader", Holder: "writer", Fence: 2, LeaseUntil: now + 300}
			require.NoError(t, db.Create(&leader).Error)
			event := model.KKAIOutboxEvent{EventKey: "image-generate-test", Topic: "image.generate.v1", AggregateID: "1", Payload: "{}", Status: model.KKAIOutboxStatusPending, LockedBy: "claim", LockedAt: now}
			require.NoError(t, db.Create(&event).Error)
			execution := model.KKAIImageExecution{GenerationID: 1, State: model.ImageGenerationExecutionPreparing, EventID: event.ID, Fence: event.LockedBy, Payload: "{}"}
			require.NoError(t, db.Create(&execution).Error)
			fence := model.ImageExecutionFence{EventID: event.ID, LockedBy: event.LockedBy, LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				if _, err := model.RequireImageExecutionFenceTx(tx, 1, fence); err != nil {
					return err
				}
				// A separate connection cannot change ownership before the fenced
				// business transaction commits. Assert the database lock error itself.
				contenderErr := db.Transaction(func(contender *gorm.DB) error {
					statement := "SET LOCAL lock_timeout = '100ms'"
					if dialect == DialectMySQL {
						statement = "SET SESSION innodb_lock_wait_timeout = 1"
					}
					if err := contender.Exec(statement).Error; err != nil {
						return err
					}
					return contender.Model(&leader).Update("fence", 3).Error
				})
				if dialect == DialectMySQL {
					var lockErr *mysqldriver.MySQLError
					require.ErrorAs(t, contenderErr, &lockErr)
					assert.EqualValues(t, 1205, lockErr.Number)
				} else {
					var lockErr *pgconn.PgError
					require.ErrorAs(t, contenderErr, &lockErr)
					assert.Equal(t, "55P03", lockErr.Code)
				}
				return nil
			}))
			require.NoError(t, db.Model(&leader).Update("fence", 3).Error)
			require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { _, err := model.RequireImageExecutionFenceTx(tx, 1, fence); return err }), model.ErrImageExecutionFenceLost)
		})
	}
}

func newImageAsyncDatabaseFixture(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	variable := "KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN"
	if dialect == DialectMySQL {
		variable = "KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN"
	}
	rawDSN := os.Getenv(variable)
	if rawDSN == "" {
		t.Skip(variable + " is not set")
	}
	previousDialect := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseType(dialect))
	t.Cleanup(func() { common.SetMainDatabaseType(previousDialect) })
	name := fmt.Sprintf("kkai_image_test_%d", time.Now().UnixNano())
	var admin, database gorm.Dialector
	create, drop := "CREATE SCHEMA "+name, "DROP SCHEMA "+name+" CASCADE"
	if dialect == DialectPostgres {
		admin = postgres.Open(rawDSN)
		database = postgres.Open(postgresDSNForRole(t, rawDSN, "", "", name))
	} else {
		parsed, err := mysqldriver.ParseDSN(rawDSN)
		require.NoError(t, err)
		parsed.DBName = ""
		admin = mysql.Open(parsed.FormatDSN())
		parsed.DBName = name
		database = mysql.Open(parsed.FormatDSN())
		create, drop = "CREATE DATABASE "+name+" CHARACTER SET utf8mb4", "DROP DATABASE "+name
	}
	adminDB, err := gorm.Open(admin, &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := adminDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminSQL.Close()) })
	require.NoError(t, adminDB.Exec(create).Error)
	t.Cleanup(func() { require.NoError(t, adminDB.Exec(drop).Error) })
	db, err := gorm.Open(database, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	optionKey := `"key"`
	if dialect == DialectMySQL {
		optionKey = "`key`"
	}
	for _, statement := range []string{
		"CREATE TABLE users (id BIGINT PRIMARY KEY, telegram_id TEXT, quota BIGINT NOT NULL DEFAULT 0, deleted_at TIMESTAMP NULL)",
		"CREATE TABLE tokens (id BIGINT PRIMARY KEY)",
		"CREATE TABLE passkey_credentials (id BIGINT PRIMARY KEY)",
		"CREATE TABLE options (" + optionKey + " VARCHAR(255) PRIMARY KEY, value TEXT)",
		"CREATE TABLE midjourneys (id BIGINT PRIMARY KEY, mj_id TEXT, channel_id BIGINT, quota BIGINT, status TEXT)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db
}
