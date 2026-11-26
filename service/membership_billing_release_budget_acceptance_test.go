//go:build !membership_legacy_baseline

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipStandardEarlyReleaseSerializesLastSixDollars(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			f := newMembershipRecoveryAcceptanceFixture(t, dialect, true, 2)
			t.Setenv("KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", "*")
			if dialect == "mysql" {
				var isolation string
				require.NoError(t, f.db.Raw("SELECT @@tx_isolation").Scan(&isolation).Error)
				require.Equal(t, "REPEATABLE-READ", isolation)
			}
			now := time.Now().Unix()
			// Both policy generations contribute to the existing $144. The
			// two new members each qualify for $6, but only one still fits.
			for i := range 24 {
				reason := "standard_early_no_evidence_compensation"
				if i < 12 {
					reason = "pilot_early_no_evidence_compensation"
				}
				require.NoError(t, f.db.Create(&model.MembershipBillingRecord{
					RequestID: fmt.Sprintf("rolling-history-%d", i), UserID: 100 + i/5,
					Scope: model.MembershipBillingScopeRelay, Source: model.MembershipSource,
					State: "released", ReservedQuota: 600, Snapshot: `{"quota_per_unit":100}`,
					CreatedAt: now - 1200, ReviewResolvedAt: now - 200, ReviewReason: reason,
				}).Error)
			}
			var ids []string
			for i := range 2 {
				id := fmt.Sprintf("rolling-candidate-%d", i)
				input := f.reservationInput(t, id, i, 100)
				input.SubscriptionQuota, input.SubscriptionSnapshot = 600, `{"quota_per_unit":100}`
				record, err := model.ReserveMembershipBilling(input)
				require.NoError(t, err)
				require.NoError(t, model.MarkMembershipBillingDispatched(id, f.channel.Id, now+3600))
				require.NoError(t, model.MarkMembershipRelayRecoveryPending(t.Context(), id, f.channel.Id, model.MembershipPilotEarlyReviewReason))
				require.NoError(t, f.db.Model(record).Updates(map[string]any{
					"created_at": now - 20*60, "dispatched_at": now - 19*60, "review_started_at": now - 16*60,
				}).Error)
				ids = append(ids, id)
			}
			control, err := model.ReadMembershipRecoveryControl(t.Context())
			require.NoError(t, err)
			fence := model.MembershipRecoveryLeaderFence{Name: "newapi-background-writer", Holder: "acceptance-worker", Fence: 1}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			arrived, start := make(chan struct{}, 2), make(chan struct{})
			failures := make(chan error, 2)
			for _, id := range ids {
				go func() {
					first := true
					guarded := model.WithMembershipRecoveryGuard(ctx, func(tx *gorm.DB) error {
						if first {
							first = false
							arrived <- struct{}{}
							select {
							case <-start:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						if err := model.RequireMembershipRecoveryControlTx(tx, control); err != nil {
							return err
						}
						return model.RequireMembershipRecoveryLeaderTx(tx, fence)
					})
					_, err := model.TryCompensateEarlyMembershipRelay(guarded, id, control)
					failures <- err
				}()
			}
			for range ids {
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			close(start)
			for range ids {
				require.NoError(t, <-failures)
			}
			var closed, pending int64
			require.NoError(t, f.db.Model(&model.MembershipBillingRecord{}).Where("request_id IN ? AND state = ? AND review_reason = ?", ids, "released", "standard_early_no_evidence_compensation").Count(&closed).Error)
			require.NoError(t, f.db.Model(&model.MembershipBillingRecord{}).Where("request_id IN ? AND state = ?", ids, "reserved").Count(&pending).Error)
			assert.EqualValues(t, 1, closed)
			assert.EqualValues(t, 1, pending)
		})
	}
}
