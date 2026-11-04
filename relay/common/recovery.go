package common

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// Sub2 owns a 900-second logical budget. The extra 30 seconds only allow its
// final result to cross the gateway; they never grant another model attempt.
const ManagedRecoveryRequestTimeout = 930 * time.Second

func (info *RelayInfo) ManagedRecoveryEnabled() bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeSub2API && info.ChannelSetting.ManagedRecoveryV1 &&
		(info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeChatCompletions)
}
