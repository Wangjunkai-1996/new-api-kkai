package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestManagedRecoverySkipsChannelHealthChecks(t *testing.T) {
	info := &RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &ChannelMeta{
			ChannelType:    constant.ChannelTypeSub2API,
			ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true},
		},
	}
	assert.True(t, info.ManagedRecoveryEnabled())
	info.IsChannelTest = true
	assert.False(t, info.ManagedRecoveryEnabled())
}
