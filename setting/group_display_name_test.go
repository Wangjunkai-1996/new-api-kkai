package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGroupDisplayNameUsesExplicitLabelAndCompatibilityFallback(t *testing.T) {
	originalGroups := UserUsableGroups2JSONString()
	originalDisplayNames := GroupDisplayNames2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserUsableGroupsByJSONString(originalGroups))
		require.NoError(t, UpdateGroupDisplayNamesByJSONString(originalDisplayNames))
	})

	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{"stable":"Legacy label"}`))
	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{}`))
	assert.Equal(t, "Legacy label", GetGroupDisplayName("stable"))
	assert.Equal(t, "unknown", GetGroupDisplayName("unknown"))

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{"stable":"New label"}`))
	assert.Equal(t, "New label", GetGroupDisplayName("stable"))
	assert.Equal(t, "blank", GetGroupDisplayName("blank"))
}

func TestGroupDisplayNamesValidationIsAtomicAndNormalizesLabels(t *testing.T) {
	originalDisplayNames := GroupDisplayNames2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDisplayNamesByJSONString(originalDisplayNames))
	})

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{"stable":"  Current label  "}`))
	assert.Equal(t, "Current label", GetGroupDisplayName("stable"))

	for _, invalid := range []string{
		`null`,
		`{"":"label"}`,
		`{"stable":"   "}`,
		`{"stable":null}`,
	} {
		require.Error(t, ValidateGroupDisplayNamesJSON(invalid), invalid)
		require.Error(t, UpdateGroupDisplayNamesByJSONString(invalid), invalid)
		assert.Equal(t, "Current label", GetGroupDisplayName("stable"), invalid)
	}
}

func TestResetGroupDisplayNamesClearsPreviousConfiguration(t *testing.T) {
	originalDisplayNames := GroupDisplayNames2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDisplayNamesByJSONString(originalDisplayNames))
	})

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{"stable":"Current label"}`))
	ResetGroupDisplayNames()
	assert.Equal(t, "stable", GetGroupDisplayName("stable"))
}

func TestUpdateUserUsableGroupsIsAtomicOnMalformedJSON(t *testing.T) {
	originalGroups := UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserUsableGroupsByJSONString(originalGroups))
	})

	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{"stable":"Current label"}`))
	require.Error(t, UpdateUserUsableGroupsByJSONString(`{"stable":`))
	assert.Equal(t, "Current label", GetUsableGroupDescription("stable"))
}

func TestUpdateGroupDisplayNamesRejectsInvalidJSONWithoutReplacingState(t *testing.T) {
	originalDisplayNames := GroupDisplayNames2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDisplayNamesByJSONString(originalDisplayNames))
	})

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{"stable":"Current label"}`))
	require.Error(t, UpdateGroupDisplayNamesByJSONString(`{"stable":`))
	assert.Equal(t, "Current label", GetGroupDisplayName("stable"))
}

func TestGetGroupDisplayNameWithFallbackPreservesSpecialGroupDescription(t *testing.T) {
	originalDisplayNames := GroupDisplayNames2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDisplayNamesByJSONString(originalDisplayNames))
	})

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{}`))
	assert.Equal(t, "Special label", GetGroupDisplayNameWithFallback("special", "Special label"))

	require.NoError(t, UpdateGroupDisplayNamesByJSONString(`{"special":"Configured label"}`))
	assert.Equal(t, "Configured label", GetGroupDisplayNameWithFallback("special", "Special label"))
}
