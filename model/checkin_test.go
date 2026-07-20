package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserCheckinWithTransactionRollsBackOnQuotaOverflow(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Checkin{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Checkin{}).Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Checkin{}).Error)
		require.NoError(t, DB.Exec("DELETE FROM users").Error)
	})

	user := User{
		Id:       901,
		Username: "checkin-overflow-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    math.MaxInt64,
	}
	require.NoError(t, DB.Create(&user).Error)
	checkin := &Checkin{
		UserId:       user.Id,
		CheckinDate:  "2026-07-20",
		QuotaAwarded: 1,
		CreatedAt:    1,
	}

	_, err := userCheckinWithTransaction(checkin, user.Id, checkin.QuotaAwarded)
	require.Error(t, err)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, int64(math.MaxInt64), stored.Quota)
	var checkinCount int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&checkinCount).Error)
	require.Zero(t, checkinCount)
}
