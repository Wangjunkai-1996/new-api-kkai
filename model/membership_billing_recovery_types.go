package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

const (
	MembershipRecoveryPending      = "pending"
	MembershipRecoveryReady        = "ready"
	MembershipRecoveryConflict     = "conflict"
	MembershipRecoveryCachePending = "cache_pending"
	MembershipRecoveryResolved     = "resolved"
	MembershipEvidenceUnknown      = "unknown"
	MembershipEvidenceFinalUsage   = "final_usage"
	MembershipEvidenceZero         = "confirmed_zero"
	MembershipEvidenceLocal        = "local_terminal"
	MembershipEvidenceConsumeLog   = "local_consume_log"
)

// MembershipBillingRecovery is only written for an exceptional request. Normal
// authorization, dispatch and settlement never query this table.
type MembershipBillingRecovery struct {
	RequestID       string  `gorm:"primaryKey;type:varchar(128);not null;index:idx_membership_recovery_due,priority:3"`
	State           string  `gorm:"type:varchar(16);not null;index:idx_membership_recovery_due,priority:1"`
	EvidenceKind    string  `gorm:"type:varchar(24);not null"`
	EvidencePayload string  `gorm:"type:text;not null"`
	EvidenceHash    string  `gorm:"type:varchar(64);not null"`
	ReceiptKey      *string `gorm:"type:varchar(191);uniqueIndex:ux_membership_recovery_receipt"`
	NextAttemptAt   int64   `gorm:"not null;index:idx_membership_recovery_due,priority:2"`
	AttemptCount    int64   `gorm:"not null"`
	LastCheckedAt   int64   `gorm:"not null"`
	LastErrorCode   string  `gorm:"type:varchar(64);not null"`
	Revision        int64   `gorm:"not null"`
	CreatedAt       int64   `gorm:"not null"`
	UpdatedAt       int64   `gorm:"not null"`
	ResolvedAt      int64   `gorm:"not null"`
}

func (MembershipBillingRecovery) TableName() string { return "kkai_membership_billing_recovery" }

// MembershipRelayEvidenceInput is an internal, trusted terminal observation,
// never a public request DTO. P1 accepts local terminals and validated consume
// logs only; remote receipts require the separately installed attempt protocol.
type MembershipRelayEvidenceInput struct {
	Version           int    `json:"version"`
	Origin            string `json:"origin"`
	Kind              string `json:"kind"`
	RequestID         string `json:"request_id"`
	RequestHash       string `json:"request_hash"`
	UserID            int    `json:"user_id"`
	TokenID           int    `json:"token_id"`
	ChannelID         int    `json:"channel_id"`
	Source            string `json:"source"`
	AccountID         int64  `json:"account_id"`
	SnapshotHash      string `json:"snapshot_hash"`
	ReceiptID         string `json:"receipt_id"`
	FinalizedAt       int64  `json:"finalized_at"`
	ObservedAt        int64  `json:"observed_at"`
	AssessedQuota     *int64 `json:"assessed_quota,omitempty"`
	BillableUsage     bool   `json:"billable_usage"`
	UsageHash         string `json:"usage_hash,omitempty"`
	ZeroCostBasis     string `json:"zero_cost_basis,omitempty"`
	LocalFeesResolved bool   `json:"local_fees_resolved"`
}

func MembershipBillingSnapshotHash(snapshot string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot)))
}

var membershipRecoverySchema struct {
	sync.Mutex
	check func(context.Context, *gorm.DB) error
}

// RegisterMembershipBillingRecoverySchemaChecker installs the canonical schema
// verifier at application startup. No runtime AutoMigrate is performed here.
func RegisterMembershipBillingRecoverySchemaChecker(check func(context.Context, *gorm.DB) error) {
	membershipRecoverySchema.Lock()
	defer membershipRecoverySchema.Unlock()
	membershipRecoverySchema.check = check
}

func CheckMembershipBillingRecoverySchema(ctx context.Context) error {
	if ctx == nil || DB == nil {
		return ErrMembershipSchemaNotReady
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	membershipRecoverySchema.Lock()
	defer membershipRecoverySchema.Unlock()
	if membershipRecoverySchema.check == nil {
		return ErrMembershipSchemaNotReady
	}
	return membershipRecoverySchema.check(ctx, DB)
}

type membershipRecoveryGuardKey struct{}

// WithMembershipRecoveryGuard supplies the background worker's transaction
// fence. Its guard must hold the leader row lock and verify database time,
// holder and fence; foreground exception capture does not acquire this lock.
func WithMembershipRecoveryGuard(ctx context.Context, guard func(*gorm.DB) error) context.Context {
	return context.WithValue(ctx, membershipRecoveryGuardKey{}, guard)
}

func membershipRecoveryTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if err := CheckMembershipBillingRecoverySchema(ctx); err != nil {
		return err
	}
	guard, _ := ctx.Value(membershipRecoveryGuardKey{}).(func(*gorm.DB) error)
	return MembershipTransaction(ctx, func(tx *gorm.DB) error {
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		if err := fn(tx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if guard != nil {
			return guard(tx)
		}
		return nil
	})
}
