package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicyIncidentPersistentDisableDefaultsOff(t *testing.T) {
	assert.False(t, policyIncidentSetting.DisableClientTokenPersistently)
}
