package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestKKAIOutboxMySQL57MigrationAndClaim(t *testing.T) {
	db := mysql57KKAIIntegrationDB(t)

	_, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.NoError(t, kkaimigrate.Check(context.Background(), db, kkaimigrate.CurrentVersion))
	require.EqualValues(t, 191, mysql57EventKeyLength(t, db))

	now := time.Unix(1_784_211_072, 0)
	event := model.KKAIOutboxEvent{
		EventKey:    "mysql57:claim:1",
		Topic:       "mysql57.claim",
		AggregateID: "1",
		Payload:     `{}`,
		Status:      model.KKAIOutboxStatusPending,
		AvailableAt: now.Unix(),
		LastError:   "",
		CreatedAt:   now.Unix(),
	}
	require.NoError(t, db.Create(&event).Error)
	processor := NewKKAIOutboxProcessor(db, "mysql57-worker")
	processor.now = func() time.Time { return now }
	require.NoError(t, processor.Register(event.Topic, func(context.Context, model.KKAIOutboxEvent) error { return nil }))

	result, err := processor.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Delivered)
}

func TestKKAIOutboxMySQL57UpgradesLegacyEventKeyLength(t *testing.T) {
	db := mysql57KKAIIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&kkaimigrate.AppliedMigration{}))
	require.NoError(t, db.Exec(`CREATE TABLE kkai_outbox (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
event_key VARCHAR(192) NOT NULL UNIQUE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC`).Error)
	for _, migration := range kkaimigrate.Plan()[:3] {
		migration.AppliedAt = 1
		require.NoError(t, db.Create(&migration).Error)
	}
	require.EqualValues(t, 192, mysql57EventKeyLength(t, db))

	_, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.EqualValues(t, 191, mysql57EventKeyLength(t, db))
}

func mysql57KKAIIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("KKAI_TEST_MYSQL57_DSN"))
	if dsn == "" {
		t.Skip("KKAI_TEST_MYSQL57_DSN is not configured")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(config.DBName, "kkai_test_"), "integration DSN must use a dedicated kkai_test_* database")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	resetMySQL57KKAIIntegrationSchema(t, db)
	t.Cleanup(func() { resetMySQL57KKAIIntegrationSchema(t, db) })
	return db
}

func mysql57EventKeyLength(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var eventKeyLength int64
	require.NoError(t, db.Raw(
		"SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		"kkai_outbox",
		"event_key",
	).Scan(&eventKeyLength).Error)
	return eventKeyLength
}

func resetMySQL57KKAIIntegrationSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{
		"kkai_internal_balance_adjustments",
		"kkai_policy_incidents",
		"kkai_job_leases",
		"kkai_outbox",
		"kkai_schema_migrations",
	} {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table).Error)
	}
}
