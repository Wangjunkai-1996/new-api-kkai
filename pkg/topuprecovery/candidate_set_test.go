package topuprecovery

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormtests "gorm.io/gorm/utils/tests"
)

type recoveryTransactionRecorder struct {
	*sql.DB
	isolation sql.IsolationLevel
}

func (recorder *recoveryTransactionRecorder) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	if options != nil {
		recorder.isolation = options.Isolation
	}
	return recorder.DB.BeginTx(ctx, options)
}

func TestRecoveryExcludesSubscriptionTopUpsAcrossPlanApplyVerify(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	createEligibleTopUp(t, db, 445, "subscription-trade-445", model.PaymentProviderEpay)
	require.NoError(t, db.Create(&model.SubscriptionOrder{
		UserId:  1,
		TradeNo: "subscription-trade-445",
		Status:  common.TopUpStatusSuccess,
		PlanId:  1,
		Money:   10,
	}).Error)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("3", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }

	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	require.Len(t, manifest.Orders, 1)
	assert.EqualValues(t, 444, manifest.Orders[0].TopUpID)

	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	_, err = service.Verify(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assertTopUpCompletion(t, db, 445, 0)
}

func TestRecoveryApplyRejectsCandidateSetDriftBeforeWriting(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("4", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)

	createEligibleTopUp(t, db, 445, "trade-445", model.PaymentProviderEpay)
	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.ErrorIs(t, err, ErrCandidateSetDrift)
	assert.Contains(t, err.Error(), "candidate set")
	assertTopUpCompletion(t, db, 444, 0)
	assertTopUpCompletion(t, db, 445, 0)
	assertOutboxCount(t, db, 0)
}

func TestRecoveryApplyRejectsCandidateRemovalBeforeWriting(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("5", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)

	require.NoError(t, db.Model(&recoveryUser{}).Where("id = ?", 1).Update("inviter_id", 0).Error)
	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.ErrorIs(t, err, ErrCandidateSetDrift)
	assertTopUpCompletion(t, db, 444, 0)
	assertOutboxCount(t, db, 0)
}

func TestRecoveryVerifyRejectsCandidateSetDrift(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("7", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)
	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)

	createEligibleTopUp(t, db, 445, "trade-445", model.PaymentProviderEpay)
	_, err = service.Verify(context.Background(), manifest, manifest.SHA256)
	require.ErrorIs(t, err, ErrCandidateSetDrift)
}

func TestRecoveryCandidateQueryBuildsPostgresCompatibleSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=recovery dbname=recovery sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)

	querySQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var sources []topUpSource
		return topUpSourceQuery(tx).
			Where("top_ups.id >= ? AND top_ups.id <= ?", 444, 500).
			Order("top_ups.id ASC").
			Find(&sources)
	})
	assert.Contains(t, querySQL, `inviters."group"`)
	assert.NotContains(t, querySQL, "inviters.`group`")
	assert.Contains(t, querySQL, "NOT EXISTS (SELECT 1 FROM subscription_orders")
}

func TestRecoveryCandidateDependencyLockCoversEligibilityRows(t *testing.T) {
	db, err := gorm.Open(gormtests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)

	statements := make([]string, 0, 3)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("capture recovery dependency locks", func(tx *gorm.DB) {
		statement := tx.Statement.SQL.String()
		if strings.Contains(statement, "FOR UPDATE") {
			statements = append(statements, statement)
		}
	}))

	require.NoError(t, lockRecoveryCandidateDependencies(db, 444, 500))
	require.Len(t, statements, 3)
	assert.Contains(t, statements[0], "top_ups")
	assert.Contains(t, statements[1], "users")
	assert.Contains(t, statements[2], "users")
	for _, statement := range statements {
		assert.Contains(t, statement, "FOR UPDATE")
	}
}

func TestRecoveryApplyRequestsSerializableTransaction(t *testing.T) {
	db := newRecoveryDatabase(t)
	createEligibleTopUp(t, db, 444, "trade-444", model.PaymentProviderEpay)
	provider := &fakeProvider{orders: map[string]ProviderOrder{
		"trade-444": successfulProviderOrder("trade-444", 1_100),
	}}
	service := New(db, provider, strings.Repeat("8", 40), 500_000)
	service.now = func() time.Time { return time.Unix(2_000, 0) }
	manifest, err := service.Plan(context.Background(), 444, 500)
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	recorder := &recoveryTransactionRecorder{DB: sqlDB}
	db.Config.ConnPool = recorder
	db.Statement.ConnPool = recorder

	_, err = service.Apply(context.Background(), manifest, manifest.SHA256)
	require.NoError(t, err)
	assert.Equal(t, sql.LevelSerializable, recorder.isolation)
}
