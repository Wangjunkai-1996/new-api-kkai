package model

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantConsumptionReconciliationPreservesReservationUntilSettlement(t *testing.T) {
	for _, test := range []struct {
		name, pending, final string
		actual               int64
		profit               int64
	}{{"confirmed_usage", "settlement_pending", "settled", 150, 726}, {"confirmed_zero", "refund_pending", "refunded", 0, 0}} {
		t.Run(test.name, func(t *testing.T) {
			db := newResellerFinanceTestDB(t)
			user, token, quote := tenantConsumptionFixture(t, db)
			consumption, err := ReserveTenantConsumption(context.Background(), db, quote, token.Id)
			require.NoError(t, err)
			require.NoError(t, MarkTenantConsumptionUncertain(db, consumption.RequestID, 0))
			input := TenantConsumptionReconciliationInput{TenantID: user.TenantID, RequestID: consumption.RequestID, IdempotencyKey: "operator-confirmed", ActualQuota: test.actual, ConfirmedZero: test.actual == 0, ActorID: 99, Reason: "upstream usage checked", Evidence: "provider billing reference", Now: 100}
			var audit *ResellerLedgerEntry
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				var err error
				audit, err = ReconcileTenantConsumptionTx(tx, input)
				return err
			}))
			assert.Equal(t, ResellerLedgerKindUsageReconciliation, audit.Kind)
			assert.Equal(t, consumption.QuoteID, audit.QuoteID)
			assert.Zero(t, audit.AmountMinor)
			assert.Zero(t, audit.PendingDelta)
			assert.Zero(t, audit.AvailableDelta)
			var payload TenantConsumptionReconciliationInput
			require.NoError(t, common.UnmarshalJsonStr(audit.Reason, &payload))
			assert.Equal(t, input.Evidence, payload.Evidence)
			assert.Equal(t, input.ActorID, payload.ActorID)
			assert.Equal(t, input.ActualQuota, payload.ActualQuota)
			require.NoError(t, db.First(consumption, consumption.ID).Error)
			assert.Equal(t, test.pending, consumption.Status)
			assert.Empty(t, consumption.LastError)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 900, user.Quota)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 900, token.RemainQuota)
			require.NoError(t, SettleTenantConsumption(context.Background(), db, consumption.RequestID))
			require.NoError(t, db.First(consumption, consumption.ID).Error)
			assert.Equal(t, test.final, consumption.Status)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 1000-test.actual, user.Quota)
			assert.EqualValues(t, test.actual, user.UsedQuota)
			var wallet ResellerWallet
			require.NoError(t, db.Where("tenant_id = ?", user.TenantID).First(&wallet).Error)
			assert.Equal(t, test.profit, wallet.PendingMinor)
			input.Now++
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				replayed, err := ReconcileTenantConsumptionTx(tx, input)
				if err == nil {
					assert.Equal(t, audit.ID, replayed.ID)
				}
				return err
			}))
			input.Evidence = "different result"
			assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := ReconcileTenantConsumptionTx(tx, input)
				return err
			}), ErrResellerLedgerConflict)
			input.IdempotencyKey = "new-review"
			assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := ReconcileTenantConsumptionTx(tx, input)
				return err
			}), ErrTenantConsumptionConflict)
			var count int64
			require.NoError(t, db.Model(&ResellerLedgerEntry{}).Where("kind = ?", ResellerLedgerKindUsageReconciliation).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestTenantConsumptionReconciliationRejectsInvalidEvidenceAndState(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	user, token, quote := tenantConsumptionFixture(t, db)
	consumption, err := ReserveTenantConsumption(context.Background(), db, quote, token.Id)
	require.NoError(t, err)
	input := TenantConsumptionReconciliationInput{TenantID: user.TenantID, RequestID: consumption.RequestID, IdempotencyKey: "review", ActualQuota: 100, ActorID: 99, Reason: "checked", Evidence: "provider-reference"}
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReconcileTenantConsumptionTx(tx, input)
		return err
	}), ErrTenantConsumptionConflict)
	require.NoError(t, MarkTenantConsumptionUncertain(db, consumption.RequestID, 0))
	for _, test := range []struct {
		name   string
		change func(*TenantConsumptionReconciliationInput)
		err    error
	}{
		{"actor", func(i *TenantConsumptionReconciliationInput) { i.ActorID = 0 }, ErrResellerFinanceInvalidInput},
		{"reason", func(i *TenantConsumptionReconciliationInput) { i.Reason = " " }, ErrResellerFinanceInvalidInput},
		{"evidence", func(i *TenantConsumptionReconciliationInput) { i.Evidence = "" }, ErrResellerFinanceInvalidInput},
		{"idempotency", func(i *TenantConsumptionReconciliationInput) { i.IdempotencyKey = "" }, ErrResellerFinanceInvalidInput},
		{"unconfirmed_zero", func(i *TenantConsumptionReconciliationInput) { i.ActualQuota = 0 }, ErrResellerFinanceInvalidInput},
		{"contradictory_zero", func(i *TenantConsumptionReconciliationInput) { i.ConfirmedZero = true }, ErrResellerFinanceInvalidInput},
		{"negative", func(i *TenantConsumptionReconciliationInput) { i.ActualQuota = -1 }, ErrResellerFinanceInvalidInput},
		{"overflow", func(i *TenantConsumptionReconciliationInput) { i.ActualQuota = math.MaxInt32 + 1 }, ErrResellerFinanceInvalidInput},
		{"other_tenant", func(i *TenantConsumptionReconciliationInput) { i.TenantID++ }, gorm.ErrRecordNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := input
			test.change(&invalid)
			assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := ReconcileTenantConsumptionTx(tx, invalid)
				return err
			}), test.err)
		})
	}
	var count int64
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.First(consumption, consumption.ID).Error)
	assert.Equal(t, "reserved", consumption.Status)
	assert.Equal(t, "upstream_final_usage_unknown", consumption.LastError)
}

