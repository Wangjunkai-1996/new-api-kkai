//go:build !membership_legacy_baseline

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func NewMembershipRecoveryAcceptanceBridge(t *testing.T, dialect string, members bool, users, conflicts int, unlimited bool) MembershipRecoveryAcceptanceBridge {
	f := newMembershipRecoveryAcceptanceFixture(t, dialect, members, users)
	records := seedMembershipAcceptanceBacklog(t, f, conflicts, unlimited)
	for _, record := range records {
		require.NoError(t, f.db.Create(&model.MembershipBillingRecovery{RequestID: record.RequestID, State: model.MembershipRecoveryConflict,
			EvidenceKind: model.MembershipEvidenceUnknown, LastErrorCode: "acceptance_conflict", Revision: 1, CreatedAt: record.CreatedAt, UpdatedAt: record.CreatedAt, NextAttemptAt: record.CreatedAt}).Error)
	}
	worker := &membershipBillingRecoveryWorker{workerID: "acceptance-worker"}
	return MembershipRecoveryAcceptanceBridge{DB: f.db, Pool: f.pool, Users: f.users, Tokens: f.tokens, Channel: f.channel,
		RunWorker: worker.run, RunPoll: MembershipBillingRecoveryControlPollJob().Run,
		SetMode: func(mode string) {
			require.NoError(t, f.db.Model(&model.Option{}).Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).Update("value", mode+":aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error)
			require.NoError(t, MembershipBillingRecoveryControlPollJob().Run(context.Background()))
		}}
}

func membershipAcceptanceExtraTables() []any {
	return []any{&model.MembershipBillingRecovery{}, &model.MembershipBillingScanProgress{}, &model.Option{}}
}

func configureMembershipRecoveryAcceptanceFixture(t *testing.T, db *gorm.DB) {
	previousMode, previousCheck := MembershipBillingRecoveryMode(), membershipBillingRecoverySchemaCheck
	membershipBillingRecoveryMode.Store("apply")
	membershipBillingRecoverySchemaCheck = func(_ context.Context, candidate *gorm.DB) error {
		if candidate != db {
			return model.ErrMembershipSchemaNotReady
		}
		return nil
	}
	membershipBillingRecoverySchema.Lock()
	membershipBillingRecoverySchema.db = nil
	membershipBillingRecoverySchema.Unlock()
	initializeMembershipRecoveryControl("acceptance-worker", "apply")
	require.NoError(t, db.Create(&model.Option{Key: model.MembershipBillingRecoveryControlOption, Value: "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}).Error)
	require.NoError(t, MembershipBillingRecoveryControlPollJob().Run(context.Background()))
	t.Cleanup(func() {
		initializeMembershipRecoveryControl("", previousMode)
		membershipBillingRecoveryMode.Store(previousMode)
		membershipBillingRecoverySchemaCheck = previousCheck
		membershipBillingRecoverySchema.Lock()
		membershipBillingRecoverySchema.db = nil
		membershipBillingRecoverySchema.Unlock()
	})
}

func (f *membershipRecoveryAcceptanceFixture) reserve(t *testing.T, id string, userIndex int, ready bool) *model.MembershipBillingRecord {
	t.Helper()
	user, token := f.users[userIndex], f.tokens[userIndex]
	record, err := model.ReserveMembershipBilling(f.reservationInput(t, id, userIndex, 100))
	require.NoError(t, err)
	require.NoError(t, model.MarkMembershipBillingDispatched(id, f.channel.Id, time.Now().Unix()+300))
	record, err = model.GetMembershipBillingRecord(id)
	require.NoError(t, err)
	if ready {
		actual := int64(3)
		require.NoError(t, model.RecordMembershipRelayEvidence(context.Background(), model.MembershipRelayEvidenceInput{
			Version: 1, Origin: model.MembershipEvidenceLocal, Kind: model.MembershipEvidenceFinalUsage,
			RequestID: id, RequestHash: record.RequestHash, UserID: user.Id, TokenID: token.Id, ChannelID: f.channel.Id,
			Source: record.Source, AccountID: record.AccountID, SnapshotHash: model.MembershipBillingSnapshotHash(record.Snapshot),
			FinalizedAt: time.Now().Unix(), ObservedAt: time.Now().Unix(), AssessedQuota: &actual, BillableUsage: true, LocalFeesResolved: true,
		}))
	}
	return record
}

func TestMembershipReservationMySQLAcceptance(t *testing.T) {
	t.Run("different_users_reserve_without_gap_deadlock", func(t *testing.T) {
		f := newMembershipRecoveryAcceptanceFixture(t, "mysql", true, 4)
		inputs := make([]model.MembershipReservationInput, len(f.users))
		for i := range inputs {
			inputs[i] = f.reservationInput(t, fmt.Sprintf("parallel-reserve-%d", i), i, 100)
		}
		// Synchronize at the actual ledger INSERT. The former missing-key
		// FOR UPDATE made all four transactions hold the same gap here.
		arrived := make(chan struct{}, len(inputs))
		release := make(chan struct{}, len(inputs))
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("acceptance_reservation_barrier", func(tx *gorm.DB) {
			if tx.Statement.Table != (model.MembershipBillingRecord{}).TableName() {
				return
			}
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		}))
		defer f.db.Callback().Create().Remove("acceptance_reservation_barrier")
		results := make(chan error, len(inputs))
		for _, input := range inputs {
			go func() {
				results <- model.MembershipTransaction(ctx, func(tx *gorm.DB) error {
					_, err := model.ReserveMembershipBillingTx(tx, input)
					return err
				})
			}()
		}
		for range inputs {
			select {
			case <-arrived:
			case <-ctx.Done():
				t.Fatal("independent users did not reach the reservation insert")
			}
		}
		for range inputs {
			release <- struct{}{}
		}
		for range inputs {
			assert.NoError(t, <-results)
		}
		if t.Failed() {
			return
		}
		for _, input := range inputs {
			stored, err := model.GetMembershipBillingRecord(input.RequestID)
			require.NoError(t, err)
			assert.Equal(t, model.MembershipSource, stored.Source)
			assert.Equal(t, input.ExpectedAccountID, stored.AccountID)
			assert.Equal(t, input.SubscriptionSnapshot, stored.Snapshot)
			assert.Equal(t, input.SubscriptionQuota, stored.ReservedQuota)
			assert.Equal(t, input.SubscriptionQuota, stored.TokenReserved)
			assert.True(t, stored.TokenQuotaDebited)
			var account model.MembershipAccount
			var token model.Token
			require.NoError(t, f.db.First(&account, stored.AccountID).Error)
			require.NoError(t, f.db.First(&token, stored.TokenID).Error)
			assert.Equal(t, input.SubscriptionQuota, account.AmountReserved)
			assert.EqualValues(t, input.SubscriptionQuota, token.UsedQuota)
			assert.EqualValues(t, 1_000_000-input.SubscriptionQuota, token.RemainQuota)
		}
	})
	t.Run("old_snapshot_replay_and_cross_user_conflict", func(t *testing.T) {
		f := newMembershipRecoveryAcceptanceFixture(t, "mysql", true, 2)
		input := f.reservationInput(t, "snapshot-reservation", 0, 100)
		tx := f.db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		var count int64
		require.NoError(t, tx.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
		require.Zero(t, count)
		_, err := model.ReserveMembershipBilling(input)
		require.NoError(t, err)
		settled, err := model.SettleMembershipBilling(input.RequestID, 3)
		require.NoError(t, err)
		require.NoError(t, tx.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
		require.Zero(t, count, "the outer RR snapshot must still predate the committed ledger")
		replay, err := model.ReserveMembershipBillingTx(tx, input)
		require.NoError(t, err)
		assert.Equal(t, "settled", replay.State)
		assert.EqualValues(t, 3, replay.ChargedQuota)
		assert.EqualValues(t, 999_997, replay.RemainingQuota)
		assert.Equal(t, settled.RequestHash, replay.RequestHash)
		assert.False(t, replay.Applied)
		require.NoError(t, tx.Commit().Error)
		before, err := model.GetMembershipBillingRecord(input.RequestID)
		require.NoError(t, err)
		otherInput := f.reservationInput(t, input.RequestID, 1, 100)
		err = model.MembershipTransaction(context.Background(), func(tx *gorm.DB) error {
			if err := tx.Model(&model.User{}).Where("id = ?", otherInput.UserID).Update("quota", 123).Error; err != nil {
				return err
			}
			_, err := model.ReserveMembershipBillingTx(tx, otherInput)
			return err
		})
		require.ErrorIs(t, err, model.ErrMembershipConflict)
		after, err := model.GetMembershipBillingRecord(input.RequestID)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		for i, user := range f.users {
			var account model.MembershipAccount
			var token model.Token
			require.NoError(t, f.db.First(&user, user.Id).Error)
			require.NoError(t, f.db.First(&token, f.tokens[i].Id).Error)
			require.NoError(t, f.db.First(&account, "user_id = ?", user.Id).Error)
			used := int64(0)
			if i == 0 {
				used = 3
			}
			assert.EqualValues(t, 1_000_000, user.Quota)
			assert.Zero(t, account.AmountReserved)
			assert.Equal(t, used, account.AmountUsed)
			assert.EqualValues(t, used, token.UsedQuota)
			assert.EqualValues(t, 1_000_000-used, token.RemainQuota)
		}
	})
	for _, members := range []bool{false, true} {
		t.Run(fmt.Sprintf("funding_failure_rolls_back_draft_membership_%t", members), func(t *testing.T) {
			f := newMembershipRecoveryAcceptanceFixture(t, "mysql", members, 1)
			input := f.reservationInput(t, "failed-token-reservation", 0, 100)
			require.NoError(t, f.db.Model(&model.Token{}).Where("id = ?", input.TokenID).Update("remain_quota", 0).Error)
			_, err := model.ReserveMembershipBilling(input)
			require.ErrorIs(t, err, model.ErrTaskBillingInsufficientToken)
			var count int64
			require.NoError(t, f.db.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
			assert.Zero(t, count)
			var user model.User
			var token model.Token
			require.NoError(t, f.db.First(&user, input.UserID).Error)
			require.NoError(t, f.db.First(&token, input.TokenID).Error)
			assert.EqualValues(t, 1_000_000, user.Quota)
			assert.Zero(t, token.RemainQuota)
			assert.Zero(t, token.UsedQuota)
			if members {
				var account model.MembershipAccount
				require.NoError(t, f.db.First(&account, input.ExpectedAccountID).Error)
				assert.Zero(t, account.AmountReserved)
				assert.Zero(t, account.AmountUsed)
				return
			}
			freeInput := f.reservationInput(t, "zero-cost-reservation", 0, 0)
			free, err := model.ReserveMembershipBilling(freeInput)
			require.NoError(t, err, "an unchanged zero-cost draft may report zero updated rows")
			assert.Zero(t, free.ReservedQuota)
			assert.True(t, free.Applied)
		})
	}
}

func TestMembershipRecoveryRealDatabaseAcceptance(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			t.Run("foreground_and_recovery_settle_once", func(t *testing.T) {
				f := newMembershipRecoveryAcceptanceFixture(t, dialect, true, 1)
				record := f.reserve(t, "race-finalizers", 0, true)
				start := make(chan struct{})
				type outcome struct {
					record *model.MembershipBillingRecord
					err    error
				}
				outcomes := make(chan outcome, 2)
				go func() {
					<-start
					r, err := model.ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
					outcomes <- outcome{r, err}
				}()
				go func() {
					<-start
					r, err := model.SettleMembershipRelayBilling(record.RequestID, 3, f.channel.Id, true)
					outcomes <- outcome{r, err}
				}()
				close(start)
				applied := 0
				for range 2 {
					result := <-outcomes
					require.NoError(t, result.err)
					if result.record.Applied {
						applied++
					}
				}
				assert.Equal(t, 1, applied)
				var account model.MembershipAccount
				require.NoError(t, f.db.First(&account, record.AccountID).Error)
				assert.EqualValues(t, 3, account.AmountUsed)
				assert.Zero(t, account.AmountReserved)
				var user model.User
				require.NoError(t, f.db.First(&user, record.UserID).Error)
				assert.EqualValues(t, 3, user.UsedQuota)
				assert.Equal(t, 1, user.RequestCount)
				var token model.Token
				require.NoError(t, f.db.First(&token, record.TokenID).Error)
				assert.Equal(t, 999_997, token.RemainQuota)
				assert.Equal(t, 3, token.UsedQuota)
			})
			t.Run("real_row_lock_cancellation", func(t *testing.T) {
				f := newMembershipRecoveryAcceptanceFixture(t, dialect, false, 1)
				record := f.reserve(t, "cancel-locked-finalizer", 0, true)
				tx := f.db.Begin()
				require.NoError(t, tx.Error)
				defer tx.Rollback()
				require.NoError(t, tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", record.UserID).Error)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				finished := make(chan error, 1)
				go func() { _, err := model.ResolveMembershipRelayEvidence(ctx, record.RequestID); finished <- err }()
				waitCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				waiting := false
				for !waiting {
					if dialect == "mysql" {
						// MySQL 5.7 can retain an empty INFORMATION_SCHEMA lock
						// snapshot while InnoDB already reports this row wait.
						var monitor struct{ Status string }
						require.NoError(t, f.db.WithContext(waitCtx).Raw("SHOW ENGINE INNODB STATUS").Scan(&monitor).Error)
						_, transactions, found := strings.Cut(monitor.Status, "\nTRANSACTIONS\n")
						require.True(t, found)
						transactions, _, found = strings.Cut(transactions, "\nFILE I/O\n")
						require.True(t, found)
						// Inspect only current transactions, excluding old deadlocks.
						for _, transaction := range strings.Split(transactions, "\n---TRANSACTION ") {
							if strings.Contains(transaction, "LOCK WAIT ") && strings.Contains(transaction, "FOR UPDATE") &&
								strings.Contains(transaction, "of table `"+f.namespace+"`.`users`") &&
								strings.Contains(transaction, "locks rec but not gap waiting") {
								waiting = true
								break
							}
						}
					} else {
						var count int64
						require.NoError(t, f.db.WithContext(waitCtx).Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE application_name = ? AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()", f.namespace).Scan(&count).Error)
						waiting = count > 0
					}
					if waiting {
						break
					}
					select {
					case <-waitCtx.Done():
						t.Fatal("no actual database lock wait was observed")
					case err := <-finished:
						t.Fatalf("resolver did not wait for the held user lock: %v", err)
					case <-ticker.C:
					}
				}
				cancel()
				select {
				case err := <-finished:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(3 * time.Second):
					t.Fatal("cancelled resolver failed to exit")
				}
				require.NoError(t, tx.Rollback().Error)
				stored, err := model.GetMembershipBillingRecord(record.RequestID)
				require.NoError(t, err)
				assert.Equal(t, "reserved", stored.State)
				resolved, err := model.ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
				require.NoError(t, err)
				assert.True(t, resolved.Applied)
				var user model.User
				require.NoError(t, f.db.First(&user, record.UserID).Error)
				assert.EqualValues(t, 999_997, user.Quota)
			})
			t.Run("deferred_recovery_does_not_lock_user", func(t *testing.T) {
				f := newMembershipRecoveryAcceptanceFixture(t, dialect, true, 1)
				record := f.reserve(t, "defer-with-user-locked", 0, true)
				var beforeUser model.User
				var beforeToken model.Token
				var beforeAccount model.MembershipAccount
				var beforeLedger model.MembershipBillingRecord
				var beforeRecovery model.MembershipBillingRecovery
				require.NoError(t, f.db.First(&beforeUser, record.UserID).Error)
				require.NoError(t, f.db.First(&beforeToken, record.TokenID).Error)
				require.NoError(t, f.db.First(&beforeAccount, record.AccountID).Error)
				require.NoError(t, f.db.First(&beforeLedger, "request_id = ?", record.RequestID).Error)
				require.NoError(t, f.db.First(&beforeRecovery, "request_id = ?", record.RequestID).Error)
				require.NotEmpty(t, beforeRecovery.EvidencePayload)

				tx := f.db.Begin()
				require.NoError(t, tx.Error)
				defer tx.Rollback()
				require.NoError(t, tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", record.UserID).Error)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				ctx = model.WithMembershipRecoveryGuard(ctx, func(tx *gorm.DB) error {
					return model.RequireMembershipRecoveryLeaderTx(tx, model.MembershipRecoveryLeaderFence{Name: "newapi-background-writer", Holder: "acceptance-worker", Fence: 1})
				})
				now := time.Now().Unix()
				require.NoError(t, model.DeferMembershipBillingRecovery(ctx, record.RequestID, beforeRecovery.Revision, "acceptance_retry", true, now), "scheduling must finish while the user row remains locked")

				var afterUser model.User
				var afterToken model.Token
				var afterAccount model.MembershipAccount
				var afterLedger model.MembershipBillingRecord
				var afterRecovery model.MembershipBillingRecovery
				require.NoError(t, f.db.First(&afterUser, record.UserID).Error)
				require.NoError(t, f.db.First(&afterToken, record.TokenID).Error)
				require.NoError(t, f.db.First(&afterAccount, record.AccountID).Error)
				require.NoError(t, f.db.First(&afterLedger, "request_id = ?", record.RequestID).Error)
				require.NoError(t, f.db.First(&afterRecovery, "request_id = ?", record.RequestID).Error)
				assert.Equal(t, beforeUser, afterUser)
				assert.Equal(t, beforeToken, afterToken)
				assert.Equal(t, beforeAccount, afterAccount)
				assert.Equal(t, beforeLedger, afterLedger)
				expectedRecovery := beforeRecovery
				expectedRecovery.State = model.MembershipRecoveryConflict
				expectedRecovery.Revision++
				expectedRecovery.AttemptCount++
				expectedRecovery.NextAttemptAt = now + 60
				expectedRecovery.LastCheckedAt, expectedRecovery.UpdatedAt = now, now
				expectedRecovery.LastErrorCode = "acceptance_retry"
				assert.Equal(t, expectedRecovery, afterRecovery, "scheduling may change only retry metadata, never saved evidence")
				require.NoError(t, tx.Rollback().Error)
			})
			t.Run("stale_leader_cannot_settle", func(t *testing.T) {
				f := newMembershipRecoveryAcceptanceFixture(t, dialect, false, 1)
				record := f.reserve(t, "stale-worker", 0, true)
				ctx := model.WithMembershipRecoveryGuard(context.Background(), func(tx *gorm.DB) error {
					return model.RequireMembershipRecoveryLeaderTx(tx, model.MembershipRecoveryLeaderFence{Name: "newapi-background-writer", Holder: "replaced-worker", Fence: 1})
				})
				_, err := model.ResolveMembershipRelayEvidence(ctx, record.RequestID)
				require.True(t, errors.Is(err, model.ErrMembershipRecoveryFenceLost), "%v", err)
				stored, err := model.GetMembershipBillingRecord(record.RequestID)
				require.NoError(t, err)
				assert.Equal(t, "reserved", stored.State)
			})
		})
	}
}
