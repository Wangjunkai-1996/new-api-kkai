package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

const (
	MembershipBillingRecoveryModeEnvironment = "KKAI_MEMBERSHIP_BILLING_RECOVERY_MODE"
	membershipRecoveryPageSize               = 100
	membershipRecoveryAdjustmentBudget       = 25
)

var membershipBillingRecoveryMode atomic.Value
var membershipBillingRecoverySchemaCheck = kkaimigrate.CheckMembershipBillingRecoveryRuntimeSchema
var membershipBillingRecoverySchema = struct {
	sync.Mutex
	db        *gorm.DB
	err       error
	nextCheck time.Time
}{}

func init() {
	model.RegisterMembershipBillingRecoverySchemaChecker(CheckMembershipBillingRecoveryRuntimeSchema)
}

func MembershipBillingRecoveryMode() string {
	if mode, ok := membershipBillingRecoveryMode.Load().(string); ok {
		return mode
	}
	return "off"
}

// Only exceptional foreground paths call this gate. Successful relay requests
// do not consult the expansion, mode, leader or recovery table.
func membershipBillingRecoveryWritable(ctx context.Context) bool {
	return MembershipBillingRecoveryMode() == "apply" && CheckMembershipBillingRecoveryRuntimeSchema(ctx, model.DB) == nil
}

func CheckMembershipBillingRecoveryRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return model.ErrMembershipInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	membershipBillingRecoverySchema.Lock()
	defer membershipBillingRecoverySchema.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if membershipBillingRecoverySchema.db == db && time.Now().Before(membershipBillingRecoverySchema.nextCheck) {
		return membershipBillingRecoverySchema.err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := membershipBillingRecoverySchemaCheck(checkCtx, db)
	if checkCtx.Err() != nil {
		return checkCtx.Err() // Cancellation is not shared with another caller.
	}
	ttl := 30 * time.Second
	if err != nil {
		ttl = 5 * time.Second
	}
	membershipBillingRecoverySchema.db, membershipBillingRecoverySchema.err = db, err
	membershipBillingRecoverySchema.nextCheck = time.Now().Add(ttl)
	return err
}

func RegisterMembershipBillingRecoveryJob(registry *BackgroundJobRegistry, workerID string) error {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(MembershipBillingRecoveryModeEnvironment)))
	if mode == "" {
		mode = "off"
	}
	switch mode {
	case "off", "observe", "apply", "paused":
	default:
		return fmt.Errorf("invalid %s: %q", MembershipBillingRecoveryModeEnvironment, mode)
	}
	if registry == nil || !leaderLeaseNamePattern.MatchString(workerID) {
		return ErrInvalidBackgroundJob
	}
	membershipBillingRecoveryMode.Store(mode)
	worker := &membershipBillingRecoveryWorker{workerID: workerID}
	return registry.Register(BackgroundJob{
		Name: "membership-relay-reconcile", Interval: time.Minute, RunOnStart: true,
		WritesData: true, RequiresLeaderLease: true, Run: worker.run,
	})
}

type membershipBillingRecoveryWorker struct {
	workerID string
	round    uint64
}

type membershipBillingRecoveryCounts struct {
	scanned   int
	queued    int
	settled   int
	released  int
	conflicts int
	cached    int
	quota     int64
}

