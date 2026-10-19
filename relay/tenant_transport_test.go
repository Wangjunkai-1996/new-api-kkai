package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantAdaptorDispatchRejectsUnverifiedTransports(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeAws, constant.ChannelTypeXunfei, constant.ChannelTypeUnknown} {
		apiType, _ := common.ChannelType2APIType(channelType)
		info := &relaycommon.RelayInfo{TenantID: 7, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, ApiType: apiType}}
		adaptor := GetAdaptor(apiType)
		require.NotNil(t, adaptor)
		// Neither an SDK client nor a WebSocket request/context is initialized:
		// rejection must happen before the underlying transport can execute.
		response, err := adaptor.DoRequest(nil, info, nil)
		assert.Nil(t, response)
		require.Error(t, err)
		var apiErr *relaytypes.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.True(t, relaytypes.IsSkipRetryError(apiErr))
	}
}
