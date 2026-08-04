package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImagePricingOptionIsNotPublishedWhenPersistenceFails(t *testing.T) {
	originalDB := DB
	originalPolicy := image_pricing_setting.JSON()
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, image_pricing_setting.UpdateByJSONString(originalPolicy))
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	DB = db

	config := image_pricing_setting.DefaultConfig()
	config.Enabled = true
	raw, err := common.Marshal(config)
	require.NoError(t, err)
	beforeHash := image_pricing_setting.PolicyHash()

	err = UpdateOption(image_pricing_setting.OptionKey, string(raw))

	require.Error(t, err)
	assert.Equal(t, beforeHash, image_pricing_setting.PolicyHash())
}
