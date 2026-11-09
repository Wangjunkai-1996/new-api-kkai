package model

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type membershipRelayEvidencePayload struct {
	MembershipRelayEvidenceInput
	Conflicts []membershipRelayEvidenceConflict `json:"conflicts,omitempty"`
}

type membershipRelayEvidenceConflict struct {
	Hash       string `json:"hash"`
	Origin     string `json:"origin"`
	ReceiptID  string `json:"receipt_id"`
	ObservedAt int64  `json:"observed_at"`
}

// lockMembershipRecoveryTx preserves the existing User -> Ledger lock order.
// The recovery row is last, so normal settlement never waits on a queue lock.
func lockMembershipRecoveryTx(tx *gorm.DB, requestID string) (*MembershipBillingRecord, *MembershipBillingRecovery, error) {
	var initial MembershipBillingRecord
	if err := tx.Select("user_id").Where("request_id = ?", requestID).First(&initial).Error; err != nil {
		return nil, nil, err
	}
	if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
		return nil, nil, err
	}
	var record MembershipBillingRecord
	if err := lockForUpdate(tx).First(&record, "request_id = ?", requestID).Error; err != nil {
		return nil, nil, err
	}
	if record.Scope != MembershipBillingScopeRelay {
		return nil, nil, ErrMembershipConflict
	}
	recovery := &MembershipBillingRecovery{}
	err := lockForUpdate(tx).First(recovery, "request_id = ?", requestID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := common.GetTimestamp()
		recovery = &MembershipBillingRecovery{RequestID: requestID, State: MembershipRecoveryPending,
			EvidenceKind: MembershipEvidenceUnknown, CreatedAt: now, NextAttemptAt: now}
	} else if err != nil {
		return nil, nil, err
	}
	return &record, recovery, nil
}

func saveMembershipRecoveryTx(tx *gorm.DB, recovery *MembershipBillingRecovery) error {
	recovery.Revision++
	recovery.UpdatedAt = common.GetTimestamp()
	return tx.Save(recovery).Error
}

func validateMembershipRelayEvidence(input MembershipRelayEvidenceInput, record *MembershipBillingRecord) error {
	if input.Version != 1 || (input.Origin != MembershipEvidenceLocal && input.Origin != MembershipEvidenceConsumeLog) ||
		input.RequestID == "" || input.UserID <= 0 || input.TokenID < 0 || input.ChannelID < 0 ||
		input.FinalizedAt <= 0 || input.ObservedAt <= 0 || input.AssessedQuota == nil ||
		*input.AssessedQuota < 0 || *input.AssessedQuota > math.MaxInt32 || !input.LocalFeesResolved ||
		len(input.ReceiptID) > 191 || len(input.UsageHash) > 64 || len(input.ZeroCostBasis) > 128 {
		return ErrMembershipInvalidInput
	}
	if input.Kind != MembershipEvidenceFinalUsage && input.Kind != MembershipEvidenceZero {
		return ErrMembershipInvalidInput
	}
	if input.Kind == MembershipEvidenceZero && (*input.AssessedQuota != 0 || input.BillableUsage || input.ZeroCostBasis == "") {
		return ErrMembershipInvalidInput
	}
	if input.Origin == MembershipEvidenceConsumeLog && input.ReceiptID == "" {
		return ErrMembershipInvalidInput
	}
	if record.Scope != MembershipBillingScopeRelay || input.RequestID != record.RequestID || input.UserID != record.UserID ||
		input.TokenID != record.TokenID || input.Source != record.Source || input.AccountID != record.AccountID ||
		input.RequestHash == "" || input.RequestHash != record.RequestHash ||
		input.SnapshotHash != MembershipBillingSnapshotHash(record.Snapshot) ||
		(record.DispatchedAt > 0 && input.ChannelID != record.ReviewChannelID) ||
		(record.ReviewStartedAt > 0 && record.ReviewChannelID != input.ChannelID) {
		return ErrMembershipConflict
	}
	if input.ZeroCostBasis == "undispatched_orphan" &&
		(record.DispatchedAt != 0 || record.CreatedAt > common.GetTimestamp()-membershipUndispatchedReviewDelay) {
		return ErrMembershipConflict
	}
	return nil
}

