package model

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTopUpFinalizeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	dsn := fmt.Sprintf("file:%s?_busy_timeout=30000&_journal_mode=WAL", filepath.Join(t.TempDir(), "topup.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &KKAIOutboxEvent{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestFinalizeTopUpConcurrentReplaySerializesOnTheOrder(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	input := FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	}
	results := make(chan *FinalizeTopUpResult, 2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := FinalizeTopUp(input)
			results <- result
			errors <- err
		}()
	}
	first, second := <-results, <-results
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	require.NotEqual(t, first.AlreadyCompleted, second.AlreadyCompleted)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(510), user.Quota)
}

func seedTopUpFinalizeFixture(t *testing.T, db *gorm.DB) TopUp {
	t.Helper()
	inviter := User{Id: 1001, Username: "inviter", Group: "vip", AffCode: "inviter-code"}
	invitee := User{Id: 3418, Username: "invitee", Group: "default", AffCode: "invitee-code", InviterId: inviter.Id, Quota: 10}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)
	topUp := TopUp{
		Id:              842,
		UserId:          invitee.Id,
		Amount:          75,
		Money:           75,
		TradeNo:         "trade-842",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      1_784_211_000,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(&topUp).Error)
	return topUp
}

func TestFinalizeTopUpCommitsOrderQuotaAndOutboxAtomically(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		CompletedAt:      1_784_211_072,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.NoError(t, err)
	require.False(t, result.AlreadyCompleted)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
	require.Equal(t, 75.0, storedTopUp.Money)
	require.EqualValues(t, 1_784_211_072, storedTopUp.CompleteTime)
	var storedUser User
	require.NoError(t, db.First(&storedUser, topUp.UserId).Error)
	require.Equal(t, int64(510), storedUser.Quota)
	var outbox KKAIOutboxEvent
	require.NoError(t, db.Where("event_key = ?", "newapi:topup:842").First(&outbox).Error)
	require.Equal(t, KKAIOutboxTopicTopUpCompleted, outbox.Topic)
	var payload TopUpCompletedEvent
	require.NoError(t, common.Unmarshal([]byte(outbox.Payload), &payload))
	require.Equal(t, 2, payload.SchemaVersion)
	require.EqualValues(t, 500, payload.CreditedQuota)
	require.Equal(t, "vip", payload.InviterGroup)
	require.NotNil(t, payload.InviterID)
	require.EqualValues(t, 1001, *payload.InviterID)
}

func TestFinalizeTopUpBeforeRebateBoundaryCreditsQuotaWithoutOutbox(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	t.Setenv(topUpRebateActiveFromIDEnv, "843")

	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		CompletedAt:      1_784_211_072,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.NoError(t, err)
	require.False(t, result.AlreadyCompleted)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
	require.EqualValues(t, 1_784_211_072, storedTopUp.CompleteTime)
	var storedUser User
	require.NoError(t, db.First(&storedUser, topUp.UserId).Error)
	require.Equal(t, int64(510), storedUser.Quota)
	var outboxCount int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestFinalizeTopUpInvalidRebateBoundaryRollsBack(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	t.Setenv(topUpRebateActiveFromIDEnv, "invalid")

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.ErrorIs(t, err, errTopUpRebateBoundaryInvalid)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
	require.Zero(t, storedTopUp.CompleteTime)
	var storedUser User
	require.NoError(t, db.First(&storedUser, topUp.UserId).Error)
	require.Equal(t, int64(10), storedUser.Quota)
}

func TestFinalizeTopUpReplayDoesNotDoubleCreditOrDuplicateOutbox(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	input := FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	}
	_, err := FinalizeTopUp(input)
	require.NoError(t, err)
	replayed, err := FinalizeTopUp(input)
	require.NoError(t, err)
	require.True(t, replayed.AlreadyCompleted)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(510), user.Quota)
	var count int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestFinalizeTopUpRollsBackWhenOutboxInsertFails(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Create(&KKAIOutboxEvent{
		EventKey:    "newapi:topup:842",
		Topic:       "conflict",
		Payload:     "{}",
		Status:      KKAIOutboxStatusPending,
		AvailableAt: 1,
		CreatedAt:   1,
	}).Error)

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.Error(t, err)
	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
	require.Zero(t, storedTopUp.CompleteTime)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(10), user.Quota)
}

func TestFinalizeTopUpAcceptsQuotaAboveRequestBillingRange(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	quotaDelta := int64(math.MaxInt32) + 1
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: quotaDelta}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, quotaDelta, result.QuotaDelta)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, quotaDelta+10, user.Quota)
}

func TestRechargeEpayCreditsConfiguredBigIntAmounts(t *testing.T) {
	tests := []struct {
		name          string
		amount        int64
		expectedQuota int64
	}{
		{name: "5000 units", amount: 5_000, expectedQuota: 2_500_000_000},
		{name: "10000 units", amount: 10_000, expectedQuota: 5_000_000_000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupTopUpFinalizeTestDB(t)
			topUp := seedTopUpFinalizeFixture(t, db)
			require.NoError(t, db.Model(&topUp).Update("amount", test.amount).Error)

			result, err := RechargeEpay(topUp.TradeNo, "alipay")
			require.NoError(t, err)
			require.Equal(t, test.expectedQuota, result.QuotaDelta)

			var user User
			require.NoError(t, db.First(&user, topUp.UserId).Error)
			require.Equal(t, test.expectedQuota+10, user.Quota)
		})
	}
}

