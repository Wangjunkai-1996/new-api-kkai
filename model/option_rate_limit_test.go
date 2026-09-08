package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionMapRejectsInvalidUserRateLimitBeforePublishing(t *testing.T) {
	originalLimits := setting.ModelRequestRateLimitUser2JSONString()
	common.OptionMapRWMutex.Lock()
	originalMapWasNil := common.OptionMap == nil
	if originalMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	originalValue, hadOriginalValue := common.OptionMap["ModelRequestRateLimitUser"]
	common.OptionMap["ModelRequestRateLimitUser"] = `{"id:9":[12,10]}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateModelRequestRateLimitUserByJSONString(originalLimits))
		common.OptionMapRWMutex.Lock()
		if originalMapWasNil {
			common.OptionMap = nil
		} else if hadOriginalValue {
			common.OptionMap["ModelRequestRateLimitUser"] = originalValue
		} else {
			delete(common.OptionMap, "ModelRequestRateLimitUser")
		}
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, setting.UpdateModelRequestRateLimitUserByJSONString(`{"id:9":[12,10]}`))

	require.Error(t, updateOptionMap("ModelRequestRateLimitUser", `{"id:0":[1,1]}`))

	common.OptionMapRWMutex.RLock()
	assert.Equal(t, `{"id:9":[12,10]}`, common.OptionMap["ModelRequestRateLimitUser"])
	common.OptionMapRWMutex.RUnlock()
	total, success, found := setting.GetUserRateLimit(9, "")
	require.True(t, found)
	assert.Equal(t, 12, total)
	assert.Equal(t, 10, success)
}