// membershipEvidenceSemanticHash excludes observation metadata and provenance.
// A second proof of the same terminal is not a contradictory charge. UsageHash
// remains included: equal amounts alone do not establish equal measured usage.
func membershipEvidenceSemanticHash(input MembershipRelayEvidenceInput) (string, error) {
	input.Origin, input.ReceiptID = "", ""
	input.FinalizedAt, input.ObservedAt = 0, 0
	data, err := common.Marshal(input)
	if err != nil {
		return "", err
	}
	return MembershipBillingSnapshotHash(string(data)), nil
}

// RecordMembershipRelayEvidence commits a terminal observation independently
// of the later funds transaction. It never charges or refunds an account.
func RecordMembershipRelayEvidence(ctx context.Context, input MembershipRelayEvidenceInput) error {
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 {
		return ErrMembershipInvalidInput
	}
	var conflict, err error
	for attempt := 0; attempt < 2; attempt++ {
		err = membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
			conflict = nil
			record, recovery, err := lockMembershipRecoveryTx(tx, input.RequestID)
			if err != nil {
				return err
			}
			if err := validateMembershipRelayEvidence(input, record); err != nil {
				return err
			}
			encoded, err := common.Marshal(input)
			if err != nil {
				return err
			}
			if len(encoded) > 16*1024 {
				return ErrMembershipInvalidInput
			}
			hash, err := membershipEvidenceSemanticHash(input)
			if err != nil {
				return err
			}
			var previous membershipRelayEvidencePayload
			if recovery.EvidenceKind != MembershipEvidenceUnknown {
				if err := common.UnmarshalJsonStr(recovery.EvidencePayload, &previous); err != nil {
					return err
				}
			}
			proofChanged := previous.Origin != input.Origin || previous.ReceiptID != input.ReceiptID
			if recovery.EvidenceKind != MembershipEvidenceUnknown && (recovery.EvidenceHash != hash || (proofChanged && (previous.UsageHash == "" || input.UsageHash == ""))) {
				if len(previous.Conflicts) < 4 {
					previous.Conflicts = append(previous.Conflicts, membershipRelayEvidenceConflict{Hash: hash, Origin: input.Origin, ReceiptID: input.ReceiptID, ObservedAt: input.ObservedAt})
					body, err := common.Marshal(previous)
					if err != nil {
						return err
					}
					if len(body) <= 16*1024 {
						recovery.EvidencePayload = string(body)
					}
				}
				recovery.LastErrorCode = "conflicting_terminal"
				if record.State == "reserved" {
					recovery.State = MembershipRecoveryConflict
					conflict = ErrMembershipConflict
				}
				return saveMembershipRecoveryTx(tx, recovery)
			}
			if recovery.EvidenceKind == MembershipEvidenceUnknown {
				recovery.EvidenceKind, recovery.EvidencePayload, recovery.EvidenceHash = input.Kind, string(encoded), hash
				if input.ReceiptID != "" {
					key := MembershipBillingSnapshotHash(input.Origin + ":" + input.ReceiptID)
					var reused MembershipBillingRecovery
					err := tx.Where("receipt_key = ? AND request_id <> ?", key, input.RequestID).First(&reused).Error
					if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
					if err == nil {
						recovery.State, recovery.LastErrorCode = MembershipRecoveryConflict, "receipt_reused"
						conflict = ErrMembershipConflict
						return saveMembershipRecoveryTx(tx, recovery)
					}
					recovery.ReceiptKey = &key
				}
			}
			if record.State != "reserved" {
				if recovery.State != MembershipRecoveryResolved {
					recovery.State = MembershipRecoveryCachePending
				}
				if (record.State == "settled" && record.ActualQuota != *input.AssessedQuota) ||
					(record.State == "released" && *input.AssessedQuota > 0) {
					recovery.LastErrorCode = "late_terminal_after_completion"
				}
			} else {
				if recovery.State != MembershipRecoveryConflict {
					recovery.State = MembershipRecoveryReady
				}
				if record.ReviewStartedAt == 0 {
					if err := tx.Model(record).Updates(map[string]any{"review_started_at": common.GetTimestamp(), "review_channel_id": input.ChannelID, "review_reason": "terminal_persistence_recovery"}).Error; err != nil {
						return err
					}
				}
			}
			recovery.NextAttemptAt = common.GetTimestamp()
			return saveMembershipRecoveryTx(tx, recovery)
		})
		if err == nil {
			return conflict
		}
		if !membershipRecoveryUniqueViolation(err) || ctx.Err() != nil {
			return err
		}
		// A competing request may have claimed the same provider receipt after our
		// preflight read. Retry in a fresh transaction to persist the conflict row.
	}
	return err
}

func membershipRecoveryUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "unique constraint failed") || strings.Contains(message, "unique constraint violation")
}

// MarkMembershipRelayRecoveryPending atomically keeps the existing uncertainty
// marker and its retry row, including records marked by an older application.
func MarkMembershipRelayRecoveryPending(ctx context.Context, requestID string, channelID int, reason string) error {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || channelID < 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return ErrMembershipInvalidInput
	}
	return membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		record, recovery, err := lockMembershipRecoveryTx(tx, requestID)
		if err != nil {
			return err
		}
		if record.State != "reserved" {
			if recovery.State != MembershipRecoveryResolved {
				recovery.State = MembershipRecoveryCachePending
			}
		} else {
			if (record.DispatchedAt > 0 || record.ReviewStartedAt > 0) && record.ReviewChannelID != channelID {
				return ErrMembershipConflict
			}
			if record.ReviewStartedAt == 0 {
				if err := tx.Model(record).Updates(map[string]any{"review_started_at": common.GetTimestamp(), "review_channel_id": channelID, "review_reason": reason}).Error; err != nil {
					return err
				}
			}
		}
		return saveMembershipRecoveryTx(tx, recovery)
	})
}

func ResolveMembershipRelayEvidence(ctx context.Context, requestID string) (*MembershipBillingRecord, error) {
	return completeMembershipRelayRecovery(ctx, requestID, false)
}

// CompensateExpiredMembershipRelay only compensates an aged, eligible relay
// with a successful empty consume-log lookup. Persisted terminal evidence wins
// independently of log database availability; a failed lookup is never empty.
func CompensateExpiredMembershipRelay(ctx context.Context, requestID string) (*MembershipBillingRecord, error) {
	record, err := ResolveMembershipRelayEvidence(ctx, requestID)
	if !errors.Is(err, errMembershipRecoveryNoEvidence) {
		return record, err
	}
	if LOG_DB == nil {
		return nil, errors.New("membership billing log database unavailable")
	}
	var count int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("request_id = ? AND type = ?", requestID, LogTypeConsume).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, ErrMembershipConflict
	}
	return completeMembershipRelayRecovery(ctx, requestID, true)
}

var errMembershipRecoveryNoEvidence = errors.New("membership recovery has no trusted terminal evidence")

