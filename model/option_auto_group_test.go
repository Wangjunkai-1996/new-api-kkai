package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateAutoGroupOptions(t *testing.T) {
	originalProfiles := setting.AutoGroupProfiles2JsonString()
	t.Cleanup(func() { require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(originalProfiles)) })
	require.NoError(t, validateOptionValue("AutoGroups", `["default","vip"]`))
	require.Error(t, validateOptionValue("AutoGroups", `{"default": true}`))
	require.NoError(t, validateOptionValue("AutoGroupProfiles", `{"auto2":["vip","default"]}`))
	require.Error(t, validateOptionValue("AutoGroupProfiles", `{"auto": ["default"]}`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip"]}`))
	require.Error(t, validateOptionValue("GroupRatio", `{"auto2":1}`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{}`))
}

func TestUpdateOptionMapAutoGroupProfilesIsAtomic(t *testing.T) {
	original := setting.AutoGroupProfiles2JsonString()
	common.OptionMapRWMutex.Lock()
	mapWasNil := common.OptionMap == nil
	if mapWasNil {
		common.OptionMap = make(map[string]string)
	}
	originalValue, hadOriginalValue := common.OptionMap["AutoGroupProfiles"]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(original))
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if mapWasNil {
			common.OptionMap = nil
		} else if hadOriginalValue {
			common.OptionMap["AutoGroupProfiles"] = originalValue
		} else {
			delete(common.OptionMap, "AutoGroupProfiles")
		}
	})

	require.NoError(t, updateOptionMap("AutoGroupProfiles", `{"auto2":["vip"]}`))
	common.OptionMapRWMutex.RLock()
	validOptionValue := common.OptionMap["AutoGroupProfiles"]
	common.OptionMapRWMutex.RUnlock()
	require.Error(t, updateOptionMap("AutoGroupProfiles", `{"auto2": []}`))
	common.OptionMapRWMutex.RLock()
	optionValueAfterError := common.OptionMap["AutoGroupProfiles"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, validOptionValue, optionValueAfterError)
	require.Equal(t, []string{"vip"}, setting.GetAutoGroupCandidates("auto2"))
}

func TestUpdateOptionMapAutoGroupsRejectsMalformedValueBeforePublishing(t *testing.T) {
	original := setting.AutoGroups2JsonString()
	common.OptionMapRWMutex.Lock()
	mapWasNil := common.OptionMap == nil
	if mapWasNil {
		common.OptionMap = make(map[string]string)
	}
	originalValue, hadOriginalValue := common.OptionMap["AutoGroups"]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(original))
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if mapWasNil {
			common.OptionMap = nil
		} else if hadOriginalValue {
			common.OptionMap["AutoGroups"] = originalValue
		} else {
			delete(common.OptionMap, "AutoGroups")
		}
	})

	require.NoError(t, updateOptionMap("AutoGroups", `["vip"]`))
	common.OptionMapRWMutex.RLock()
	validOptionValue := common.OptionMap["AutoGroups"]
	common.OptionMapRWMutex.RUnlock()
	require.Error(t, updateOptionMap("AutoGroups", `["vip","vip"]`))
	common.OptionMapRWMutex.RLock()
	optionValueAfterError := common.OptionMap["AutoGroups"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, validOptionValue, optionValueAfterError)
	require.Equal(t, []string{"vip"}, setting.GetAutoGroups())
}

func TestUpdateAutoGroupProfilesEmptyOption(t *testing.T) {
	for _, mode := range []string{"single", "bulk"} {
		for _, tc := range []struct {
			name       string
			value      string
			referenced bool
		}{
			{name: "empty", value: ""},
			{name: "whitespace", value: " \n\t"},
			{name: "referenced", value: "", referenced: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				originalDB := DB
				originalGroupCol := commonGroupCol
				originalProfiles := setting.AutoGroupProfiles2JsonString()
				common.OptionMapRWMutex.Lock()
				originalOptions := common.OptionMap
				common.OptionMap = make(map[string]string)
				common.OptionMapRWMutex.Unlock()
				t.Cleanup(func() {
					DB = originalDB
					commonGroupCol = originalGroupCol
					require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(originalProfiles))
					common.OptionMapRWMutex.Lock()
					common.OptionMap = originalOptions
					common.OptionMapRWMutex.Unlock()
				})

				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
				require.NoError(t, db.AutoMigrate(&Option{}, &Token{}))
				DB = db
				commonGroupCol = "`group`"
				const configured = `{"auto2":["vip"]}`
				require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(configured))
				require.NoError(t, UpdateOption("AutoGroupProfiles", configured))
				if tc.referenced {
					require.NoError(t, db.Create(&Token{Key: "test-key", Group: "auto2"}).Error)
				}

				if mode == "bulk" {
					err = UpdateOptionsBulk(map[string]string{"AutoGroupProfiles": tc.value})
				} else {
					err = UpdateOption("AutoGroupProfiles", tc.value)
				}
				expectedValue := tc.value
				if tc.referenced {
					require.ErrorContains(t, err, "still used by 1 API key(s)")
					expectedValue = configured
					assert.Equal(t, []string{"vip"}, setting.GetAutoGroupCandidates("auto2"))
				} else {
					require.NoError(t, err)
					assert.Empty(t, setting.GetAutoGroupProfilesCopy())
				}
				var persisted Option
				require.NoError(t, db.Where("key = ?", "AutoGroupProfiles").First(&persisted).Error)
				assert.Equal(t, expectedValue, persisted.Value)
				common.OptionMapRWMutex.RLock()
				assert.Equal(t, expectedValue, common.OptionMap["AutoGroupProfiles"])
				common.OptionMapRWMutex.RUnlock()
			})
		}
	}
}
