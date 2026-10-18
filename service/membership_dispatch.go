package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// PrepareMembershipDispatch makes an interrupted relay discoverable without
// treating a timeout as proof of zero cost. The same deadline is enforced by the
// transport; task and image requests retain their existing durable lifecycles.
func PrepareMembershipDispatch(ctx context.Context, info *relaycommon.RelayInfo) (context.Context, context.CancelFunc, error) {
	if info.MembershipBillingRequestID == "" || info.MembershipBillingRequestID != info.RequestId {
		return ctx, func() {}, nil
	}
	deadline := info.MembershipDeadline
	if deadline.IsZero() {
		duration := max(time.Hour, time.Duration(common.RelayTimeout)*time.Second, time.Duration(constant.StreamingTimeout)*time.Second)
		deadline = time.Now().Add(duration)
		if existing, ok := ctx.Deadline(); ok && existing.Before(deadline) {
			deadline = existing
		}
		info.MembershipDeadline = deadline
	}
	if err := model.MarkMembershipBillingDispatched(info.MembershipBillingRequestID, info.ChannelId, deadline.Unix()); err != nil {
		return ctx, func() {}, err
	}
	bounded, cancel := context.WithDeadline(ctx, deadline)
	return bounded, cancel, nil
}