func completeMembershipRelayRecovery(ctx context.Context, requestID string, compensate bool) (*MembershipBillingRecord, error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 {
		return nil, ErrMembershipInvalidInput
	}
	var result *MembershipBillingRecord
	err := membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		result = nil
		record, recovery, err := lockMembershipRecoveryTx(tx, requestID)
		if err != nil {
			return err
		}
		if record.State != "reserved" {
			if record.State != "settled" && record.State != "released" {
				return ErrMembershipConflict
			}
			result = record
			if recovery.State != MembershipRecoveryResolved {
				recovery.State = MembershipRecoveryCachePending
				return saveMembershipRecoveryTx(tx, recovery)
			}
			return nil
		}
		if recovery.State == MembershipRecoveryConflict {
			return ErrMembershipConflict
		}
		if recovery.State != MembershipRecoveryPending && recovery.State != MembershipRecoveryReady {
			return ErrMembershipConflict
		}
		action, reason, actual := "release", "no_evidence_compensation", int64(0)
		billable := false
		if recovery.EvidenceKind == MembershipEvidenceFinalUsage || recovery.EvidenceKind == MembershipEvidenceZero {
			if recovery.State != MembershipRecoveryReady {
				return ErrMembershipConflict
			}
			var evidence MembershipRelayEvidenceInput
			if err := common.UnmarshalJsonStr(recovery.EvidencePayload, &evidence); err != nil {
				return err
			}
			if err := validateMembershipRelayEvidence(evidence, record); err != nil {
				return err
			}
			hash, err := membershipEvidenceSemanticHash(evidence)
			if err != nil || hash != recovery.EvidenceHash || evidence.Kind != recovery.EvidenceKind {
				return ErrMembershipConflict
			}
			actual, billable = *evidence.AssessedQuota, evidence.BillableUsage
			reason = "trusted_terminal_evidence"
			if evidence.Kind == MembershipEvidenceFinalUsage {
				action = "settle"
			}
		} else if recovery.EvidenceKind == MembershipEvidenceUnknown {
			if recovery.State != MembershipRecoveryPending || recovery.EvidencePayload != "" || recovery.EvidenceHash != "" || recovery.ReceiptKey != nil {
				return ErrMembershipConflict
			}
			if !compensate {
				return errMembershipRecoveryNoEvidence
			}
			if record.CreatedAt > common.GetTimestamp()-72*60*60 || !record.prepareMembershipReview(common.GetTimestamp()) {
				return ErrMembershipConflict
			}
		} else {
			return ErrMembershipConflict
		}
		if action == "settle" {
			result, err = settleMembershipRelayBillingTx(tx, requestID, actual, record.ReviewChannelID, billable)
		} else {
			result, err = ReleaseMembershipBillingTx(tx, requestID)
		}
		if err != nil {
			return err
		}
		now := common.GetTimestamp()
		audit := membershipBillingResolution{OperationID: "membership-recovery-" + reason, ActorID: 0, Action: action, ActualQuota: actual, Reason: reason, At: now}
		encoded, err := common.Marshal(audit)
		if err != nil {
			return err
		}
		result.ReviewStartedAt = record.ReviewStartedAt
		if result.ReviewStartedAt == 0 {
			result.ReviewStartedAt = now
		}
		result.ReviewReason, result.ReviewResolution, result.ReviewResolvedAt = reason, string(encoded), now
		if err := tx.Model(result).Updates(map[string]any{"review_started_at": result.ReviewStartedAt, "review_reason": reason, "review_resolution": string(encoded), "review_resolved_at": now}).Error; err != nil {
			return err
		}
		recovery.State, recovery.NextAttemptAt = MembershipRecoveryCachePending, now
		return saveMembershipRecoveryTx(tx, recovery)
	})
	return result, err
}

// CompleteMembershipRelayRecoveryCache is called only after successful cache
// invalidation. Its version guard cannot finish a newly updated recovery row.
func CompleteMembershipRelayRecoveryCache(ctx context.Context, requestID string, revision int64) error {
	return membershipRecoveryTransaction(ctx, func(tx *gorm.DB) error {
		record, recovery, err := lockMembershipRecoveryTx(tx, requestID)
		if err != nil {
			return err
		}
		if record.State != "settled" && record.State != "released" {
			return ErrMembershipConflict
		}
		if recovery.State == MembershipRecoveryResolved && recovery.Revision == revision {
			return nil
		}
		if recovery.State != MembershipRecoveryCachePending || recovery.Revision != revision {
			return ErrMembershipConflict
		}
		recovery.State, recovery.ResolvedAt = MembershipRecoveryResolved, common.GetTimestamp()
		return saveMembershipRecoveryTx(tx, recovery)
	})
}
