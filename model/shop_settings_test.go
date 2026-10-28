package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopSettingsDefaultsAreReadOnlyUntilSaved(t *testing.T) {
	db := shopTestDB(t)
	value, err := GetShopSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), value.Version)
	assert.False(t, value.SalesEnabled)
	assert.Equal(t, "epay", value.Provider)
	assert.Equal(t, "production", value.Environment)
	assert.Equal(t, "CNY", value.Currency)
	assert.Equal(t, 2, value.CurrencyExponent)
	assert.Equal(t, int64(1), value.MinAmountMinor)
	assert.Equal(t, int64(1000000), value.MaxAmountMinor)
	assert.Equal(t, int64(900), value.ReservationSeconds)
	assert.Equal(t, 3, value.MaxUnpaidOrders)
	var retention ShopRetentionPolicy
	require.NoError(t, common.UnmarshalJsonStr(value.RetentionPolicyJSON, &retention))
	assert.Equal(t, ShopRetentionPolicy{90 * 86400, 30 * 86400, 365 * 86400, 1095 * 86400, 365 * 86400}, retention)
	var reminders ShopReminderPolicy
	require.NoError(t, common.UnmarshalJsonStr(value.ReminderPolicyJSON, &reminders))
	assert.Equal(t, ShopReminderPolicy{300, 900, 86400}, reminders)

	var count int64
	require.NoError(t, db.Model(&ShopSettings{}).Count(&count).Error)
	assert.Zero(t, count, "reading defaults must not configure or open the shop")

	value.ProviderAccountRef = "epay:configured-merchant"
	saved, err := UpdateShopSettings(db, shopTestCommand("settings-default-save", 0, 100), *value, "Initialize paused shop")
	require.NoError(t, err)
	assert.Equal(t, int64(1), saved.Version)
	assert.False(t, saved.SalesEnabled)
	assert.Equal(t, "epay:configured-merchant", saved.ProviderAccountRef)
	require.NoError(t, db.Model(&ShopSettings{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestShopSettingsExistingValuesAreNeverReplacedByDefaults(t *testing.T) {
	db := shopTestDB(t)
	stored := ShopSettings{ID: 1, Version: 8, PolicyVersion: 6, SalesEnabled: true, Provider: "epay", ProviderAccountRef: "epay:older-merchant", Environment: "test", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 55, MaxAmountMinor: 123456, ReservationSeconds: 1380, MaxUnpaidOrders: 9, RetentionPolicyJSON: `{"secret_after_delivery_seconds":1234}`, ReminderPolicyJSON: `{"payment_unknown_seconds":4321}`}
	require.NoError(t, db.Create(&stored).Error)
	got, err := GetShopSettings(db)
	require.NoError(t, err)
	assert.Equal(t, stored, *got)
}
