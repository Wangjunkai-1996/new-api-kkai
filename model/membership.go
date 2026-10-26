package model

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

const MembershipDurationSeconds int64 = 30 * 24 * 60 * 60

const (
	MembershipSource         = "membership"
	MembershipWalletSource   = "wallet"
	MembershipActive         = "active"
	MembershipExpired        = "expired"
	MembershipExhausted      = "exhausted"
	MembershipCancelled      = "cancelled"
	MembershipOrderPending   = "pending"
	MembershipOrderSuccess   = "success"
	MembershipOrderClosed    = "closed"
	MembershipOrderException = "paid_exception"
)

var (
	ErrMembershipDisabled          = errors.New("membership is not enabled")
	ErrMembershipSchemaNotReady    = errors.New("membership schema is not ready")
	ErrMembershipConflict          = errors.New("membership state changed; refresh and retry")
	ErrMembershipQuoteChanged      = errors.New("membership quote changed; quote again")
	ErrMembershipInsufficientQuota = errors.New("insufficient wallet quota")
	ErrMembershipInvalidInput      = errors.New("invalid membership input")
	ErrMembershipNotFound          = errors.New("membership record not found")
	membershipRuntimeEnabled       atomic.Bool
	membershipSchemaCache          struct {
		sync.Mutex
		db *gorm.DB
	}
)

func init() {
	membershipRuntimeEnabled.Store(strings.EqualFold(os.Getenv("KKAI_MEMBERSHIP_ENABLED"), "true"))
}

// MembershipEnabled is a process capability gate, independent of the sales switch.
func MembershipEnabled() bool { return membershipRuntimeEnabled.Load() }

type MembershipTier struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Rank            int                `json:"rank"`
	Style           string             `json:"style"`
	PriceMinor      int64              `json:"price_minor"`
	Currency        string             `json:"currency"`
	Quota           int64              `json:"quota"`
	Enabled         bool               `json:"enabled"`
	Discounts       map[string]float64 `json:"discounts"`
	StripePriceID   string             `json:"stripe_price_id,omitempty"`
	PaymentProducts map[string]string  `json:"payment_products,omitempty"`
}

type MembershipGroup struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Published   bool   `json:"published"`
}

// MembershipModelLimit is an operator-declared upstream contract for the entire
// API request, including hidden context and internal tool/model iterations.
// It is safety configuration for new authorizations, not an entitlement discount.
type MembershipModelLimit struct {
	MaxInputTokens  int64              `json:"max_input_tokens"`
	MaxOutputTokens int64              `json:"max_output_tokens"`
	MaxToolCalls    int64              `json:"max_tool_calls"`
	TaskUsageLimits map[string]float64 `json:"task_usage_limits,omitempty"`
	Evidence        string             `json:"evidence"`
}

type MembershipCatalog struct {
	ModelLimits            map[string]MembershipModelLimit `json:"model_limits"`
	ReleaseID              int64                           `json:"release_id"`
	SalesEnabled           bool                            `json:"sales_enabled"`
	TopupDiscountsDisabled bool                            `json:"topup_discounts_disabled"`
	Tiers                  []MembershipTier                `json:"tiers"`
	Groups                 []MembershipGroup               `json:"groups"`
}

type MembershipCatalogState struct {
	ID                     int `gorm:"primaryKey"`
	ReleaseID              int64
	SalesEnabled           bool
	TopupDiscountsDisabled bool
}

func (MembershipCatalogState) TableName() string { return "kkai_membership_catalog" }

type MembershipPricingRelease struct {
	ID        int64  `gorm:"primaryKey"`
	Payload   string `gorm:"type:text"`
	ActorID   int
	CreatedAt int64
}

func (MembershipPricingRelease) TableName() string { return "kkai_membership_releases" }

type MembershipGroupRecord struct {
	GroupKey       string `gorm:"primaryKey;type:varchar(64)"`
	Enabled        bool
	FirstReleaseID int64
	CreatedAt      int64
	UpdatedAt      int64
}

func (MembershipGroupRecord) TableName() string { return "kkai_membership_groups" }

type MembershipUserState struct {
	UserID           int `gorm:"primaryKey;autoIncrement:false"`
	CurrentAccountID int64
	PendingOrderID   int64
}

func (MembershipUserState) TableName() string { return "kkai_membership_users" }

type MembershipAccount struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	UserID         int    `json:"user_id" gorm:"index"`
	TierID         string `json:"tier_id" gorm:"type:varchar(64)"`
	Rank           int    `json:"rank" gorm:"column:tier_rank"`
	ReleaseID      int64  `json:"release_id"`
	CurrentOrderID int64  `json:"current_order_id"`
	AmountTotal    int64  `json:"amount_total"`
	AmountUsed     int64  `json:"amount_used"`
	AmountReserved int64  `json:"amount_reserved"`
	StartedAt      int64  `json:"started_at"`
	ExpiresAt      int64  `json:"expires_at"`
	Revision       int64  `json:"revision"`
	Status         string `json:"status" gorm:"type:varchar(24)"`
}

