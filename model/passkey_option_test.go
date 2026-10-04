package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPasskeyDomainRemovalRequiresReviewAndPublishesAtomically(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}, &PasskeyCredential{}))
	previousDB, previousOptions := DB, common.OptionMap
	previousSettings, previousAddress := *system_setting.GetPasskeySettings(), system_setting.ServerAddress
	DB, common.OptionMap = db, map[string]string{}
	*system_setting.GetPasskeySettings() = system_setting.PasskeySettings{}
	system_setting.ServerAddress = ""
	t.Cleanup(func() {
		DB, common.OptionMap = previousDB, previousOptions
		*system_setting.GetPasskeySettings() = previousSettings
		system_setting.ServerAddress = previousAddress
		require.NoError(t, sqlDB.Close())
	})
	initial := map[string]string{
		"passkey.rp_id": "new.example.com", "passkey.legacy_rp_ids": "old.example.com",
		"passkey.origins": "https://new.example.com,https://old.example.com",
	}
	require.NoError(t, UpdateOptionsBulk(initial))
	oldRPID := "old.example.com"
	require.NoError(t, db.Create(&PasskeyCredential{UserID: 1, RPID: &oldRPID, CredentialID: "old-key", PublicKey: "public-key"}).Error)

	require.ErrorIs(t, UpdateOption("passkey.legacy_rp_ids", ""), ErrPasskeyDomainRemovalConfirmation)
	assert.Equal(t, "old.example.com", system_setting.GetPasskeySettings().LegacyRPIDs)
	var stored Option
	require.NoError(t, db.Where("key = ?", "passkey.legacy_rp_ids").First(&stored).Error)
	assert.Equal(t, "old.example.com", stored.Value)

	change, err := UpdatePasskeyDomainOptions(map[string]string{"passkey.legacy_rp_ids": ""}, true, "")
	require.NoError(t, err)
	require.True(t, change.ConfirmationRequired)
	assert.EqualValues(t, 1, change.AffectedCredentials)
	assert.Equal(t, "old.example.com", system_setting.GetPasskeySettings().LegacyRPIDs)
	_, err = UpdatePasskeyDomainOptions(map[string]string{"passkey.legacy_rp_ids": ""}, false, change.RemovalConfirmation)
	require.NoError(t, err)
	assert.Empty(t, system_setting.GetPasskeySettings().LegacyRPIDs)
	assert.Empty(t, common.OptionMap["passkey.legacy_rp_ids"])
}
