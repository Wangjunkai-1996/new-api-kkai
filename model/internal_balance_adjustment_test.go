package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInternalBalanceAdjustmentModelTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalCacheSync := syncInternalBalanceAdjustmentUserCache
	t.Cleanup(func() {
		DB = originalDB
		SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
		syncInternalBalanceAdjustmentUserCache = originalCacheSync
	})

	SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&User{}, &InternalBalanceAdjustment{}))
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestInternalBalanceAdjustmentSynchronizesCacheOnlyAfterFirstCommit(t *testing.T) {
	db := setupInternalBalanceAdjustmentModelTest(t)
	require.NoError(t, db.Create(&User{
		Id:       801,
		Username: "cache-invalidation-801",
		Password: "not-a-real-password",
		Quota:    100,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "cache-aff-801",
	}).Error)

	type cacheSync struct {
		userID int
		delta  int64
	}
	cacheSyncs := make([]cacheSync, 0, 1)
	syncInternalBalanceAdjustmentUserCache = func(userID int, delta int64) error {
		var user User
		require.NoError(t, db.First(&user, userID).Error)
		require.Equal(t, 125, user.Quota)
		cacheSyncs = append(cacheSyncs, cacheSync{userID: userID, delta: delta})
		return nil
	}
	input := InternalBalanceAdjustmentInput{
		OperationID:   "cache-credit-801",
		UserID:        801,
		Delta:         25,
		Reason:        "invitation_reward",
		Metadata:      `{}`,
		PayloadSHA256: strings.Repeat("a", 64),
		CreatedAt:     1,
	}

	first, err := ApplyInternalBalanceAdjustment(input)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	replay, err := ApplyInternalBalanceAdjustment(input)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	assert.Equal(t, []cacheSync{{userID: 801, delta: 25}}, cacheSyncs)

	var user User
	require.NoError(t, db.First(&user, 801).Error)
	assert.Equal(t, 125, user.Quota)
}

func TestInternalBalanceAdjustmentModelRejectsUnsafeInputBeforeWriting(t *testing.T) {
	db := setupInternalBalanceAdjustmentModelTest(t)
	unsafeInputs := []InternalBalanceAdjustmentInput{
		{
			OperationID:   "negative-credit",
			UserID:        1,
			Delta:         -1,
			Reason:        InternalBalanceAdjustmentReasonCredit,
			Metadata:      `{}`,
			PayloadSHA256: strings.Repeat("a", 64),
			CreatedAt:     1,
		},
		{
			OperationID:   "oversized-credit",
			UserID:        1,
			Delta:         int64(math.MaxInt32) + 1,
			Reason:        InternalBalanceAdjustmentReasonCredit,
			Metadata:      `{}`,
			PayloadSHA256: strings.Repeat("b", 64),
			CreatedAt:     1,
		},
		{
			OperationID:   "invalid-payload-hash",
			UserID:        1,
			Delta:         1,
			Reason:        InternalBalanceAdjustmentReasonCredit,
			Metadata:      `{}`,
			PayloadSHA256: strings.Repeat("z", 64),
			CreatedAt:     1,
		},
	}
	for _, input := range unsafeInputs {
		_, err := ApplyInternalBalanceAdjustment(input)
		assert.True(t, errors.Is(err, ErrBalanceAdjustmentInvalidInput))
	}

	var count int64
	require.NoError(t, db.Model(&InternalBalanceAdjustment{}).Count(&count).Error)
	assert.Zero(t, count)
}