func (MembershipAccount) TableName() string  { return "kkai_membership_accounts" }
func (a MembershipAccount) Available() int64 { return a.AmountTotal - a.AmountUsed - a.AmountReserved }
func (a MembershipAccount) ActiveAt(now int64) bool {
	return a.Status == MembershipActive && a.ExpiresAt > now && a.AmountUsed < a.AmountTotal
}

type MembershipOrderTerms struct {
	Tier              MembershipTier `json:"tier"`
	ReleaseID         int64          `json:"release_id"`
	DurationSeconds   int64          `json:"duration_seconds"`
	Action            string         `json:"action"`
	ExpectedAccountID int64          `json:"expected_account_id"`
	ExpectedRevision  int64          `json:"expected_revision"`
}

type MembershipOrder struct {
	ID                int64                      `json:"id" gorm:"primaryKey"`
	TradeNo           string                     `json:"trade_no" gorm:"uniqueIndex;type:varchar(128)"`
	UserID            int                        `json:"user_id" gorm:"index"`
	TierID            string                     `json:"tier_id" gorm:"type:varchar(64)"`
	ReleaseID         int64                      `json:"release_id"`
	AmountMinor       int64                      `json:"amount_minor"`
	Currency          string                     `json:"currency" gorm:"type:varchar(8)"`
	Provider          string                     `json:"provider" gorm:"type:varchar(32)"`
	PaymentMethod     string                     `json:"payment_method" gorm:"type:varchar(32)"`
	Status            string                     `json:"status" gorm:"type:varchar(24);index"`
	ExpiresAt         int64                      `json:"expires_at"`
	Terms             string                     `json:"-" gorm:"type:text"`
	ReceiptID         string                     `json:"receipt_id" gorm:"type:varchar(128)"`
	ReceiptKey        *string                    `json:"-" gorm:"uniqueIndex;type:varchar(191)"`
	ActualAmountMinor int64                      `json:"actual_amount_minor"`
	ActualCurrency    string                     `json:"actual_currency" gorm:"type:varchar(8)"`
	ActualProvider    string                     `json:"actual_provider" gorm:"type:varchar(32)"`
	ActualOneTime     bool                       `json:"actual_one_time"`
	ExceptionReason   string                     `json:"exception_reason" gorm:"type:varchar(512)"`
	AccountID         int64                      `json:"account_id"`
	CreatedAt         int64                      `json:"created_at"`
	PaidAt            int64                      `json:"paid_at"`
	ResolvedAt        int64                      `json:"resolved_at"`
	Resolution        string                     `json:"resolution" gorm:"type:text"`
	CheckoutState     string                     `json:"checkout_state" gorm:"type:varchar(24)"`
	CheckoutData      string                     `json:"-" gorm:"type:text"`
	CheckoutStartedAt int64                      `json:"checkout_started_at"`
	Payments          []MembershipPaymentReceipt `json:"payments,omitempty" gorm:"-"`
}

func (MembershipOrder) TableName() string { return "kkai_membership_orders" }

type MembershipPaymentReceipt struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	ReceiptKey  string `json:"-" gorm:"uniqueIndex;type:varchar(191)"`
	OrderID     int64  `json:"order_id" gorm:"index"`
	Provider    string `json:"provider" gorm:"type:varchar(32)"`
	ReceiptID   string `json:"receipt_id" gorm:"type:varchar(128)"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency" gorm:"type:varchar(8)"`
	OneTime     bool   `json:"one_time"`
	Status      string `json:"status" gorm:"type:varchar(24)"`
	Reason      string `json:"reason" gorm:"type:varchar(512)"`
	CreatedAt   int64  `json:"created_at"`
	Resolution  string `json:"resolution" gorm:"type:text"`
}

func (MembershipPaymentReceipt) TableName() string { return "kkai_membership_payments" }

type MembershipOrderInput struct {
	UserID            int
	TierID            string
	ReleaseID         int64
	Provider          string
	PaymentMethod     string
	TradeNo           string
	ExpiresAt         int64
	ExpectedAccountID int64
	ExpectedRevision  int64
}

// MembershipPaymentInput contains verified provider facts, never client return parameters.
type MembershipPaymentInput struct {
	TradeNo         string
	Provider        string
	ReceiptID       string
	AmountMinor     int64
	Currency        string
	OneTime         bool
	Paid            bool
	ValidationError string
}

type MembershipSelf struct {
	ServerTime int64              `json:"server_time"`
	Account    *MembershipAccount `json:"account"`
	Tier       *MembershipTier    `json:"tier"`
	Discounts  map[string]float64 `json:"discounts"`
}

type MembershipQuote struct {
	AccountID       int64   `json:"account_id"`
	Revision        int64   `json:"revision"`
	ReleaseID       int64   `json:"release_id"`
	TierID          string  `json:"tier_id"`
	TierName        string  `json:"tier_name"`
	TierStyle       string  `json:"tier_style"`
	Active          bool    `json:"active"`
	Discount        float64 `json:"discount"`
	MembershipRatio float64 `json:"membership_ratio"`
	WalletRatio     float64 `json:"wallet_ratio"`
}

