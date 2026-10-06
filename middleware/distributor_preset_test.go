package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestChannelSupportsRequestPathEnforcesPresetRoutes(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeAdvancedCustom, constant.ChannelTypeVLLM, constant.ChannelTypeSGLang} {
		channel := &model.Channel{Type: channelType}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions", Converter: "none"}},
		}})
		assert.True(t, channelSupportsRequestPath(channel, "/v1/chat/completions", "example"))
		assert.False(t, channelSupportsRequestPath(channel, "/v1/images/generations", "example"))
		assert.False(t, channelSupportsRequestPath(channel, "/v1/responses/compact", "example"))
	}
}