func (w *membershipBillingRecoveryWorker) run(parent context.Context) error {
	mode := MembershipBillingRecoveryMode()
	if mode == "paused" {
		logger.LogWarn(parent, "membership recovery paused; automatic financial adjustments are disabled")
		return nil
	}
	if !model.MembershipEnabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	if err := CheckMembershipBillingRecoveryRuntimeSchema(ctx, model.DB); err != nil {
		present, inspectErr := kkaimigrate.MembershipBillingRecoverySchemaObjectsPresent(model.DB.WithContext(ctx))
		if inspectErr != nil {
			return errors.Join(err, inspectErr)
		}
		if present || !errors.Is(err, kkaimigrate.ErrSchemaNotReady) || mode == "apply" {
			return err // Never let an old resolver ignore installed evidence.
		}
		_, err = model.ReconcileDueMembershipRelayBilling(ctx, 50)
		return err
	}
	lease, err := NewKKAILeaderLeaseStore(model.DB, "newapi-background-writer").Current(ctx)
	if err != nil {
		return err
	}
	if lease.Holder != w.workerID || lease.Fence <= 0 {
		return model.ErrMembershipRecoveryFenceLost
	}
	fence := model.MembershipRecoveryLeaderFence{Name: lease.LeaseName, Holder: lease.Holder, Fence: lease.Fence}
	ctx = model.WithMembershipRecoveryGuard(ctx, func(tx *gorm.DB) error {
		if MembershipBillingRecoveryMode() == "paused" {
			return context.Canceled
		}
		return model.RequireMembershipRecoveryLeaderTx(tx, fence)
	})
	counts := membershipBillingRecoveryCounts{}
	var failures []error
	defer func() {
		logger.LogInfo(parent, fmt.Sprintf("membership recovery mode=%s scanned=%d queued=%d settled=%d released=%d conflicts=%d cache_refreshed=%d adjusted_quota=%d failures=%d", mode, counts.scanned, counts.queued, counts.settled, counts.released, counts.conflicts, counts.cached, counts.quota, len(failures)))
	}()
	w.round++
	if mode == "apply" || mode == "observe" {
		lanes := []string{"dispatched_orphan", "undispatched_orphan", "conflict"}
		for offset := range lanes {
			lane := lanes[(int(w.round)+offset)%len(lanes)]
			phase, stop := context.WithTimeout(ctx, time.Second)
			err = w.discover(phase, lane, mode == "observe", &counts)
			stop()
			if err != nil {
				failures = append(failures, err)
			}
			if ctx.Err() != nil || errors.Is(err, model.ErrMembershipRecoveryFenceLost) {
				return errors.Join(append(failures, ctx.Err())...)
			}
		}
	}
	// Aged receives its own 25-write/6-second budget before ready work. Neither
	// a ready backlog nor failed receipt lookups can borrow this opportunity.
	phase, stop := context.WithTimeout(ctx, 6*time.Second)
	err = w.aged(phase, &counts)
	stop()
	if err != nil {
		failures = append(failures, err)
	}
	if ctx.Err() != nil || errors.Is(err, model.ErrMembershipRecoveryFenceLost) {
		return errors.Join(append(failures, ctx.Err())...)
	}
	phase, stop = context.WithTimeout(ctx, 6*time.Second)
	err = w.due(phase, mode == "apply", &counts)
	stop()
	if err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (w *membershipBillingRecoveryWorker) discover(ctx context.Context, lane string, observe bool, counts *membershipBillingRecoveryCounts) error {
	now := time.Now().Unix()
	if lane == "conflict" {
		var records []model.MembershipBillingRecovery
		if err := model.DB.WithContext(ctx).Where("state = ? AND next_attempt_at <= ?", "conflict", now).Order("next_attempt_at, request_id").Limit(membershipRecoveryPageSize).Find(&records).Error; err != nil {
			return err
		}
		for _, record := range records {
			counts.scanned++
			counts.conflicts++
			if !observe {
				var ledger model.MembershipBillingRecord
				if err := model.DB.WithContext(ctx).Select("state").First(&ledger, "request_id = ?", record.RequestID).Error; err != nil {
					return err
				}
				if ledger.State == "settled" || ledger.State == "released" {
					// Manual review may already have closed the ledger. This
					// call only moves its stale recovery row to cache_pending.
					if _, err := model.ResolveMembershipRelayEvidence(ctx, record.RequestID); err != nil {
						return err
					}
					continue
				}
				if err := model.DeferMembershipBillingRecovery(ctx, record.RequestID, record.Revision, "evidence_conflict", false, now); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if observe {
		lane = "observe_" + lane
	}
	workCtx, stopWork := membershipRecoveryScanWorkContext(ctx)
	defer stopWork()
	records, progress, err := model.MembershipBillingScanPage(workCtx, lane, now, membershipRecoveryPageSize)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return model.AdvanceMembershipBillingScan(ctx, progress, nil, now)
	}
	var last *model.MembershipBillingRecord
	for i := range records {
		record := &records[i]
		if err = workCtx.Err(); err != nil {
			break
		}
		counts.scanned++
		if !observe {
			err = model.MarkMembershipRelayRecoveryPending(workCtx, record.RequestID, record.ReviewChannelID, "orphan_authorization")
			if err != nil {
				break
			}
			counts.queued++
		}
		last = record
	}
	if last != nil {
		err = errors.Join(err, model.AdvanceMembershipBillingScan(ctx, progress, last, now))
	}
	return err
}

// Reserve part of the existing phase budget for its durable checkpoint. Saving
// only after the phase context expires would repeatedly revisit its first page.
// The checkpoint still shares the caller's cancellation and leader fence.
func membershipRecoveryScanWorkContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(ctx, deadline.Add(-200*time.Millisecond))
	}
	return context.WithCancel(ctx)
}

func membershipRecoveryConsumeLogs(ctx context.Context, records []model.MembershipBillingRecord) (map[string][]model.Log, error) {
	if model.LOG_DB == nil {
		return nil, errors.New("membership recovery log database unavailable")
	}
	ids := make([]string, len(records))
	for i := range records {
		ids[i] = records[i].RequestID
	}
	var logs []model.Log
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := model.LOG_DB.WithContext(queryCtx).Select("id", "request_id", "user_id", "token_id", "channel_id", "type", "quota", "other", "created_at").
		Where("request_id IN ? AND type = ?", ids, model.LogTypeConsume).Find(&logs).Error
	byID := make(map[string][]model.Log, len(records))
	for _, log := range logs {
		byID[log.RequestId] = append(byID[log.RequestId], log)
	}
	return byID, err
}

func membershipRecoveryLogEvidence(record model.MembershipBillingRecord, logs []model.Log) (model.MembershipRelayEvidenceInput, error) {
	if len(logs) != 1 {
		return model.MembershipRelayEvidenceInput{}, model.ErrMembershipConflict
	}
	log := logs[0]
	var evidence struct {
		BillingSource string `json:"billing_source"`
		AdminInfo     struct {
			MembershipBilling struct {
				RequestID string `json:"request_id"`
				Assessed  *int64 `json:"assessed"`
				Charged   *int64 `json:"charged"`
			} `json:"membership_billing"`
		} `json:"admin_info"`
	}
	if common.UnmarshalJsonStr(log.Other, &evidence) != nil || log.Id <= 0 ||
		log.UserId != record.UserID || log.TokenId != record.TokenID || log.ChannelId != record.ReviewChannelID ||
		evidence.BillingSource != record.Source || evidence.AdminInfo.MembershipBilling.RequestID != record.RequestID ||
		evidence.AdminInfo.MembershipBilling.Assessed == nil || evidence.AdminInfo.MembershipBilling.Charged == nil ||
		*evidence.AdminInfo.MembershipBilling.Assessed < 0 || *evidence.AdminInfo.MembershipBilling.Charged != int64(log.Quota) ||
		*evidence.AdminInfo.MembershipBilling.Charged != min(*evidence.AdminInfo.MembershipBilling.Assessed, record.ReservedQuota) {
		return model.MembershipRelayEvidenceInput{}, model.ErrMembershipConflict
	}
	return model.MembershipRelayEvidenceInput{
		Version: 1, Origin: "local_consume_log", Kind: "final_usage", RequestID: record.RequestID, RequestHash: record.RequestHash,
		UserID: record.UserID, TokenID: record.TokenID, ChannelID: record.ReviewChannelID,
		Source: record.Source, AccountID: record.AccountID, SnapshotHash: model.MembershipBillingSnapshotHash(record.Snapshot),
		ReceiptID: strconv.Itoa(log.Id), FinalizedAt: log.CreatedAt, ObservedAt: time.Now().Unix(),
		AssessedQuota: evidence.AdminInfo.MembershipBilling.Assessed, BillableUsage: true, LocalFeesResolved: true,
	}, nil
}

func (w *membershipBillingRecoveryWorker) aged(ctx context.Context, counts *membershipBillingRecoveryCounts) error {
	now := time.Now().Unix()
	workCtx, stopWork := membershipRecoveryScanWorkContext(ctx)
	defer stopWork()
	records, progress, err := model.MembershipBillingScanPage(workCtx, "aged", now, membershipRecoveryPageSize)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return model.AdvanceMembershipBillingScan(ctx, progress, nil, now)
	}
	logs, logErr := membershipRecoveryConsumeLogs(workCtx, records)
	var last *model.MembershipBillingRecord
	var failures []error
	adjusted := 0
	for i := range records {
		if adjusted >= membershipRecoveryAdjustmentBudget || workCtx.Err() != nil {
			break
		}
		record := &records[i]
		counts.scanned++
		var recovery model.MembershipBillingRecovery
		found := model.DB.WithContext(workCtx).First(&recovery, "request_id = ?", record.RequestID).Error
		if found != nil && !errors.Is(found, gorm.ErrRecordNotFound) {
			failures = append(failures, found)
			break
		}
		var completed *model.MembershipBillingRecord
		conflict := false
		if found == nil && recovery.State == "conflict" {
			counts.conflicts++
			last = record
			continue
		}
		if found == nil && recovery.State == "ready" {
			completed, err = model.ResolveMembershipRelayEvidence(workCtx, record.RequestID)
			conflict = errors.Is(err, model.ErrMembershipConflict)
		} else if logErr != nil {
			err = logErr
		} else if receipts := logs[record.RequestID]; len(receipts) > 0 {
			var evidence model.MembershipRelayEvidenceInput
			evidence, err = membershipRecoveryLogEvidence(*record, receipts)
			if err == nil {
				err = model.RecordMembershipRelayEvidence(workCtx, evidence)
			}
			if err == nil {
				completed, err = model.ResolveMembershipRelayEvidence(workCtx, record.RequestID)
			}
			conflict = errors.Is(err, model.ErrMembershipConflict)
		} else {
			completed, err = model.CompensateExpiredMembershipRelay(workCtx, record.RequestID)
			// A consume log can arrive between our batch read and the model's
			// final empty-log check. Retry discovery; that race is not evidence
			// of contradictory charges and must not strand the row in conflict.
		}
		if err != nil {
			failures = append(failures, err)
			if errors.Is(err, model.ErrMembershipRecoveryFenceLost) || workCtx.Err() != nil {
				break
			}
			if deferErr := w.deferRecord(workCtx, *record, conflict, "aged_reconcile_failed", now); deferErr != nil {
				failures = append(failures, deferErr)
				break
			}
		} else if completed != nil && completed.Applied {
			adjusted++
			counts.quota += completed.ChargedQuota
			if completed.State == "released" {
				counts.released++
			} else {
				counts.settled++
			}
		}
		last = record
	}
	if last != nil {
		failures = append(failures, model.AdvanceMembershipBillingScan(ctx, progress, last, now))
	}
	return errors.Join(failures...)
}

