package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// MarkMembershipBillingUncertain runs only after the transport has stopped.
// The recovery write is atomic with the existing uncertainty marker; older
// schemas and disabled recovery retain the original review path.
func MarkMembershipBillingUncertain(requestID string, channelID int, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !membershipBillingRecoveryWritable(ctx) {
		return model.MarkMembershipBillingUncertainContext(ctx, requestID, channelID, reason)
	}
	if err := model.MarkMembershipRelayRecoveryPending(ctx, requestID, channelID, reason); err != nil {
		// Keep the original durable review marker even if the expansion write
		// failed. Deadline discovery can recreate the recovery record later.
		return errors.Join(err, model.MarkMembershipBillingUncertainContext(ctx, requestID, channelID, reason))
	}
	return nil
}

// persistMembershipTerminalFailure is reached only after a trusted local
// terminal failed to commit. Never rebuild the frozen quote from RelayInfo:
// its pricing fields may already have been normalized for usage display.
func persistMembershipTerminalFailure(info *relaycommon.RelayInfo, requestID string, assessed int64, release bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reason := "settlement_failed"
	if release {
		reason = "release_failed"
	}
	if info.BillingOutcomeUnknown || membershipStreamIncomplete(info) ||
		(release && (info.MembershipHasBillableUsage || info.MembershipViolationFee != 0 || info.MembershipAssessedQuota > 0)) {
		return model.MarkMembershipBillingUncertainContext(ctx, requestID, info.ChannelId, "terminal_evidence_unconfirmed")
	}
	if !membershipBillingRecoveryWritable(ctx) {
		return model.MarkMembershipBillingUncertainContext(ctx, requestID, info.ChannelId, reason)
	}
	var record model.MembershipBillingRecord
	err := model.DB.WithContext(ctx).First(&record, "request_id = ?", requestID).Error
	if err == nil {
		now := common.GetTimestamp()
		input := model.MembershipRelayEvidenceInput{
			Version: 1, Origin: model.MembershipEvidenceLocal, Kind: model.MembershipEvidenceFinalUsage,
			RequestID: record.RequestID, RequestHash: record.RequestHash,
			UserID: info.UserId, TokenID: info.TokenId, ChannelID: info.ChannelId,
			Source: record.Source, AccountID: record.AccountID,
			SnapshotHash: model.MembershipBillingSnapshotHash(record.Snapshot),
			FinalizedAt:  now, ObservedAt: now, AssessedQuota: &assessed,
			BillableUsage:     info.MembershipHasBillableUsage || info.MembershipViolationFee > 0,
			LocalFeesResolved: true,
		}
		if release {
			input.Kind, input.ZeroCostBasis = model.MembershipEvidenceZero, "local_terminal_zero"
		}
		err = model.RecordMembershipRelayEvidence(ctx, input)
	}
	if err != nil {
		return errors.Join(err, model.MarkMembershipBillingUncertainContext(ctx, requestID, info.ChannelId, reason))
	}
	return nil
}
