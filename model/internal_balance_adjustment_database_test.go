package model

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInternalBalanceAdjustmentExternalDatabaseCompatibility(t *testing.T) {
	tests := []struct {
		name         string
		environment  string
		databaseType common.DatabaseType
		open         func(string) gorm.Dialector
	}{
		{
			name:         "mysql",
			environment:  "TEST_MYSQL_DSN",
			databaseType: common.DatabaseTypeMySQL,
			open:         func(dsn string) gorm.Dialector { return mysql.Open(dsn) },
		},
		{
			name:         "postgres",
			environment:  "TEST_POSTGRES_DSN",
			databaseType: common.DatabaseTypePostgreSQL,
			open:         func(dsn string) gorm.Dialector { return postgres.Open(dsn) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.environment)
			if dsn == "" {
				t.Skipf("%s is not configured", test.environment)
			}
			db, err := gorm.Open(test.open(dsn), &gorm.Config{})
			require.NoError(t, err)
			if db.Migrator().HasTable(&User{}) || db.Migrator().HasTable(&InternalBalanceAdjustment{}) {
				t.Skip("refusing to modify a database containing NewAPI balance tables")
			}

			originalDB := DB
			originalMainDatabaseType := common.MainDatabaseType()
			originalLogDatabaseType := common.LogDatabaseType()
			originalRedisEnabled := common.RedisEnabled
			originalCacheSync := syncInternalBalanceAdjustmentUserCache
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&InternalBalanceAdjustment{})
				_ = db.Migrator().DropTable(&User{})
				sqlDB, dbErr := db.DB()
				if dbErr == nil {
					_ = sqlDB.Close()
				}
				DB = originalDB
				SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
				common.RedisEnabled = originalRedisEnabled
				syncInternalBalanceAdjustmentUserCache = originalCacheSync
			})

			DB = db
			SetDatabaseTypes(test.databaseType, test.databaseType)
			common.RedisEnabled = false
			syncInternalBalanceAdjustmentUserCache = func(int, int64) error { return nil }
			require.NoError(t, db.AutoMigrate(&User{}, &InternalBalanceAdjustment{}))
			require.NoError(t, db.Create(&User{
				Id:       901,
				Username: "external-balance-901",
				Password: "not-a-real-password",
				Quota:    100,
				Status:   common.UserStatusEnabled,
				Group:    "default",
				AffCode:  "external-aff-901",
			}).Error)

			input := InternalBalanceAdjustmentInput{
				OperationID:   "external-credit-901",
				UserID:        901,
				Delta:         25,
				Reason:        "invitation_reward",
				Metadata:      `{}`,
				PayloadSHA256: strings.Repeat("b", 64),
				CreatedAt:     1,
			}
			first, err := ApplyInternalBalanceAdjustment(input)
			require.NoError(t, err)
			assert.False(t, first.Replayed)
			assert.Equal(t, int64(125), first.Adjustment.BalanceAfter)
			replay, err := ApplyInternalBalanceAdjustment(input)
			require.NoError(t, err)
			assert.True(t, replay.Replayed)

			input.PayloadSHA256 = strings.Repeat("c", 64)
			_, err = ApplyInternalBalanceAdjustment(input)
			assert.True(t, errors.Is(err, ErrBalanceAdjustmentIdempotencyConflict))

			concurrentInput := InternalBalanceAdjustmentInput{
				OperationID:   "external-concurrent-credit-901",
				UserID:        901,
				Delta:         10,
				Reason:        "invitation_reward",
				Metadata:      `{}`,
				PayloadSHA256: strings.Repeat("d", 64),
				CreatedAt:     2,
			}
			const concurrentRequests = 8
			results := make(chan *InternalBalanceAdjustmentResult, concurrentRequests)
			errorsByRequest := make(chan error, concurrentRequests)
			var waitGroup sync.WaitGroup
			for range concurrentRequests {
				waitGroup.Add(1)
				go func() {
					defer waitGroup.Done()
					result, applyErr := ApplyInternalBalanceAdjustment(concurrentInput)
					if applyErr != nil {
						errorsByRequest <- applyErr
						return
					}
					results <- result
				}()
			}
			waitGroup.Wait()
			close(results)
			close(errorsByRequest)
			var requestErrors []error
			for requestErr := range errorsByRequest {
				requestErrors = append(requestErrors, requestErr)
			}
			assert.Empty(t, requestErrors)
			var firstApplications int
			for result := range results {
				if !result.Replayed {
					firstApplications++
				}
			}
			assert.Equal(t, 1, firstApplications)

			originalOperationID := "external-credit-901"
			reversalInput := InternalBalanceAdjustmentInput{
				OperationID:         "external-reversal-901",
				UserID:              901,
				Delta:               -25,
				Reason:              "invitation_reward_reversal",
				Metadata:            `{"original_operation_id":"external-credit-901"}`,
				PayloadSHA256:       strings.Repeat("e", 64),
				OriginalOperationID: &originalOperationID,
				CreatedAt:           3,
			}
			reversal, err := ApplyInternalBalanceAdjustment(reversalInput)
			require.NoError(t, err)
			assert.False(t, reversal.Replayed)
			reversalInput.OperationID = "external-second-reversal-901"
			reversalInput.PayloadSHA256 = strings.Repeat("f", 64)
			_, err = ApplyInternalBalanceAdjustment(reversalInput)
			assert.True(t, errors.Is(err, ErrBalanceAdjustmentReversalConflict))

			var user User
			require.NoError(t, db.First(&user, 901).Error)
			assert.Equal(t, 110, user.Quota)
		})
	}
}
