package model

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMembershipRecoveryControlPostgresPauseBarrier(t *testing.T) {
	dsn := os.Getenv("TEST_MEMBERSHIP_CONTROL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_MEMBERSHIP_CONTROL_POSTGRES_DSN is not configured; PostgreSQL pause barrier was not run")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, config.Host)
	for _, fallback := range config.Fallbacks {
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
	}
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_"))
	config.RuntimeParams["statement_timeout"] = "5000"
	config.RuntimeParams["lock_timeout"] = "4000"
	adminSQL := stdlib.OpenDB(*config)
	admin, err := gorm.Open(postgres.New(postgres.Config{Conn: adminSQL}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "membership_control_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() {
		assert.NoError(t, admin.Exec(`DROP SCHEMA "`+schema+`" CASCADE`).Error)
		assert.NoError(t, adminSQL.Close())
	})
	config.RuntimeParams["search_path"] = schema
	connections := make([]*gorm.DB, 0, 2)
	for range 2 {
		sqlDB := stdlib.OpenDB(*config)
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		db, openErr := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, openErr)
		connections = append(connections, db)
		t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	}
	db, operator := connections[0], connections[1]
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	membershipRecoverySchema.Lock()
	previousChecker := membershipRecoverySchema.check
	membershipRecoverySchema.Unlock()
	RegisterMembershipBillingRecoverySchemaChecker(func(context.Context, *gorm.DB) error { return nil })
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		RegisterMembershipBillingRecoverySchemaChecker(previousChecker)
	})
	require.NoError(t, db.AutoMigrate(&Option{}, &User{}))
	// Use the actual User quota column and actual recovery transaction wrapper.
	require.NoError(t, db.Create(&User{Id: 1, Username: "control-test", AffCode: "control-test", Quota: 100}).Error)
	active := "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	paused := "paused:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, db.Create(&Option{Key: MembershipBillingRecoveryControlOption, Value: active}).Error)
	control, err := ParseMembershipRecoveryControl(active)
	require.NoError(t, err)
	var operatorPID, workerPID int
	require.NoError(t, operator.Raw("SELECT pg_backend_pid()").Scan(&operatorPID).Error)
	require.NoError(t, db.Raw("SELECT pg_backend_pid()").Scan(&workerPID).Error)
	require.NotEqual(t, workerPID, operatorPID)

	for _, cancelRound := range []bool{false, true} {
		t.Run(map[bool]string{false: "admitted_commit_before_pause", true: "cancelled_round_rolls_back"}[cancelRound], func(t *testing.T) {
			require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).Update("value", active).Error)
			require.NoError(t, db.Model(&User{}).Where("id = ?", 1).Update("quota", 100).Error)
			parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := WithMembershipRecoveryGuard(parent, func(tx *gorm.DB) error { return RequireMembershipRecoveryControlTx(tx, control) })
			entered, finish := make(chan struct{}), make(chan struct{})
			workerDone, pauseDone := make(chan error, 1), make(chan error, 1)
			go func() {
				workerDone <- membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
					if updateErr := tx.Model(&User{}).Where("id = ?", 1).Update("quota", gorm.Expr("quota + 30")).Error; updateErr != nil {
						return updateErr
					}
					close(entered)
					select {
					case <-finish:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-entered:
			case workerErr := <-workerDone:
				t.Fatalf("recovery transaction failed before reaching its body: %v", workerErr)
			case <-parent.Done():
				t.Fatal(parent.Err())
			}
			go func() {
				pauseDone <- operator.Model(&Option{}).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).Update("value", paused).Error
			}()
			// Prove an actual PostgreSQL wait edge, not a timing assumption.
			require.Eventually(t, func() bool {
				var blocked bool
				return admin.Raw("SELECT ? = ANY(pg_blocking_pids(?))", workerPID, operatorPID).Scan(&blocked).Error == nil && blocked
			}, time.Second, 5*time.Millisecond)
			if cancelRound {
				cancel()
			} else {
				close(finish)
			}
			workerErr := <-workerDone
			if cancelRound {
				require.Error(t, workerErr)
			} else {
				require.NoError(t, workerErr)
			}
			require.NoError(t, <-pauseDone)
			called := false
			oldRound := WithMembershipRecoveryGuard(context.Background(), func(tx *gorm.DB) error { return RequireMembershipRecoveryControlTx(tx, control) })
			err = membershipRecoveryTransaction(oldRound, func(tx *gorm.DB) error { called = true; return nil })
			require.ErrorIs(t, err, ErrMembershipRecoveryControl)
			assert.False(t, called, "paused control must reject the old generation before touching money")
			var quota int
			require.NoError(t, db.Model(&User{}).Where("id = ?", 1).Select("quota").Scan(&quota).Error)
			assert.Equal(t, map[bool]int{false: 130, true: 100}[cancelRound], quota)
			require.NoError(t, MembershipTransaction(context.Background(), func(tx *gorm.DB) error {
				return tx.Model(&User{}).Where("id = ?", 1).Update("quota", gorm.Expr("quota - 10")).Error
			}), "foreground accounting remains available during recovery pause")
		})
	}
}
