package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoGroupProfilesAreAtomicAndCopied(t *testing.T) {
	originalGroups := AutoGroups2JsonString()
	originalProfiles := AutoGroupProfiles2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoGroupsByJsonString(originalGroups))
		require.NoError(t, UpdateAutoGroupProfilesByJsonString(originalProfiles))
	})

	require.NoError(t, UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip","default"]}`))
	assert.True(t, IsAutoGroup("auto"))
	assert.True(t, IsAutoGroup("auto2"))
	assert.Equal(t, []string{"vip", "default"}, GetAutoGroupCandidates("auto2"))
	assert.Equal(t, []string{"auto", "auto2"}, GetAutoGroupNames())

	candidates := GetAutoGroupCandidates("auto2")
	candidates[0] = "changed"
	assert.Equal(t, []string{"vip", "default"}, GetAutoGroupCandidates("auto2"))

	require.Error(t, UpdateAutoGroupProfilesByJsonString(`{"auto2":`))
	assert.Equal(t, []string{"vip", "default"}, GetAutoGroupCandidates("auto2"))
}

func TestAutoGroupProfilesRejectInvalidNamesAndReferences(t *testing.T) {
	assert.NoError(t, ValidateAutoGroupProfilesJSON(""))
	for _, invalid := range []string{
		`{"auto": ["default"]}`,
		`{"": ["default"]}`,
		`{"auto2": []}`,
		`{"auto2": ["default", "default"]}`,
		`{"auto2": ["auto3"], "auto3": ["default"]}`,
	} {
		require.Error(t, ValidateAutoGroupProfilesJSON(invalid), invalid)
	}
}

func TestAutoGroupsUpdateIsAtomicOnMalformedJSON(t *testing.T) {
	original := AutoGroups2JsonString()
	t.Cleanup(func() { require.NoError(t, UpdateAutoGroupsByJsonString(original)) })

	require.NoError(t, UpdateAutoGroupsByJsonString(`["vip"]`))
	require.Error(t, UpdateAutoGroupsByJsonString(`["vip","vip"]`))
	assert.Equal(t, []string{"vip"}, GetAutoGroups())
	require.Error(t, UpdateAutoGroupsByJsonString(`{"broken": true}`))
	assert.Equal(t, []string{"vip"}, GetAutoGroups())
}
