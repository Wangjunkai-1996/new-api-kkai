package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestRequestTokenAutoGroupsRespectProfileOrderPermissionsAndLimit(t *testing.T) {
	previousGroups := setting.AutoGroups2JsonString()
	previousProfiles := setting.AutoGroupProfiles2JsonString()
	previousUsable := setting.UserUsableGroups2JSONString()
	previousLimit := setting.GetMaxTokenAutoGroups()
	previousRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(previousProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(strconv.Itoa(previousLimit)))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
	})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip","private"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto","auto2":"Auto 2"}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("1"))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2,"private":3}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Equal(t, []string{"default", "vip"}, GetRequestAutoGroupCandidates(ctx, "default", "auto"))
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"private", "vip", "default"})
	assert.Equal(t, []string{"vip"}, GetRequestAutoGroupCandidates(ctx, "default", "auto"))
	assert.Equal(t, []string{"vip"}, GetRequestAutoGroupCandidates(ctx, "default", "auto2"))
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"default"})
	assert.Empty(t, GetRequestAutoGroupCandidates(ctx, "default", "auto2"))
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{})
	assert.Empty(t, GetRequestAutoGroupCandidates(ctx, "default", "auto"))
}
