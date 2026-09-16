package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAutoGroupCandidatesUseIndependentProfiles(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalProfiles := setting.AutoGroupProfiles2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(originalProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip","default"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto","auto2":"Auto 2"}`))

	assert.Equal(t, []string{"default", "vip"}, GetUserAutoGroupCandidates("", "auto"))
	assert.Equal(t, []string{"vip", "default"}, GetUserAutoGroupCandidates("", "auto2"))
	assert.Equal(t, []string{"auto", "auto2"}, GetUserAutoGroups(""))
	assert.Empty(t, GetUserAutoGroupCandidates("", "auto3"))
}
