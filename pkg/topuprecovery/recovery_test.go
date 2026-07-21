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
	Group     string
	Quota     int64
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
	service := New(db, provider, strings.Repeat("a", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	require.Len(t, manifest.Orders, 1)
	assert.EqualValues(t, 444, manifest.Orders[0].TopUpID)
	assert.EqualValues(t, 1_100, manifest.Orders[0].CompletedAt)
	assert.Equal(t, "500000", manifest.QuotaPerUnit)
	assert.EqualValues(t, 100_000_000, manifest.Orders[0].CreditedQuota)
	assert.EqualValues(t, 2, manifest.Orders[0].InviterID)
	assert.Equal(t, "vip", manifest.Orders[0].InviterGroup)
	assert.Equal(t, "newapi:topup:444", manifest.Orders[0].EventKey)
	assert.Len(t, manifest.Orders[0].EventPayloadSHA256, 64)
	require.NoError(t, ValidateManifest(manifest, manifest.SHA256, strings.Repeat("a", 40)))

	first, err := service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Equal(t, 1, first.UpdatedCount)
	assert.Zero(t, first.AlreadySetCount)
	assert.Equal(t, 1, first.OutboxCreatedCount)
	assert.Zero(t, first.OutboxAlreadyPresentCount)
	assertTopUpCompletion(t, db, 444, 1_100)
	assertUserQuota(t, db, 1, 700_000_000)
	assertTopUpOutbox(t, db, manifest.Orders[0])

	second, err := service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Zero(t, second.UpdatedCount)
	assert.Equal(t, 1, second.AlreadySetCount)
	assert.Zero(t, second.OutboxCreatedCount)
	assert.Equal(t, 1, second.OutboxAlreadyPresentCount)
	assertTopUpCompletion(t, db, 444, 1_100)
	assertUserQuota(t, db, 1, 700_000_000)
	assertTopUpOutbox(t, db, manifest.Orders[0])

	verified, err := service.Verify(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Equal(t, 1, verified.VerifiedCount)
}

func TestRecoveryLatestCutoffCapturesTopUpHighWaterMark(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	createEligibleTopUp(t, db, 500, "trade-500", model.PaymentProviderEpay)
	service := New(db, &fakeProvider{}, strings.Repeat("9", 40), 500_000)

	cutoff, err := service.LatestCutoff(context.Background(), 444)
	require.NoError(t, err)
	assert.EqualValues(t, 500, cutoff)

	_, err = service.LatestCutoff(context.Background(), 501)
	require.ErrorIs(t, err, ErrInvalidManifest)
}

func TestRecoveryIncludesCompletedOrderAndCreatesOnlyMissingOutbox(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", 444).Update("complete_time", 1_130).Error)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("d", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	require.Len(t, manifest.Orders, 1)
	assert.EqualValues(t, 1_130, manifest.Orders[0].CompletedAt)

	result, err := service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Zero(t, result.UpdatedCount)
	assert.Equal(t, 1, result.AlreadySetCount)
	assert.Equal(t, 1, result.OutboxCreatedCount)
	assertTopUpCompletion(t, db, 444, 1_130)
	assertUserQuota(t, db, 1, 700_000_000)
	assertTopUpOutbox(t, db, manifest.Orders[0])
}

func TestRecoveryRejectsStoredCompletionBeforeProviderEvidence(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", 444).Update("complete_time", 1_099).Error)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("d", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	_, err := service.Plan(context.Background(), 444, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside provider evidence bounds")
}

func TestRecoveryRejectsManifestAndProviderDriftWithoutWriting(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("b", 40), 500_000)
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
	assertOutboxCount(t, db, 0)
}

func TestRecoveryRejectsSourceAndOutboxDriftAtomically(t *testing.T) {
	t.Run("source amount changed", func(t *testing.T) {
		db := newRecoveryDatabase(t)
		createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
		provider := &fakeProvider{orders: map[string]ProviderOrder{
			"trade-444": successfulProviderOrder("trade-444", 1_100),
		}}
		service := New(db, provider, strings.Repeat("e", 40), 500_000)
		service.now = func() time.Time { return time.Unix(2_000, 0) }
		manifest, err := service.Plan(context.Background(), 444, 500)
		require.NoError(t, err)

		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", 444).Update("amount", 201).Error)
		_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source row drifted")
		assertTopUpCompletion(t, db, 444, 0)
		assertOutboxCount(t, db, 0)
	})

	t.Run("event key already has conflicting payload", func(t *testing.T) {
		db := newRecoveryDatabase(t)
		createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
		createEligibleTopUp(t, db, 445, "trade-445", model.PaymentProviderEpay)
		provider := &fakeProvider{orders: map[string]ProviderOrder{
			"trade-444": successfulProviderOrder("trade-444", 1_100),
			"trade-445": successfulProviderOrder("trade-445", 1_101),
		}}
		service := New(db, provider, strings.Repeat("f", 40), 500_000)
		service.now = func() time.Time { return time.Unix(2_000, 0) }
		manifest, err := service.Plan(context.Background(), 444, 500)
		require.NoError(t, err)
		require.NoError(t, db.Create(&model.KKAIOutboxEvent{
			EventKey:    "newapi:topup:445",
			Topic:       model.KKAIOutboxTopicTopUpCompleted,
			AggregateID: "445",
			Payload:     "{\"conflict\":true}",
			Status:      model.KKAIOutboxStatusPending,
			AvailableAt: 1_101,
			CreatedAt:   1_101,
		}).Error)

		_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outbox")
		assertTopUpCompletion(t, db, 444, 0)
		assertTopUpCompletion(t, db, 445, 0)
		assertOutboxCount(t, db, 1)
	})
}

