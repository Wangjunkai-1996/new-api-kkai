package kkaimigrate

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestVideoWebhookExpansion(t *testing.T) {
	dialects := map[string]gorm.Dialector{"sqlite": sqlite.Open(":memory:")}
	if dsn := os.Getenv("KKAI_VIDEO_WEBHOOK_TEST_POSTGRES_DSN"); dsn != "" {
		dialects["postgres"] = postgres.Open(dsn)
	}
	if dsn := os.Getenv("KKAI_VIDEO_WEBHOOK_TEST_MYSQL_DSN"); dsn != "" {
		dialects["mysql"] = mysql.Open(dsn)
	}
	for name, dialect := range dialects {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			state, err := PlanVideoWebhookExpansion(db)
			require.NoError(t, err)
			require.Equal(t, "pristine", state, "requires a fresh disposable database")
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.VideoWebhookTask{}, &model.VideoWebhookEndpoint{}))
			})
			ctx := t.Context()
			require.ErrorIs(t, ApplyVideoWebhookExpansion(ctx, db, Options{}), ErrUnsafeMigration)
			require.NoError(t, ApplyVideoWebhookExpansion(ctx, db, Options{Offline: true}))
			require.NoError(t, CheckVideoWebhookSchema(db))
			require.NoError(t, ApplyVideoWebhookExpansion(ctx, db, Options{Offline: true}))
			sub := model.VideoWebhookTask{TaskRowID: 1, EventID: "evt_one", TaskID: "task_one", EndpointID: "wh_one", State: "waiting"}
			require.NoError(t, db.Create(&sub).Error)
			sub.TaskRowID = 2
			assert.Error(t, db.Create(&sub).Error, "event IDs must be unique")
			if name != "sqlite" {
				oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
				t.Cleanup(func() { common.SetDatabaseTypes(oldMain, oldLog) })
				databaseType := common.DatabaseTypeMySQL
				if name == "postgres" {
					databaseType = common.DatabaseTypePostgreSQL
				}
				common.SetDatabaseTypes(databaseType, databaseType)
				// Simulate a task transaction that established its snapshot before a
				// concurrent endpoint revocation (MySQL defaults to repeatable read).
				require.False(t, db.Migrator().HasTable(&model.Token{}), "test needs a disposable database")
				require.NoError(t, db.AutoMigrate(&model.Token{}))
				t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&model.Token{})) })
				owner := model.VideoWebhookOwner{TenantID: 17, UserID: 4, TokenID: 2}
				token := model.Token{Id: 2, UserId: 4, TenantID: 17, Key: "fixture"}
				require.NoError(t, db.Create(&token).Error)
				endpoint := model.VideoWebhookEndpoint{ID: "wh_stale", VideoWebhookOwner: owner, State: "active"}
				require.NoError(t, db.Create(&endpoint).Error)
				tx := db.Begin()
				require.NoError(t, tx.Error)
				defer tx.Rollback()
				var snapshot model.VideoWebhookEndpoint
				require.NoError(t, tx.First(&snapshot, "id = ?", endpoint.ID).Error)
				require.NoError(t, owner.ChangeEndpoint(ctx, db, "revoke", endpoint.ID, nil))
				task := model.Task{ID: 9, TaskID: "task_stale", TenantID: 17, UserId: 4, PrivateData: model.TaskPrivateData{TokenId: 2}}
				require.ErrorIs(t, owner.Subscribe(tx, &task, endpoint.ID, "evt_stale", "", "seedance-2.5"), model.ErrVideoWebhookNotReady)
				require.NoError(t, tx.Rollback().Error)
			}
			require.NoError(t, db.Migrator().DropIndex(&model.VideoWebhookTask{}, "idx_video_webhook_tasks_event_id"))
			assert.Error(t, CheckVideoWebhookSchema(db))
			assert.Error(t, ApplyVideoWebhookExpansion(ctx, db, Options{Offline: true}), "do not silently adopt a damaged table")
		})
	}
}
