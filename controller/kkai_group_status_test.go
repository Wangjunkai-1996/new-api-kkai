package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type kkaiGroupStatusAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Groups []service.KKAIGroupStatusEntry `json:"groups"`
	} `json:"data"`
}

func TestKKAIGroupStatusControllerReturnsOnlyVisibleGroups(t *testing.T) {
	setupKKAIGroupStatusControllerTest(t)
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{
		"default": {"-:vip": "hidden"}
	}`)))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status/groups?window=now", nil)
	ctx.Set("id", 1001)

	GetKKAIGroupStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response kkaiGroupStatusAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Groups, 1)
	assert.Equal(t, "default", response.Data.Groups[0].Group)
	assert.NotContains(t, recorder.Body.String(), `"group":"vip"`)
	assert.NotContains(t, recorder.Body.String(), "channel_id")
	assert.NotContains(t, recorder.Body.String(), "base_url")
	assert.NotContains(t, recorder.Body.String(), "key")
}

func setupKKAIGroupStatusControllerTest(t *testing.T) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	originalGetUserGroup := getKKAIUserGroup

	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	common.RDB = nil
	getKKAIUserGroup = func(int, bool) (string, error) { return "default", nil }
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))

	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		getKKAIUserGroup = originalGetUserGroup
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(originalSpecialGroups)))
	})
}
