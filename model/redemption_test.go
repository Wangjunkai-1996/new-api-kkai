package model

import (
	"math"
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &KKAIOutboxEvent{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&KKAIOutboxEvent{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&KKAIOutboxEvent{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, int64(500), user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)
	var outbox KKAIOutboxEvent
	require.NoError(t, DB.Where("event_key = ?", "newapi:redemption:"+strconv.Itoa(redemption.Id)).First(&outbox).Error)
	assert.Equal(t, strconv.Itoa(redemption.Id), outbox.AggregateID)
	var payload TopUpCompletedEvent
	require.NoError(t, common.Unmarshal([]byte(outbox.Payload), &payload))
	assert.Equal(t, "newapi:redemption:"+strconv.Itoa(redemption.Id), payload.EventKey)
	assert.Equal(t, PaymentProviderRedemption, payload.PaymentProvider)
	assert.EqualValues(t, redemption.Id, payload.SourceOrderID)
	assert.EqualValues(t, userId, payload.InviteeID)
	assert.EqualValues(t, 500, payload.CreditedQuota)
	assert.Nil(t, payload.InviterID)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, int64(500), user.Quota)
	var outboxCount int64
	require.NoError(t, DB.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)
}

func TestRedeemCapturesInviterIdentityInOutbox(t *testing.T) {
	userId, key := setupRedeemFixture(t, 1_000)
	inviter := User{Username: "redeem-inviter", Password: "password", Status: common.UserStatusEnabled, Group: "vip", AffCode: "redeem-inviter-code"}
	require.NoError(t, DB.Create(&inviter).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("inviter_id", inviter.Id).Error)

	_, err := Redeem(key, userId)
	require.NoError(t, err)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	var outbox KKAIOutboxEvent
	require.NoError(t, DB.Where("event_key = ?", "newapi:redemption:"+strconv.Itoa(redemption.Id)).First(&outbox).Error)
	var payload TopUpCompletedEvent
	require.NoError(t, common.Unmarshal([]byte(outbox.Payload), &payload))
	require.NotNil(t, payload.InviterID)
	assert.EqualValues(t, inviter.Id, *payload.InviterID)
	assert.Equal(t, "vip", payload.InviterGroup)
}

func TestRedeemOverflowRollsBackWalletAndCode(t *testing.T) {
	userId, key := setupRedeemFixture(t, 1)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", math.MaxInt64).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	require.Equal(t, int64(math.MaxInt64), user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
	require.Zero(t, redemption.RedeemedTime)
	require.Zero(t, redemption.UsedUserId)
	var outboxCount int64
	require.NoError(t, DB.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestRedeemRollsBackCodeAndQuotaWhenOutboxInsertFails(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)
	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	require.NoError(t, DB.Create(&KKAIOutboxEvent{
		EventKey:    "newapi:redemption:" + strconv.Itoa(redemption.Id),
		Topic:       "conflict",
		Payload:     "{}",
		Status:      KKAIOutboxStatusPending,
		AvailableAt: 1,
		CreatedAt:   1,
	}).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Zero(t, user.Quota)
	require.NoError(t, DB.First(&redemption, redemption.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
	assert.Zero(t, redemption.RedeemedTime)
	assert.Zero(t, redemption.UsedUserId)
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, int64(300), user.Quota, "quota must be credited exactly once")
	var outboxCount int64
	require.NoError(t, DB.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)
}
