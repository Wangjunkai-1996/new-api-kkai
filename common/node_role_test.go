package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitNodeRoleFromEnvironment(t *testing.T) {
	tests := []struct {
		name          string
		explicitRole  string
		legacyType    string
		disableWrites string
		wantRole      NodeRole
		wantMaster    bool
		wantWriteJobs bool
	}{
		{
			name:          "default preserves leader behavior",
			wantRole:      NodeRoleLeader,
			wantMaster:    true,
			wantWriteJobs: true,
		},
		{
			name:          "legacy slave maps to readonly standby",
			legacyType:    "slave",
			wantRole:      NodeRoleStandbyReadonly,
			wantMaster:    false,
			wantWriteJobs: false,
		},
		{
			name:          "serving handles traffic without write jobs",
			explicitRole:  "serving",
			legacyType:    "slave",
			wantRole:      NodeRoleServing,
			wantMaster:    true,
			wantWriteJobs: false,
		},
		{
			name:          "leader write jobs can be disabled",
			explicitRole:  "leader",
			disableWrites: "true",
			wantRole:      NodeRoleLeader,
			wantMaster:    true,
			wantWriteJobs: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(NodeRoleEnvironmentVariable, test.explicitRole)
			t.Setenv("NODE_TYPE", test.legacyType)
			t.Setenv("DISABLE_BACKGROUND_TASKS", test.disableWrites)
			restore := snapshotNodeRoleState()
			t.Cleanup(restore)

			require.NoError(t, InitNodeRoleFromEnvironment())
			require.Equal(t, test.wantRole, CurrentNodeRole())
			require.Equal(t, test.wantMaster, IsMasterNode)
			require.Equal(t, test.wantWriteJobs, CanRunWriteBackgroundJobs())
			require.True(t, CanRunReadOnlyBackgroundJobs())
		})
	}
}

func TestInitNodeRoleFromEnvironmentRejectsInvalidValues(t *testing.T) {
	restore := snapshotNodeRoleState()
	t.Cleanup(restore)
	t.Setenv(NodeRoleEnvironmentVariable, "primary-ish")
	t.Setenv("DISABLE_BACKGROUND_TASKS", "")

	require.ErrorIs(t, InitNodeRoleFromEnvironment(), ErrInvalidNodeRole)

	t.Setenv(NodeRoleEnvironmentVariable, "leader")
	t.Setenv("DISABLE_BACKGROUND_TASKS", "sometimes")
	require.ErrorIs(t, InitNodeRoleFromEnvironment(), ErrInvalidNodeRole)
}

func snapshotNodeRoleState() func() {
	role := nodeRole
	disabled := writeBackgroundTasksDisabled
	master := IsMasterNode
	return func() {
		nodeRole = role
		writeBackgroundTasksDisabled = disabled
		IsMasterNode = master
	}
}
