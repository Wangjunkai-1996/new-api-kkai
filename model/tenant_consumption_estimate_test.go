package model

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantEstimatedReservationRetainsUnfundedActualAndResumesAfterTopup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-estimate-actual?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	user, token, input := tenantConsumptionFixture(t, db)
	record, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err)
	require.NoError(t, RecordTenantConsumptionResult(db, record.RequestID, 1200, 0, false))
	require.ErrorIs(t, SettleTenantConsumption(context.Background(), db, record.RequestID), ErrResellerInsufficientFunds)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.EqualValues(t, 900, user.Quota)
	assert.Equal(t, 900, token.RemainQuota)
	assert.Zero(t, user.UsedQuota)
	require.NoError(t, db.First(record, record.ID).Error)
	assert.Equal(t, "settlement_pending", record.Status)
	assert.EqualValues(t, 1200, record.ActualQuota)
	assert.NotEmpty(t, record.LastError)
	var count int64
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
	assert.Zero(t, count, "unfunded actual is retained as a claim, never invented profit")
	input.RequestID = "blocked-until-actual-funded"
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.ErrorIs(t, err, ErrTenantConsumptionConflict)
	require.ErrorIs(t, RecordTenantConsumptionResult(db, record.RequestID, 0, 0, true), ErrTenantConsumptionConflict)

	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: user.TenantID, CustomerID: user.Id, TopUpID: 2, OperationID: "fund-pending-actual", PaidMinor: 5000, PaymentFeeMinor: 80, CreditedQuota: 500})
	require.NoError(t, err)
	require.NoError(t, db.Model(&user).Update("quota", gorm.Expr("quota + ?", 500)).Error)
	require.NoError(t, db.Model(&token).Update("remain_quota", gorm.Expr("remain_quota + ?", 500)).Error)
	require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
	require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.EqualValues(t, 300, user.Quota)
	assert.Equal(t, 300, token.RemainQuota)
	assert.EqualValues(t, 1200, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
	require.NoError(t, err, "settled actual releases the customer's next paid request")
}