func TestFinalizeTopUpCreditsMaximumInt64Balance(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", 0).Error)

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: math.MaxInt64}, nil
		},
	})
	require.NoError(t, err)

	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(math.MaxInt64), user.Quota)
}

func TestFinalizeTopUpCreditsBalanceAlreadyAboveMaxInt32(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	startingQuota := int64(math.MaxInt32) + 100
	require.NoError(t, db.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", startingQuota).Error)

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.NoError(t, err)

	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, startingQuota+500, user.Quota)
}

func TestFinalizeTopUpRejectsQuotaThatWouldOverflowInt64Balance(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	startingQuota := int64(math.MaxInt64 - 100)
	require.NoError(t, db.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", startingQuota).Error)

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 101}, nil
		},
	})
	require.ErrorIs(t, err, ErrTopUpQuotaInvalid)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, startingQuota, user.Quota)
}

func TestFinalizeTopUpMissingInviterDoesNotRollBackCredit(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Delete(&User{}, 1001).Error)

	_, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          topUp.TradeNo,
		ExpectedProvider: PaymentProviderEpay,
		Prepare: func(*TopUp, *User) (TopUpCompletion, error) {
			return TopUpCompletion{QuotaDelta: 500}, nil
		},
	})
	require.NoError(t, err)

	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(510), user.Quota)
	var outbox KKAIOutboxEvent
	require.NoError(t, db.Where("event_key = ?", "newapi:topup:842").First(&outbox).Error)
	var payload TopUpCompletedEvent
	require.NoError(t, common.Unmarshal([]byte(outbox.Payload), &payload))
	require.Nil(t, payload.InviterID)
	require.Equal(t, "default", payload.InviterGroup)
}

func TestManualTopUpCompletionUsesProviderQuotaSemantics(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		provider      string
		amount        int64
		money         float64
		expectedQuota int64
	}{
		{name: "epay amount units", provider: PaymentProviderEpay, amount: 2, money: 1.25, expectedQuota: 1_000_000},
		{name: "epay 5000 units", provider: PaymentProviderEpay, amount: 5_000, expectedQuota: 2_500_000_000},
		{name: "epay 10000 units", provider: PaymentProviderEpay, amount: 10_000, expectedQuota: 5_000_000_000},
		{name: "stripe paid money", provider: PaymentProviderStripe, amount: 2, money: 1.25, expectedQuota: 625_000},
		{name: "creem final quota", provider: PaymentProviderCreem, amount: 12_345, money: 1.25, expectedQuota: 12_345},
		{name: "waffo amount units", provider: PaymentProviderWaffo, amount: 2, money: 1.25, expectedQuota: 1_000_000},
		{name: "waffo pancake amount units", provider: PaymentProviderWaffoPancake, amount: 2, money: 1.25, expectedQuota: 1_000_000},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completion, err := prepareManualTopUpCompletion(&TopUp{
				PaymentProvider: testCase.provider,
				Amount:          testCase.amount,
				Money:           testCase.money,
			}, nil)
			require.NoError(t, err)
			require.Equal(t, testCase.expectedQuota, completion.QuotaDelta)
		})
	}
}

func TestTopUpQuotaFromDecimalUsesRoundedPositiveInt64Range(t *testing.T) {
	tests := []struct {
		name     string
		quota    decimal.Decimal
		expected int64
		wantErr  bool
	}{
		{name: "round down", quota: decimal.RequireFromString("41.4"), expected: 41},
		{name: "round half away", quota: decimal.RequireFromString("41.5"), expected: 42},
		{name: "max int64", quota: decimal.NewFromInt(math.MaxInt64), expected: math.MaxInt64},
		{name: "zero after rounding", quota: decimal.RequireFromString("0.4"), wantErr: true},
		{name: "negative", quota: decimal.NewFromInt(-1), wantErr: true},
		{name: "overflow", quota: decimal.NewFromInt(math.MaxInt64).Add(decimal.RequireFromString("0.5")), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quota, err := topUpQuotaFromDecimal(test.quota)
			if test.wantErr {
				require.ErrorIs(t, err, ErrTopUpQuotaInvalid)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, quota)
		})
	}
}

func TestManualTopUpCompletionRejectsMissingOrUnknownProvider(t *testing.T) {
	for _, provider := range []string{"", "unknown", PaymentProviderBalance} {
		_, err := prepareManualTopUpCompletion(&TopUp{
			PaymentProvider: provider,
			Amount:          2,
			Money:           1.25,
		}, nil)
		require.ErrorIs(t, err, ErrTopUpPaymentProviderInvalid)
	}
}

func TestManualCompleteTopUpRejectsUnknownProviderWithoutMutation(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Model(&topUp).Update("payment_provider", "unknown").Error)

	err := ManualCompleteTopUp(topUp.TradeNo, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpPaymentProviderInvalid)

	var storedTopUp TopUp
	require.NoError(t, db.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	require.Equal(t, int64(10), user.Quota)
	var outboxCount int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}