type MembershipBillingRecord struct {
	RequestID                string `json:"request_id" gorm:"primaryKey;type:varchar(128)"`
	Scope                    string `json:"billing_scope" gorm:"column:billing_scope;type:varchar(16)"`
	UserID                   int    `json:"user_id" gorm:"index"`
	TokenID                  int    `json:"token_id"`
	TokenReserved            int64  `json:"token_reserved"`
	TokenQuotaDebited        bool   `json:"token_quota_debited"`
	TokenRefundUnapplied     int64  `json:"token_refund_unapplied"`
	TokenUsedRefundUnapplied int64  `json:"token_used_refund_unapplied"`
	AccountID                int64  `json:"account_id"`
	AccountRevision          int64  `json:"account_revision"`
	Source                   string `json:"source" gorm:"type:varchar(16)"`
	GroupKey                 string `json:"group_key" gorm:"type:varchar(64)"`
	Snapshot                 string `json:"snapshot" gorm:"type:text"`
	RequestHash              string `json:"-" gorm:"type:varchar(64)"`
	ReservedQuota            int64  `json:"reserved_quota"`
	ActualQuota              int64  `json:"actual_quota"`
	ChargedQuota             int64  `json:"charged_quota"`
	ExcessQuota              int64  `json:"excess_quota"`
	State                    string `json:"state" gorm:"type:varchar(24);index"`
	CreatedAt                int64  `json:"created_at"`
	DispatchedAt             int64  `json:"dispatched_at"`
	ReviewAfter              int64  `json:"review_after"`
	SettledAt                int64  `json:"settled_at"`
	RemainingQuota           int64  `json:"remaining_quota" gorm:"-"`
	ReviewReason             string `json:"review_reason" gorm:"type:varchar(512)"`
	ReviewStartedAt          int64  `json:"review_started_at"`
	ReviewChannelID          int    `json:"review_channel_id"`
	ReviewResolution         string `json:"review_resolution" gorm:"type:text"`
	ReviewResolvedAt         int64  `json:"review_resolved_at"`
	Applied                  bool   `json:"-" gorm:"-"`
}

func (MembershipBillingRecord) TableName() string { return "kkai_membership_billing" }

type MembershipReservationInput struct {
	RequestID            string
	Scope                string
	UserID               int
	TokenID              int
	TokenKey             string
	SkipToken            bool
	GroupKey             string
	SubscriptionQuota    int64
	WalletQuota          int64
	ExpectedAccountID    int64
	ExpectedRevision     int64
	ExpectedReleaseID    int64 `json:",omitempty"`
	SubscriptionSnapshot string
	WalletSnapshot       string
}

// MembershipModels is for isolated fixtures and explicit migrations only. These
// tables deliberately do not join runtime AutoMigrate or legacy prerequisites.
func MembershipModels() []any {
	return []any{&MembershipCatalogState{}, &MembershipPricingRelease{}, &MembershipGroupRecord{}, &MembershipUserState{}, &MembershipAccount{}, &MembershipOrder{}, &MembershipBillingRecord{}, &MembershipPaymentReceipt{}}
}

func CheckMembershipSchema() error {
	if DB == nil {
		return ErrMembershipSchemaNotReady
	}
	membershipSchemaCache.Lock()
	defer membershipSchemaCache.Unlock()
	if membershipSchemaCache.db == DB {
		return nil
	}
	for _, table := range MembershipModels() {
		if !DB.Migrator().HasTable(table) {
			// HasTable discards query errors; distinguish an absent schema from
			// a failed database connection before reporting it as unprepared.
			if _, err := DB.Migrator().GetTables(); err != nil {
				return err
			}
			return ErrMembershipSchemaNotReady
		}
	}
	membershipSchemaCache.db = DB
	return nil
}

// membershipTransaction retries only SQLite's transaction-conflict error. User
// rows serialize writers on MySQL/PostgreSQL; unique keys remain the final guard.
func membershipTransaction(fn func(*gorm.DB) error) error {
	return MembershipTransaction(context.Background(), fn)
}

// MembershipTransaction retries the entire owning transaction on SQLite write
// conflicts. The callback must contain database effects only, never upstream I/O.
func MembershipTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return MembershipTransactionWithDB(ctx, DB, fn)
}

// MembershipTransactionWithDB preserves the owning caller's database handle.
func MembershipTransactionWithDB(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	if db == nil || ctx == nil || fn == nil {
		return ErrMembershipInvalidInput
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = db.WithContext(ctx).Transaction(fn)
		if err == nil {
			return nil
		}
		message := strings.ToLower(err.Error())
		if db.Dialector.Name() != "sqlite" || (!strings.Contains(message, "sqlite_busy") && !strings.Contains(message, "database is locked")) {
			return err
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 5 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}