func TestRecoveryVerifyRequiresExpectedOutbox(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("1", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", 444).Update("complete_time", 1_100).Error)

	_, err = service.Verify(context.Background(), manifest, manifest.SHA256)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox")
}

func TestRecoveryFailsClosedForUnsupportedProviderAndExcludesUninvitedOrders(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderStripe)
	provider := &fakeProvider{orders: map[string]ProviderOrder{}}
	service := New(db, provider, strings.Repeat("c", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	_, err := service.Plan(context.Background(), 444, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider evidence")

	require.NoError(t, db.Model(&recoveryUser{}).Where("id = ?", 1).Update("inviter_id", 0).Error)
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	assert.Empty(t, manifest.Orders)
}

func TestNewFromDatabaseLoadsQuotaPerUnitStrictly(t *testing.T) {
	db := newRecoveryDatabase(t)
	provider := &fakeProvider{orders: map[string]ProviderOrder{}}

	service, err := NewFromDatabase(db, provider, strings.Repeat("2", 40))
	require.NoError(t, err)
	assert.Equal(t, 500_000.0, service.quotaPerUnit)

	require.NoError(t, db.Create(&optionRow{Key: "QuotaPerUnit", Value: "600000"}).Error)
	service, err = NewFromDatabase(db, provider, strings.Repeat("2", 40))
	require.NoError(t, err)
	assert.Equal(t, 600_000.0, service.quotaPerUnit)

	require.NoError(t, db.Model(&optionRow{}).Where("key = ?", "QuotaPerUnit").Update("value", "invalid").Error)
	_, err = NewFromDatabase(db, provider, strings.Repeat("2", 40))
	require.ErrorIs(t, err, ErrInvalidQuotaConfiguration)
}

func newRecoveryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &recoveryUser{}, &model.KKAIOutboxEvent{}, &optionRow{}))
	return db
}

func createEligibleTopUp(t *testing.T, db *gorm.DB, id int, tradeNo, provider string) {
	t.Helper()
	require.NoError(t, db.FirstOrCreate(&recoveryUser{}, &recoveryUser{ID: 2, Group: "vip"}).Error)
	require.NoError(t, db.FirstOrCreate(&recoveryUser{}, &recoveryUser{ID: 1, InviterID: 2, Quota: 700_000_000}).Error)
	require.NoError(t, db.Create(&model.TopUp{
		Id:              id,
		UserId:          1,
		Amount:          200,
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

func assertUserQuota(t *testing.T, db *gorm.DB, id int64, expected int64) {
	t.Helper()
	user := recoveryUser{}
	require.NoError(t, db.First(&user, id).Error)
	assert.Equal(t, expected, user.Quota)
}

func assertTopUpOutbox(t *testing.T, db *gorm.DB, evidence OrderEvidence) {
	t.Helper()
	var events []model.KKAIOutboxEvent
	require.NoError(t, db.Where("event_key = ?", evidence.EventKey).Find(&events).Error)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, model.KKAIOutboxTopicTopUpCompleted, event.Topic)
	assert.Equal(t, "444", event.AggregateID)
	assert.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	assert.EqualValues(t, evidence.CompletedAt, event.AvailableAt)
	assert.EqualValues(t, evidence.CompletedAt, event.CreatedAt)
	assert.Equal(t, evidence.EventPayloadSHA256, hashString(event.Payload))
	var payload model.TopUpCompletedEvent
	require.NoError(t, common.Unmarshal([]byte(event.Payload), &payload))
	assert.EqualValues(t, evidence.TopUpID, payload.SourceOrderID)
	assert.EqualValues(t, evidence.CreditedQuota, payload.CreditedQuota)
	require.NotNil(t, payload.InviterID)
	assert.EqualValues(t, evidence.InviterID, *payload.InviterID)
	assert.Equal(t, evidence.InviterGroup, payload.InviterGroup)
}

func assertOutboxCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&count).Error)
	assert.Equal(t, expected, count)
}

func assertTopUpCompletion(t *testing.T, db *gorm.DB, id int, expected int64) {
	t.Helper()
	stored := model.TopUp{}
	require.NoError(t, db.First(&stored, id).Error)
	assert.Equal(t, expected, stored.CompleteTime)
	assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
}