func (w *membershipBillingRecoveryWorker) deferRecord(ctx context.Context, record model.MembershipBillingRecord, conflict bool, code string, now int64) error {
	if err := model.MarkMembershipRelayRecoveryPending(ctx, record.RequestID, record.ReviewChannelID, code); err != nil {
		return err
	}
	var recovery model.MembershipBillingRecovery
	if err := model.DB.WithContext(ctx).First(&recovery, "request_id = ?", record.RequestID).Error; err != nil {
		return err
	}
	return model.DeferMembershipBillingRecovery(ctx, record.RequestID, recovery.Revision, code, conflict, now)
}

func (w *membershipBillingRecoveryWorker) due(ctx context.Context, apply bool, counts *membershipBillingRecoveryCounts) error {
	states := []string{"cache_pending"}
	if apply {
		states = []string{"ready", "cache_pending", "pending"}
	}
	var failures []error
	now := time.Now().Unix()
	adjusted := 0
	for offset := range states {
		state := states[(int(w.round)+offset)%len(states)]
		phase, stop := context.WithTimeout(ctx, 2*time.Second)
		var rows []model.MembershipBillingRecovery
		err := model.DB.WithContext(phase).Where("state = ? AND next_attempt_at <= ?", state, now).Order("next_attempt_at, request_id").Limit(membershipRecoveryPageSize / len(states)).Find(&rows).Error
		if err != nil {
			failures = append(failures, err)
			stop()
			continue
		}
		for _, row := range rows {
			if phase.Err() != nil || (state == "ready" && adjusted >= membershipRecoveryAdjustmentBudget) {
				break
			}
			counts.scanned++
			switch state {
			case "ready":
				var record *model.MembershipBillingRecord
				record, err = model.ResolveMembershipRelayEvidence(phase, row.RequestID)
				if err == nil && record != nil && record.Applied {
					adjusted++
					counts.quota += record.ChargedQuota
					if record.State == "released" {
						counts.released++
					} else {
						counts.settled++
					}
				}
			case "cache_pending":
				var record model.MembershipBillingRecord
				err = model.DB.WithContext(phase).First(&record, "request_id = ?", row.RequestID).Error
				if err == nil && record.State == "reserved" {
					err = model.ErrMembershipConflict
				}
				if err == nil {
					record.Applied = true
					err = model.MembershipBillingAfterCommitContext(phase, &record)
					if err == nil {
						err = model.CompleteMembershipRelayRecoveryCache(phase, row.RequestID, row.Revision)
						if err == nil {
							counts.cached++
						}
					}
				}
			case "pending":
				var record model.MembershipBillingRecord
				err = model.DB.WithContext(phase).First(&record, "request_id = ?", row.RequestID).Error
				if err == nil && record.State != "reserved" {
					// A foreground completion won the race; the model closes the
					// orphan recovery without applying its money a second time.
					_, err = model.ResolveMembershipRelayEvidence(phase, row.RequestID)
				} else if err == nil {
					var logs map[string][]model.Log
					logs, err = membershipRecoveryConsumeLogs(phase, []model.MembershipBillingRecord{record})
					if err == nil && len(logs[record.RequestID]) > 0 {
						var evidence model.MembershipRelayEvidenceInput
						evidence, err = membershipRecoveryLogEvidence(record, logs[record.RequestID])
						if err == nil {
							err = model.RecordMembershipRelayEvidence(phase, evidence)
						}
					} else if err == nil {
						// P2 has no verified receipt interface yet. An absent local
						// log remains unknown until the independent aged fallback.
						err = model.DeferMembershipBillingRecovery(phase, row.RequestID, row.Revision, "receipt_lookup_unavailable", false, now)
					}
				}
			}
			if err != nil {
				failures = append(failures, err)
				if errors.Is(err, model.ErrMembershipRecoveryFenceLost) {
					stop()
					return errors.Join(failures...)
				}
				failures = append(failures, model.DeferMembershipBillingRecovery(phase, row.RequestID, row.Revision, "recovery_attempt_failed", errors.Is(err, model.ErrMembershipConflict), now))
			}
		}
		stop()
	}
	return errors.Join(failures...)
}
