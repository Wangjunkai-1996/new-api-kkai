package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestValidateAutoGroupOptions(t *testing.T) {
	require.NoError(t, validateOptionValue("AutoGroups", `["default","vip"]`))
	require.Error(t, validateOptionValue("AutoGroups", `{"default": true}`))
	require.NoError(t, validateOptionValue("AutoGroupProfiles", `{"auto2":["vip","default"]}`))
	require.Error(t, validateOptionValue("AutoGroupProfiles", `{"auto": ["default"]}`))
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
