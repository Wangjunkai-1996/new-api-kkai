package common

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// A slow first response is distinct from a long, healthy stream. The scanner
// also uses this as a per-frame idle bound, never a whole-request deadline.
const ManagedRecoveryResponseHeaderTimeout = 930 * time.Second

func (info *RelayInfo) ManagedRecoveryEnabled() bool {
	return info != nil && !info.IsChannelTest && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeSub2API && info.ChannelSetting.ManagedRecoveryV1 &&
		(info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeChatCompletions)
}
