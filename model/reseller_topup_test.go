package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerTopUpFinalizationCreatesPaidLotWithoutInviteOutbox(t *testing.T) {
	// A payment started before the routing switch was disabled must still be
	// credited atomically when its signed callback reaches the platform Host.
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&CustomerPaymentLot{}))
	user := User{Id: 43, TenantID: 2, Username: "reseller-customer", AffCode: "reseller-customer", Quota: 0}
	require.NoError(t, db.Create(&user).Error)
	topup := TopUp{UserId: user.Id, TenantID: 2, TradeNo: "tenant-payment", Amount: 10, Money: 999, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, topup.SetCNYPaymentSnapshot("10.01"))
	require.NoError(t, topup.Insert())
	input := FinalizeTopUpInput{TradeNo: topup.TradeNo, ExpectedProvider: PaymentProviderEpay, CompletedAt: 100, Prepare: func(*TopUp, *User) (TopUpCompletion, error) { return TopUpCompletion{QuotaDelta: 500}, nil }}
	_, err := FinalizeTopUp(input)
	require.NoError(t, err)
	second, err := FinalizeTopUp(input)
	require.NoError(t, err)
	assert.True(t, second.AlreadyCompleted)
	var lot CustomerPaymentLot
	require.NoError(t, db.First(&lot, "top_up_id = ?", topup.Id).Error)
	assert.Equal(t, int64(1001), lot.PaidMinor)
	assert.Equal(t, int64(17), lot.PaymentFeeMinor)
	assert.Equal(t, int64(500), lot.CreditedQuota)
	var outboxCount, lotCount int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&outboxCount).Error)
	require.NoError(t, db.Model(&CustomerPaymentLot{}).Count(&lotCount).Error)
	assert.Zero(t, outboxCount)
	assert.Equal(t, int64(1), lotCount)
}

func TestResellerTopUpRejectsNonCNYAndMissingSnapshot(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	user := User{Id: 43, TenantID: 2, Username: "reseller-customer", AffCode: "reseller-customer"}
	require.NoError(t, db.Create(&user).Error)
	for _, topup := range []TopUp{
		{TenantID: 2, PaymentProvider: PaymentProviderStripe, Currency: "USD", PaidMinor: 100},
		{TenantID: 2, PaymentProvider: PaymentProviderEpay, Currency: ResellerCurrencyCNY},
		{TenantID: 0, PaymentProvider: PaymentProviderEpay, Currency: ResellerCurrencyCNY, PaidMinor: 100},
	} {
		topup.UserId = user.Id
		assert.ErrorIs(t, topup.Insert(), ErrResellerFinanceInvalidInput)
	}
	for _, amount := range []string{"0.001", "-1", "NaN", "1e1000", "92233720368547758.08"} {
		_, err := ParseCNYMinor(amount)
		assert.Error(t, err, amount)
	}
	minor, err := ParseCNYMinor("90071992547409.91")
	require.NoError(t, err)
	assert.Equal(t, int64(9007199254740991), minor)
}

func TestResellerTopUpLotFailureRollsBackOrderAndQuota(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	// Deliberately omit the lot table: finalization must roll back the old quota path too.
	user := User{Id: 43, TenantID: 2, Username: "reseller-customer", AffCode: "reseller-customer", Quota: 5}
	require.NoError(t, db.Create(&user).Error)
	topup := TopUp{TenantID: 2, UserId: user.Id, TradeNo: "rollback-tenant", PaymentProvider: PaymentProviderEpay, Currency: ResellerCurrencyCNY, PaidMinor: 100, Status: common.TopUpStatusPending}
	require.NoError(t, topup.Insert())
	_, err := FinalizeTopUp(FinalizeTopUpInput{TradeNo: topup.TradeNo, Prepare: func(*TopUp, *User) (TopUpCompletion, error) { return TopUpCompletion{QuotaDelta: 500}, nil }})
	require.Error(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&topup, topup.Id).Error)
	assert.Equal(t, int64(5), user.Quota)
	assert.Equal(t, common.TopUpStatusPending, topup.Status)
}
