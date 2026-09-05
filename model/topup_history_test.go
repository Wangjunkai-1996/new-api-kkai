package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTopUpTodayPaymentTotal(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	start := time.Date(2026, time.September, 5, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	orders := []TopUp{
		{UserId: 7, TradeNo: "at-midnight", Money: 15, Status: common.TopUpStatusSuccess, CompleteTime: start.Unix()},
		{UserId: 7, TradeNo: "before-next-midnight", Money: 37.5, Status: common.TopUpStatusSuccess, CompleteTime: end.Unix() - 1},
		{UserId: 7, TradeNo: "created-last-month", Money: 8.25, Status: common.TopUpStatusSuccess, CreateTime: start.AddDate(0, 0, -31).Unix(), CompleteTime: now.Unix()},
		{UserId: 8, TradeNo: "another-user", Money: 20, Status: common.TopUpStatusSuccess, CompleteTime: now.Unix()},
		{UserId: 0, TradeNo: "zero-user", Money: 1.25, Status: common.TopUpStatusSuccess, CompleteTime: now.Unix()},
		{UserId: 7, TradeNo: "before-midnight", Money: 100, Status: common.TopUpStatusSuccess, CompleteTime: start.Unix() - 1},
		{UserId: 7, TradeNo: "at-next-midnight", Money: 200, Status: common.TopUpStatusSuccess, CompleteTime: end.Unix()},
		{UserId: 7, TradeNo: "pending", Money: 300, Status: common.TopUpStatusPending, CompleteTime: now.Unix()},
		{UserId: 7, TradeNo: "expired", Money: 400, Status: common.TopUpStatusExpired, CompleteTime: now.Unix()},
		{UserId: 7, TradeNo: "failed", Money: 500, Status: common.TopUpStatusFailed, CompleteTime: now.Unix()},
	}
	require.NoError(t, db.Create(&orders).Error)

	userID, zeroUserID, emptyUserID := 7, 0, 99
	for _, tc := range []struct {
		name   string
		userID *int
		want   float64
	}{
		{name: "admin includes all users", want: 82},
		{name: "user includes only own payments", userID: &userID, want: 60.75},
		{name: "zero user ID stays scoped", userID: &zeroUserID, want: 1.25},
		{name: "no payments returns zero", userID: &emptyUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			total, err := GetTopUpTodayPaymentTotal(tc.userID, now)
			require.NoError(t, err)
			assert.Equal(t, tc.want, total)
		})
	}
}
