//go:build membership_legacy_baseline

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func membershipAcceptanceExtraTables() []any                                { return nil }
func configureMembershipRecoveryAcceptanceFixture(_ *testing.T, _ *gorm.DB) {}

// This bridge is compiled only into the explicitly archived historical source.
// Its worker is the historical implementation, never the candidate's resolver.
func NewMembershipRecoveryAcceptanceBridge(t *testing.T, dialect string, members bool, users, conflicts int, unlimited bool) MembershipRecoveryAcceptanceBridge {
	f := newMembershipRecoveryAcceptanceFixture(t, dialect, members, users)
	seedMembershipAcceptanceBacklog(t, f, conflicts, unlimited)
	allowed := make(map[string]bool, conflicts)
	for i := 0; i < conflicts; i++ {
		allowed[fmt.Sprintf("membership request conflict-%04d has conflicting consume receipt", i)] = true
	}
	mode := "apply"
	return MembershipRecoveryAcceptanceBridge{DB: f.db, Pool: f.pool, Users: f.users, Tokens: f.tokens, Channel: f.channel,
		SetMode: func(value string) { mode = value },
		RunWorker: func(ctx context.Context) error {
			if mode == "off" {
				return nil
			}
			resolved, err := model.ReconcileDueMembershipRelayBilling(ctx, 50)
			if resolved != 0 {
				return fmt.Errorf("historical worker unexpectedly resolved %d known conflicts", resolved)
			}
			if err == nil {
				return nil
			}
			// The historical job returns one joined error per conflict. Admit exactly
			// the seeded conflicts; cancellation, SQL failures and unexpected messages
			// still fail functional validity.
			joined, ok := err.(interface{ Unwrap() []error })
			if !ok {
				return err
			}
			var failures []error
			for _, failure := range joined.Unwrap() {
				if !allowed[strings.TrimSpace(failure.Error())] {
					failures = append(failures, failure)
				}
			}
			return errors.Join(failures...)
		},
	}
}
