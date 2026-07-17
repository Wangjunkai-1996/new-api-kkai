package topuprecovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type recoveryUser struct {
	ID        int64
	InviterID int64
}

func (recoveryUser) TableName() string {
	return "users"
}

type fakeProvider struct {
	orders map[string]ProviderOrder
}

func (provider *fakeProvider) Lookup(_ context.Context, tradeNo string) (ProviderOrder, error) {
	order, ok := provider.orders[tradeNo]
	if !ok {
		return ProviderOrder{}, ErrInvalidProviderEvidence
	}
	return order, nil
}

func TestRecoveryPlanApplyVerifyAndReplay(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("a", 40))
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	require.Len(t, manifest.Orders, 1)
	assert.EqualValues(t, 444, manifest.Orders[0].TopUpID)
	assert.EqualValues(t, 1_100, manifest.Orders[0].CompletedAt)
	require.NoError(t, ValidateManifest(manifest, manifest.SHA256, strings.Repeat("a", 40)))

	first, err := service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Equal(t, 1, first.UpdatedCount)
	assert.Zero(t, first.AlreadySetCount)
	assertTopUpCompletion(t, db, 444, 1_100)

	second, err := service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Zero(t, second.UpdatedCount)
	assert.Equal(t, 1, second.AlreadySetCount)
	assertTopUpCompletion(t, db, 444, 1_100)

	verified, err := service.Verify(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Equal(t, 1, verified.VerifiedCount)
}

func TestRecoveryRejectsManifestAndProviderDriftWithoutWriting(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("b", 40))
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)

	tampered := *manifest
	tampered.GeneratedAt++
	_, err = service.Apply(context.Background(), &tampered, manifest.SHA256)
	require.ErrorIs(t, err, ErrInvalidManifest)
	assertTopUpCompletion(t, db, 444, 0)

	provider.orders["trade-444"] = successfulProviderOrder("trade-444", 1_101)
	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider evidence drifted")
	assertTopUpCompletion(t, db, 444, 0)
}

func TestRecoveryFailsClosedForUnsupportedProviderAndExcludesUninvitedOrders(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderStripe)
	provider := &fakeProvider{orders: map[string]ProviderOrder{}}
	service := New(db, provider, strings.Repeat("c", 40))
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	_, err := service.Plan(context.Background(), 444, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider evidence")

	require.NoError(t, db.Model(&recoveryUser{}).Where("id = ?", 1).Update("inviter_id", 0).Error)
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	assert.Empty(t, manifest.Orders)
}

func newRecoveryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &recoveryUser{}))
	return db
}

func createEligibleTopUp(t *testing.T, db *gorm.DB, id int, tradeNo, provider string) {
	t.Helper()
	require.NoError(t, db.Create(&recoveryUser{ID: 1, InviterID: 2}).Error)
	require.NoError(t, db.Create(&model.TopUp{
		Id:              id,
		UserId:          1,
		TradeNo:         tradeNo,
		PaymentProvider: provider,
		CreateTime:      1_000,
		CompleteTime:    0,
		Status:          common.TopUpStatusSuccess,
	}).Error)
}

func successfulProviderOrder(tradeNo string, completedAt int64) ProviderOrder {
	return ProviderOrder{
		Code:           1,
		Status:         1,
		TradeNo:        "provider-" + tradeNo,
		ServiceTradeNo: tradeNo,
		PaymentType:    "alipay",
		EndTime:        "1970-01-01 08:18:20",
		CompletedAt:    completedAt,
	}
}

func assertTopUpCompletion(t *testing.T, db *gorm.DB, id int, expected int64) {
	t.Helper()
	stored := model.TopUp{}
	require.NoError(t, db.First(&stored, id).Error)
	assert.Equal(t, expected, stored.CompleteTime)
	assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
}
