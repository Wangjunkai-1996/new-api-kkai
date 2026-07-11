package operation_setting

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyIncidentSafetySettingsDefaultOff(t *testing.T) {
	setting := DefaultPolicyIncidentSetting()
	assert.False(t, setting.DisableClientTokenPersistently)
	assert.False(t, setting.IsolateUpstreamOnPolicyIncident)
}

func TestPolicyIncidentSettingRejectsRemovedEvidenceOptions(t *testing.T) {
	_, err := BuildPolicyIncidentSettingCandidate(DefaultPolicyIncidentSetting(), map[string]string{
		"policy_incident_setting.evidence_enabled": "true",
	})
	require.Error(t, err)
}

func TestPolicyIncidentSettingSnapshotPublishesWholeValuesAtomically(t *testing.T) {
	original := GetPolicyIncidentSetting()
	t.Cleanup(func() { require.NoError(t, PublishPolicyIncidentSetting(original)) })
	first := PolicyIncidentSetting{DisableClientTokenPersistently: true}
	second := PolicyIncidentSetting{IsolateUpstreamOnPolicyIncident: true}
	require.NoError(t, PublishPolicyIncidentSetting(first))

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1000; index++ {
			if index%2 == 0 {
				require.NoError(t, PublishPolicyIncidentSetting(first))
			} else {
				require.NoError(t, PublishPolicyIncidentSetting(second))
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1000; index++ {
			snapshot := GetPolicyIncidentSetting()
			assert.True(t, snapshot == first || snapshot == second, "observed torn policy setting: %+v", snapshot)
		}
	}()
	waitGroup.Wait()
}
