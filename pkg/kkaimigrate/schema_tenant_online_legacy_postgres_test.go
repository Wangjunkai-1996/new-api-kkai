package kkaimigrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// These schema-11 write projections deliberately omit tenant/cash columns.
// They exercise old GORM SQL against every committed expansion boundary.
type tenantOnlineLegacyUser struct {
	ID        int64 `gorm:"primaryKey"`
	Username  string
	Email     string
	Quota     int64
	UsedQuota int64
}

func (tenantOnlineLegacyUser) TableName() string { return "users" }

type tenantOnlineLegacyTopUp struct {
	ID      int64 `gorm:"primaryKey"`
	UserID  int64
	TradeNo string
	Amount  int64
}

func (tenantOnlineLegacyTopUp) TableName() string { return "top_ups" }

type tenantOnlineLegacyToken struct {
	ID          int64 `gorm:"primaryKey"`
	UserID      int64
	Key         string
	RemainQuota int64
	UsedQuota   int64
	DeletedAt   gorm.DeletedAt
}

func (tenantOnlineLegacyToken) TableName() string { return "tokens" }

func TestTenantOnlinePostgresLegacyWritesAtEveryExpansionBoundary(t *testing.T) {
	rawDSN := os.Getenv("KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN")
	if rawDSN == "" {
		t.Skip("KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN is not set; real PostgreSQL acceptance was not run")
	}
	config, err := pgx.ParseConfig(rawDSN)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, config.Host)
	for _, fallback := range config.Fallbacks {
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
	}
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_"), "requires a dedicated local test database")
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	// Match chooseDB in both the old and current application. A driver's
	// prepared SELECT * cache cannot survive changed result columns.
	dialector, ok := db.Dialector.(*postgres.Dialector)
	require.True(t, ok)
	businessDB, err := gorm.Open(postgres.New(postgres.Config{DSN: dialector.DSN, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	businessSQL, err := businessDB.DB()
	require.NoError(t, err)
	businessSQL.SetMaxOpenConns(1)
	businessSQL.SetMaxIdleConns(1)
	t.Cleanup(func() { assert.NoError(t, businessSQL.Close()) })
	// The shared migration fixture has intentionally minimal old tables.
	// Add only the old business columns used by these frozen projections.
	for _, entry := range []struct {
		model  any
		fields []string
	}{
		{&tenantOnlineLegacyUser{}, []string{"UsedQuota"}},
		{&tenantOnlineLegacyTopUp{}, []string{"UserID", "TradeNo", "Amount"}},
		{&tenantOnlineLegacyToken{}, []string{"UserID", "Key", "RemainQuota", "UsedQuota", "DeletedAt"}},
	} {
		for _, field := range entry.fields {
			if !db.Migrator().HasColumn(entry.model, field) {
				require.NoError(t, db.Migrator().AddColumn(entry.model, field))
			}
		}
	}

	// Each old request gets its own pooled connection, separate from the
	// migration's pinned session. No goroutine timing or sleep controls the test.
	writes := int64(0)
	writeLegacyRequest := func() error {
		writes++
		id := int64(1000) + writes
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return businessDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL lock_timeout='500ms'").Error; err != nil {
				return err
			}
			user := tenantOnlineLegacyUser{ID: id, Username: fmt.Sprintf("old-%d", id), Email: fmt.Sprintf("old-%d@example.test", id), Quota: 100, UsedQuota: 0}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
			if err := tx.Create(&tenantOnlineLegacyTopUp{ID: id, UserID: id, TradeNo: fmt.Sprintf("old-%d", id), Amount: 20}).Error; err != nil {
				return err
			}
			if err := tx.Create(&tenantOnlineLegacyToken{ID: id, UserID: id, Key: fmt.Sprintf("old-key-%d", id), RemainQuota: 120}).Error; err != nil {
				return err
			}
			if err := tx.Model(&user).Updates(map[string]any{"quota": 120, "used_quota": 5}).Error; err != nil {
				return err
			}
			var loaded tenantOnlineLegacyUser
			if err := tx.First(&loaded, id).Error; err != nil {
				return err
			}
			if loaded.Quota != 120 || loaded.UsedQuota != 5 {
				return fmt.Errorf("old request read an inconsistent committed balance")
			}
			return nil
		})
	}
	require.NoError(t, writeLegacyRequest(), "pristine schema-11 business request")
	const callback = "tenant_online_legacy_acceptance"
	require.NoError(t, db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != (tenantOnlineReceipt{}).TableName() {
			return
		}
		if err := writeLegacyRequest(); err != nil {
			tx.AddError(fmt.Errorf("old request after expansion step %d: %w", writes-1, err))
		}
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Update().Remove(callback)) })
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(len(tenantOnlineSteps())+1), writes, "one real old request at every additive step boundary")
	require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))

	var users []model.User
	require.NoError(t, db.Where("id>=1000").Order("id").Find(&users).Error)
	require.Len(t, users, int(writes))
	for _, user := range users {
		assert.Equal(t, model.PlatformTenantID, user.TenantID)
		assert.Nil(t, user.NormalizedUsername)
		assert.Nil(t, user.NormalizedEmail)
		assert.Equal(t, int64(120), user.Quota)
		assert.Equal(t, int64(5), user.UsedQuota)
		var topup model.TopUp
		require.NoError(t, db.First(&topup, user.Id).Error)
		assert.Equal(t, model.PlatformTenantID, topup.TenantID)
		assert.Zero(t, topup.PaidMinor)
		assert.Empty(t, topup.Currency)
		var token model.Token
		require.NoError(t, db.First(&token, user.Id).Error)
		assert.Equal(t, model.PlatformTenantID, token.TenantID)
		assert.Equal(t, 120, token.RemainQuota)
	}
}
