package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWaffoPancakeResolutionDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	dsn := fmt.Sprintf("file:waffo-pancake-resolution-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestResolveWaffoPancakeTradeNoClassifiesPermanentFailures(t *testing.T) {
	db := setupWaffoPancakeResolutionDB(t)
	event := &WaffoPancakeWebhookEvent{Data: WaffoPancakeWebhookData{
		OrderMerchantExternalID:       "pancake-order",
		MerchantProvidedBuyerIdentity: WaffoPancakeBuyerIdentityFromUserID(7),
	}}

	_, err := ResolveWaffoPancakeTradeNo(event)
	require.ErrorIs(t, err, ErrWaffoPancakeTopUpNotFound)
	require.True(t, IsPermanentWaffoPancakeResolutionError(err))

	require.NoError(t, db.Create(&model.TopUp{
		UserId:          8,
		TradeNo:         "pancake-order",
		PaymentProvider: model.PaymentProviderWaffoPancake,
		Status:          common.TopUpStatusPending,
	}).Error)
	_, err = ResolveWaffoPancakeTradeNo(event)
	require.ErrorIs(t, err, ErrWaffoPancakeIdentityMismatch)
	require.True(t, IsPermanentWaffoPancakeResolutionError(err))
}

func TestResolveWaffoPancakeTradeNoPreservesDatabaseFailures(t *testing.T) {
	db := setupWaffoPancakeResolutionDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = ResolveWaffoPancakeTradeNo(&WaffoPancakeWebhookEvent{Data: WaffoPancakeWebhookData{
		OrderMerchantExternalID:       "pancake-order",
		MerchantProvidedBuyerIdentity: WaffoPancakeBuyerIdentityFromUserID(7),
	}})
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrWaffoPancakeTopUpNotFound))
	require.False(t, IsPermanentWaffoPancakeResolutionError(err))
}