func TestTenantConsumptionReconciliationAuditAndIntentAreAtomic(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	user, token, quote := tenantConsumptionFixture(t, db)
	consumption, err := ReserveTenantConsumption(context.Background(), db, quote, token.Id)
	require.NoError(t, err)
	require.NoError(t, MarkTenantConsumptionUncertain(db, consumption.RequestID, 0))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail-reconciliation-intent", func(tx *gorm.DB) {
		if tx.Statement.Table == "tenant_consumptions" {
			tx.AddError(ErrTenantConsumptionConflict)
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove("fail-reconciliation-intent")) })
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReconcileTenantConsumptionTx(tx, TenantConsumptionReconciliationInput{TenantID: user.TenantID, RequestID: consumption.RequestID, IdempotencyKey: "review", ActualQuota: 100, ActorID: 99, Reason: "checked", Evidence: "provider-reference"})
		return err
	}), ErrTenantConsumptionConflict)
	var count int64
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.First(consumption, consumption.ID).Error)
	assert.Equal(t, "reserved", consumption.Status)
	assert.Equal(t, "upstream_final_usage_unknown", consumption.LastError)
}

func TestTenantConsumptionReconciliationRequiresStoppedExecutionForUnmarkedReservation(t *testing.T) {
	for _, test := range []struct {
		name, status, lastError string
		confirmed               bool
		accepted                bool
	}{
		{"active_without_confirmation", "reserved", "", false, false},
		{"stopped_execution_confirmed", "reserved", "", true, true},
		{"known_unknown_without_confirmation", "reserved", "upstream_final_usage_unknown", false, true},
		{"known_unknown_with_confirmation", "reserved", "upstream_final_usage_unknown", true, true},
		{"other_error_with_confirmation", "reserved", "other_error", true, false},
		{"settled_with_confirmation", "settled", "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newResellerFinanceTestDB(t)
			user, token, quote := tenantConsumptionFixture(t, db)
			consumption, err := ReserveTenantConsumption(context.Background(), db, quote, token.Id)
			require.NoError(t, err)
			require.NoError(t, db.Model(consumption).Updates(map[string]any{"status": test.status, "last_error": test.lastError}).Error)
			input := TenantConsumptionReconciliationInput{TenantID: user.TenantID, RequestID: consumption.RequestID, IdempotencyKey: "review", ActualQuota: 100, ActorID: 99, Reason: "instance termination checked", Evidence: "terminated instance and drained request reference", ConfirmedExecutionStopped: test.confirmed}
			var entry *ResellerLedgerEntry
			err = db.Transaction(func(tx *gorm.DB) error {
				var err error
				entry, err = ReconcileTenantConsumptionTx(tx, input)
				return err
			})
			if !test.accepted {
				require.ErrorIs(t, err, ErrTenantConsumptionConflict)
				var count int64
				require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
				assert.Zero(t, count)
				return
			}
			require.NoError(t, err)
			var audit TenantConsumptionReconciliationInput
			require.NoError(t, common.UnmarshalJsonStr(entry.Reason, &audit))
			assert.Equal(t, test.confirmed, audit.ConfirmedExecutionStopped)
			require.NoError(t, db.First(consumption, consumption.ID).Error)
			assert.Equal(t, "settlement_pending", consumption.Status)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 900, user.Quota)
			input.ConfirmedExecutionStopped = !input.ConfirmedExecutionStopped
			assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := ReconcileTenantConsumptionTx(tx, input)
				return err
			}), ErrResellerLedgerConflict)
		})
	}
}
