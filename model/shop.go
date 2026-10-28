package model

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrShopInvalid           = errors.New("SHOP_VALIDATION_FAILED")
	ErrShopNotFound          = errors.New("SHOP_NOT_FOUND")
	ErrShopVersion           = errors.New("SHOP_VERSION_CONFLICT")
	ErrShopIdempotency       = errors.New("SHOP_IDEMPOTENCY_CONFLICT")
	ErrShopPaused            = errors.New("SHOP_NEW_ORDERS_PAUSED")
	ErrShopProductChanged    = errors.New("SHOP_PRODUCT_CHANGED")
	ErrShopStock             = errors.New("SHOP_OUT_OF_STOCK")
	ErrShopCapacity          = errors.New("SHOP_CAPACITY_FULL")
	ErrShopPendingLimit      = errors.New("SHOP_PENDING_ORDER_LIMIT")
	ErrShopAction            = errors.New("SHOP_ACTION_NOT_ALLOWED")
	ErrShopPaymentUnresolved = errors.New("SHOP_PAYMENT_UNRESOLVED")
	ErrShopEvidence          = errors.New("SHOP_EVIDENCE_CONFLICT")
	ErrShopRefund            = errors.New("SHOP_REFUND_CONFLICT")
	ErrShopRevealBlocked     = errors.New("SHOP_REVEAL_BLOCKED")
	ErrShopRevealExpired     = errors.New("SHOP_REVEAL_EXPIRED")
	ErrShopSecret            = errors.New("SHOP_SECRET_UNAVAILABLE")
	ErrShopResponsibilities  = errors.New("SHOP_ACCOUNT_HAS_RESPONSIBILITIES")
)

const (
	ShopTopicReceipt      = "shop.payment.receipt.v1"
	ShopTopicFulfillment  = "shop.fulfillment.v1"
	ShopTopicNotification = "shop.notification.v1"
)

type ShopRecord struct {
	ID        int64  `gorm:"primaryKey"`
	PublicID  string `gorm:"type:varchar(64);not null;uniqueIndex"`
	Version   int64  `gorm:"not null"`
	CreatedAt int64  `gorm:"autoCreateTime:false;not null"`
	UpdatedAt int64  `gorm:"autoUpdateTime:false;not null"`
}
type ShopSecret struct {
	Ciphertext string
	KeyVersion string
}
type ShopScope struct {
	UserID   int
	TenantID int64
	Admin    bool
}

// RequestHash is computed by the service over canonical business input. Secrets
// require a keyed digest; randomized ciphertext is never used as that input.
type ShopCommand struct {
	ActorID         int
	TenantID        int64
	Admin           bool
	Key             string
	RequestHash     string
	HashKeyVersion  string
	ExpectedVersion int64
	Now             int64
}
type ShopSettings struct {
	ID                  int64 `gorm:"primaryKey"`
	Version             int64
	SalesEnabled        bool
	Provider            string `gorm:"type:varchar(50)"`
	ProviderAccountRef  string `gorm:"type:varchar(128)"`
	Environment         string `gorm:"type:varchar(32)"`
	Currency            string `gorm:"type:varchar(8)"`
	CurrencyExponent    int
	MinAmountMinor      int64
	MaxAmountMinor      int64
	ReservationSeconds  int64
	MaxUnpaidOrders     int
	PolicyVersion       int64
	ReminderPolicyJSON  string `gorm:"type:text"`
	RetentionPolicyJSON string `gorm:"type:text"`
	UpdatedAt           int64  `gorm:"autoUpdateTime:false"`
}
type ShopUserState struct {
	UserID    int `gorm:"primaryKey;autoIncrement:false"`
	TenantID  int64
	Version   int64
	RetiredAt int64
	UpdatedAt int64 `gorm:"autoUpdateTime:false"`
}
type ShopCategory struct {
	ShopRecord
	TenantID  int64  `gorm:"index:idx_shop_category_sort,priority:1"`
	Name      string `gorm:"type:varchar(120)"`
	SortOrder int    `gorm:"index:idx_shop_category_sort,priority:2"`
}
type ShopProduct struct {
	ShopRecord
	TenantID                     int64  `gorm:"index:idx_shop_product_catalog,priority:1"`
	Status                       string `gorm:"type:varchar(24);index:idx_shop_product_catalog,priority:2"`
	Name                         string `gorm:"type:varchar(120)"`
	CategoryID                   *int64 `gorm:"index"`
	SortOrder                    int    `gorm:"index:idx_shop_product_catalog,priority:3"`
	CoverRef                     string `gorm:"type:varchar(500)"`
	Summary                      string `gorm:"type:text"`
	Specification                string `gorm:"type:text"`
	ProductKind                  string `gorm:"type:varchar(32)"`
	DeliveryType                 string `gorm:"type:varchar(16)"`
	PriceMinor                   int64
	Currency                     string `gorm:"type:varchar(8)"`
	CurrencyExponent             int
	TermsVersion                 int64
	TermsJSON                    string `gorm:"type:text"`
	InventoryIdentityDomain      string `gorm:"type:varchar(128)"`
	IdentityNormalizationVersion int64
	ManualCapacity               int64
	ManualUsed                   int64
	LowStockThreshold            int64
}
type ShopOrder struct {
	ShopRecord
	TenantID                 int64 `gorm:"index:idx_shop_order_user,priority:1"`
	UserID                   int   `gorm:"index:idx_shop_order_user,priority:2"`
	ProductID                int64 `gorm:"index"`
	ProductVersion           int64
	SnapshotVersion          int64
	SnapshotJSON             string `gorm:"type:text"`
	SnapshotHash             string `gorm:"type:varchar(64)"`
	TermsVersion             int64
	PolicyAlgorithmVersion   int64
	Quantity                 int
	AmountMinor              int64
	Currency                 string `gorm:"type:varchar(8)"`
	CurrencyExponent         int
	TermsAcceptedAt          int64
	PurchaseState            string `gorm:"type:varchar(16);index:idx_shop_order_expiry,priority:1"`
	PaymentState             string `gorm:"type:varchar(16);index"`
	DeliveryState            string `gorm:"type:varchar(24);index:idx_shop_order_due,priority:1"`
	EffectiveReceiptID       *int64 `gorm:"uniqueIndex"`
	CurrentDeliveryID        *int64
	ActiveAftersaleID        *int64
	ActiveAttemptID          *int64
	ReservationExpiresAt     int64 `gorm:"index:idx_shop_order_expiry,priority:2"`
	ReservedStockID          *int64
	ManualCapacityHeld       bool
	PaidAt                   int64
	ClosedAt                 int64
	ClosedReason             string `gorm:"type:varchar(64)"`
	BuyerInputCiphertext     string `json:"-" gorm:"type:text"`
	BuyerInputKeyVersion     string `json:"-" gorm:"type:varchar(32)"`
	BuyerInputRetentionUntil int64
	RetentionPolicyVersion   int64
	RetentionHold            bool
	RetentionHoldReason      string `gorm:"type:varchar(200)"`
	BuyerInputPurgedAt       int64
	InputCompleteAt          int64
	MissingInputKeysJSON     string `gorm:"type:text"`
	SLAStartedAt             int64
	OriginalDeliveryDueAt    int64
	DeliveryDueAt            int64 `gorm:"index:idx_shop_order_due,priority:2"`
	SLAPauseStartedAt        int64
	SLAPausedSeconds         int64
	WarrantyStartedAt        int64
	WarrantyEndsAt           int64
	EntitlementStartsAt      int64
	EntitlementEndsAt        int64
	ProcessingOwnerID        int
	ExceptionCode            string `gorm:"type:varchar(64)"`
	NextAction               string `gorm:"type:varchar(200)"`
	RiskHold                 bool
	RiskReasonCode           string `gorm:"type:varchar(200)"`
	RiskPublicNote           string `gorm:"type:text"`
	RiskEvidenceRef          string `gorm:"type:text"`
	RiskActorID              int
	RiskChangedAt            int64
}
type ShopStockUnit struct {
	ShopRecord
	TenantID              int64
	ProductID             int64  `gorm:"index:idx_shop_stock_pick,priority:1"`
	State                 string `gorm:"type:varchar(16);index:idx_shop_stock_pick,priority:2"`
	SecretCiphertext      string `json:"-" gorm:"type:text"`
	SecretKeyVersion      string `json:"-" gorm:"type:varchar(32)"`
	FingerprintKeyVersion string `json:"-" gorm:"type:varchar(32);uniqueIndex:idx_shop_stock_identity,priority:2"`
	IdentityFingerprint   string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_shop_stock_identity,priority:3"`
	IdentityDomainHash    string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_shop_stock_identity,priority:1"`
	ImportBatchPublicID   string `gorm:"type:varchar(64);index"`
	NormalizationVersion  int64
	MaskedLabel           string `gorm:"type:varchar(200)"`
	ReservedOrderID       *int64 `gorm:"uniqueIndex"`
	ReservationExpiresAt  int64  `gorm:"index"`
	SoldOrderID           *int64 `gorm:"index"`
	RevealedAt            int64
	QuarantineReason      string `gorm:"type:varchar(200)"`
	SecretRetentionUntil  int64
	SecretPurgedAt        int64
	RetentionHold         bool
}
type ShopPaymentAttempt struct {
	ShopRecord
	OrderID                    int64 `gorm:"uniqueIndex:idx_shop_attempt_number,priority:1;index"`
	TenantID                   int64
	UserID                     int
	AttemptNo                  int64  `gorm:"uniqueIndex:idx_shop_attempt_number,priority:2"`
	TradeNo                    string `gorm:"type:varchar(128);not null;uniqueIndex"`
	Provider                   string `gorm:"type:varchar(50)"`
	ProviderAccountRef         string `gorm:"type:varchar(128)"`
	Environment                string `gorm:"type:varchar(32)"`
	PaymentMethod              string `gorm:"type:varchar(32)"`
	ExpectedAmountMinor        int64
	Currency                   string `gorm:"type:varchar(8)"`
	CurrencyExponent           int
	BusinessKind               string `gorm:"type:varchar(16)"`
	RequestHash                string `json:"-" gorm:"type:varchar(64)"`
	ProviderSnapshotCiphertext string `json:"-" gorm:"type:text"`
	CredentialKeyVersion       string `json:"-" gorm:"type:varchar(32)"`
	CreateState                string `gorm:"type:varchar(16);index:idx_shop_attempt_check,priority:1"`
	ProviderSessionID          string `gorm:"type:varchar(191)"`
	ProviderOrderID            string `gorm:"type:varchar(191)"`
	CheckoutCiphertext         string `json:"-" gorm:"type:text"`
	CheckoutKeyVersion         string `json:"-" gorm:"type:varchar(32)"`
	ProviderExpiresAt          int64
	CloseState                 string `gorm:"type:varchar(16)"`
	CloseEvidenceRef           string `gorm:"type:varchar(500)"`
	ClaimToken                 string `json:"-" gorm:"type:varchar(64)"`
	ClaimUntil                 int64
	NextCheckAt                int64 `gorm:"index:idx_shop_attempt_check,priority:2"`
	LastCheckedAt              int64
	CheckAttempts              int
	LastReasonCode             string `gorm:"type:varchar(64)"`
	ReviewOwnerID              int
}
type ShopPaymentReceipt struct {
	ShopRecord
	ReceiptKey            string `gorm:"type:varchar(64);not null;uniqueIndex"`
	Provider              string `gorm:"type:varchar(50)"`
	ProviderAccountRef    string `gorm:"type:varchar(128)"`
	Environment           string `gorm:"type:varchar(32)"`
	ProviderTransactionID string `gorm:"type:varchar(191)"`
	OriginalTradeRef      string `gorm:"type:varchar(128)"`
	ProviderSessionID     string `gorm:"type:varchar(191)"`
	ProviderOrderID       string `gorm:"type:varchar(191)"`
	AmountMinor           int64
	Currency              string `gorm:"type:varchar(8)"`
	CurrencyExponent      int
	ProviderPaidAt        int64
	ReceivedAt            int64
	VerificationSource    string `gorm:"type:varchar(32)"`
	EvidenceRef           string `gorm:"type:varchar(500)"`
	EvidenceHash          string `gorm:"type:varchar(64)"`
	PricingVerified       bool
	AttemptID             *int64 `gorm:"index"`
	OrderID               *int64 `gorm:"index"`
	ProcessingState       string `gorm:"type:varchar(16);index:idx_shop_receipt_retry,priority:1"`
	ExceptionCode         string `gorm:"type:varchar(64)"`
	ReviewOwnerID         int
	NextAction            string `gorm:"type:varchar(200)"`
	NextRetryAt           int64  `gorm:"index:idx_shop_receipt_retry,priority:2"`
	LastProcessedAt       int64
}
type ShopDelivery struct {
	ShopRecord
	OrderID              int64  `gorm:"uniqueIndex:idx_shop_delivery_version,priority:1;index"`
	VersionNo            int64  `gorm:"uniqueIndex:idx_shop_delivery_version,priority:2"`
	AftersaleID          *int64 `gorm:"uniqueIndex"`
	StockUnitID          *int64 `gorm:"uniqueIndex"`
	ManualCiphertext     string `json:"-" gorm:"type:text"`
	SecretKeyVersion     string `json:"-" gorm:"type:varchar(32)"`
	Instructions         string `gorm:"type:text"`
	DeliveredAt          int64
	ViewExpiresAt        int64
	RevealRevokedAt      int64
	RevealRevokedReason  string `gorm:"type:varchar(64)"`
	CreatorID            int
	FirstRevealedAt      int64
	SecretRetentionUntil int64
	SecretPurgedAt       int64
	RetentionHold        bool
}
type ShopAftersale struct {
	ShopRecord
	TenantID            int64
	UserID              int
	OrderID             int64  `gorm:"index"`
	State               string `gorm:"type:varchar(24);index"`
	IssueType           string `gorm:"type:varchar(24)"`
	RequestedResolution string `gorm:"type:varchar(16)"`
	Description         string `gorm:"type:text"`
	AssignedAdminID     int
	ResolutionCode      string `gorm:"type:varchar(32)"`
	ResolutionNote      string `gorm:"type:text"`
	ClosedAt            int64
}
type ShopEvent struct {
	ShopRecord
	TenantID        int64
	OrderID         *int64 `gorm:"index"`
	ReceiptID       *int64 `gorm:"index"`
	AftersaleID     *int64 `gorm:"index"`
	RefundCaseID    *int64 `gorm:"index"`
	Kind            string `gorm:"type:varchar(64)"`
	ActorID         int
	ActorKind       string  `gorm:"type:varchar(16)"`
	Visibility      string  `gorm:"type:varchar(16)"`
	ReasonCode      string  `gorm:"type:varchar(64)"`
	Note            string  `gorm:"type:text"`
	EvidenceIDsJSON string  `gorm:"type:text"`
	EvidenceHash    string  `gorm:"type:varchar(64)"`
	OperationKey    *string `gorm:"type:varchar(64);uniqueIndex"`
}
type ShopRefundCase struct {
	ShopRecord
	ReceiptID        int64  `gorm:"uniqueIndex"`
	OrderID          *int64 `gorm:"index"`
	AftersaleID      *int64
	Kind             string `gorm:"type:varchar(16)"`
	AmountMinor      int64
	Currency         string `gorm:"type:varchar(8)"`
	CurrencyExponent int
	State            string `gorm:"type:varchar(16);index"`
	Reason           string `gorm:"type:text"`
	ApprovedBy       int
	ApprovedAt       int64
	CancelledBy      int
	CancelledAt      int64
	CancelReason     string `gorm:"type:text"`
	ReviewOwnerID    int
	NextAction       string `gorm:"type:varchar(200)"`
}
type ShopRefundAttempt struct {
	ShopRecord
	RefundCaseID      int64 `gorm:"uniqueIndex:idx_shop_refund_number,priority:1"`
	AttemptNo         int64 `gorm:"uniqueIndex:idx_shop_refund_number,priority:2"`
	AmountMinor       int64
	Currency          string `gorm:"type:varchar(8)"`
	CurrencyExponent  int
	State             string `gorm:"type:varchar(16);index"`
	ProviderRefundRef string `gorm:"type:varchar(191)"`
	AdjustmentID      *int64 `gorm:"uniqueIndex"`
	RegisteredBy      int
	RegisteredAt      int64
	OperatedAt        int64
	VerifiedBy        int
	VerifiedAt        int64
	EvidenceIDsJSON   string `gorm:"type:text"`
	FailureCode       string `gorm:"type:varchar(64)"`
	NextAction        string `gorm:"type:varchar(200)"`
}
type ShopPaymentAdjustment struct {
	ShopRecord
	ReceiptID             int64  `gorm:"index"`
	AdjustmentKey         string `gorm:"type:varchar(64);uniqueIndex"`
	Kind                  string `gorm:"type:varchar(16)"`
	ProviderAdjustmentRef string `gorm:"type:varchar(191)"`
	AmountMinor           int64
	Currency              string `gorm:"type:varchar(8)"`
	CurrencyExponent      int
	State                 string `gorm:"type:varchar(16);index"`
	Source                string `gorm:"type:varchar(32)"`
	EvidenceIDsJSON       string `gorm:"type:text"`
	VerifiedBy            int
	VerifiedAt            int64
}
type ShopPaymentEvidence struct {
	ShopRecord
	ReceiptID             int64  `gorm:"index"`
	Source                string `gorm:"type:varchar(32)"`
	Provider              string `gorm:"type:varchar(50)"`
	ProviderAccountRef    string `gorm:"type:varchar(128)"`
	Environment           string `gorm:"type:varchar(32)"`
	ProviderTransactionID string `gorm:"type:varchar(191)"`
	ProviderAdjustmentRef string `gorm:"type:varchar(191)"`
	AmountMinor           int64
	Currency              string `gorm:"type:varchar(8)"`
	CurrencyExponent      int
	ObservedStatus        string `gorm:"type:varchar(32)"`
	ObservedAt            int64
	Reference             string `gorm:"type:varchar(500)"`
	Summary               string `gorm:"type:text"`
	CorrectionOfID        *int64 `gorm:"index"`
	CreatedBy             int
}
type ShopIdempotencyKey struct {
	ID                    int64  `gorm:"primaryKey"`
	IdentityHash          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID              int64
	ActorUserID           int
	Scope                 string `gorm:"type:varchar(64)"`
	KeyHash               string `gorm:"type:varchar(64)"`
	RequestHash           string `json:"-" gorm:"type:varchar(64)"`
	RequestHashKeyVersion string `json:"-" gorm:"type:varchar(32)"`
	ResourceType          string `gorm:"type:varchar(32)"`
	ResourcePublicID      string `gorm:"type:varchar(64)"`
	ResultMetadataJSON    string `gorm:"type:text"`
	CreatedAt             int64  `gorm:"autoCreateTime:false"`
	ExpiresAt             int64
}

func ShopSchemaModels() []any {
	return []any{&ShopSettings{}, &ShopUserState{}, &ShopCategory{}, &ShopProduct{}, &ShopOrder{}, &ShopStockUnit{}, &ShopPaymentAttempt{}, &ShopPaymentReceipt{}, &ShopDelivery{}, &ShopAftersale{}, &ShopEvent{}, &ShopRefundCase{}, &ShopRefundAttempt{}, &ShopPaymentAdjustment{}, &ShopPaymentEvidence{}, &ShopIdempotencyKey{}}
}
func NewShopPublicID(prefix string) (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func shopRecord(prefix string, now int64) (ShopRecord, error) {
	id, err := NewShopPublicID(prefix)
	return ShopRecord{PublicID: id, Version: 1, CreatedAt: now, UpdatedAt: now}, err
}
func shopNow(now int64) int64 {
	if now > 0 {
		return now
	}
	return time.Now().Unix()
}
func shopDigest(v any) string {
	b, _ := common.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func shopTransaction(db *gorm.DB, fn func(*gorm.DB) error) error {
	if db == nil {
		return ErrShopInvalid
	}
	var options *sql.TxOptions
	if db.Dialector.Name() == "mysql" || db.Dialector.Name() == "postgres" {
		// Locking establishes ownership; subsequent responsibility and replay
		// reads must see commits made while this transaction waited for a lock.
		options = &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	}
	var err error
	for n := 0; n < 3; n++ {
		err = db.Transaction(func(tx *gorm.DB) error {
			if fence, ok := tx.Statement.Context.Value(shopWorkerFenceContextKey{}).(ShopWorkerFence); ok {
				if err := RequireShopWorkerFenceTx(tx, fence); err != nil {
					return err
				}
			}
			return fn(tx)
		}, options)
		if err == nil || db.Dialector.Name() != "sqlite" || !isSQLiteBusyError(err) {
			return err
		}
		if db.Statement.Context.Err() != nil {
			return db.Statement.Context.Err()
		}
	}
	return err
}
func shopAuthorize(tx *gorm.DB, c ShopCommand, admin bool) error {
	if c.ActorID <= 0 || c.TenantID != PlatformTenantID {
		return ErrShopNotFound
	}
	var u User
	if err := tx.Select("id", "tenant_id", "status", "role").First(&u, c.ActorID).Error; err != nil {
		return shopNotFound(err)
	}
	if u.TenantID != PlatformTenantID || u.Status != common.UserStatusEnabled || (admin && (!c.Admin || u.Role < common.RoleAdminUser)) {
		return ErrShopNotFound
	}
	return nil
}
func shopLockUser(tx *gorm.DB, userID int, active bool, now int64) error {
	var u User
	err := lockForUpdate(tx.Unscoped()).Select("id", "tenant_id", "status", "deleted_at").First(&u, userID).Error
	missing := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !missing {
		return err
	}
	if missing && active || !missing && u.TenantID != PlatformTenantID {
		return ErrShopNotFound
	}
	s := ShopUserState{UserID: userID, TenantID: PlatformTenantID, Version: 1, UpdatedAt: now}
	if missing {
		s.RetiredAt = now
	}
	if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error; err != nil {
		return err
	}
	fence := tx.Model(&ShopUserState{}).Where("user_id = ? AND tenant_id = ?", userID, PlatformTenantID).Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now})
	if fence.Error != nil {
		return fence.Error
	}
	if fence.RowsAffected != 1 {
		return ErrShopVersion
	}
	if err = tx.First(&s, userID).Error; err != nil {
		return err
	}
	err = tx.Unscoped().Select("id", "status", "deleted_at").First(&u, userID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if active && (err != nil || u.Status != common.UserStatusEnabled || u.DeletedAt.Valid || s.RetiredAt > 0) {
		return ErrShopNotFound
	}
	return nil
}
func shopVersion(actual, want int64) error {
	if actual != want || want <= 0 {
		return ErrShopVersion
	}
	return nil
}
func shopCAS(tx *gorm.DB, m any, id, version, now int64, fields map[string]any) error {
	fields["version"] = gorm.Expr("version + 1")
	fields["updated_at"] = now
	r := tx.Model(m).Where("id = ? AND version = ?", id, version).Updates(fields)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrShopVersion
	}
	return nil
}
func shopIdempotency(tx *gorm.DB, c ShopCommand, scope, target string) (*ShopIdempotencyKey, error) {
	if len(c.Key) < 8 || len(c.Key) > 128 || len(c.RequestHash) != 64 {
		return nil, ErrShopInvalid
	}
	var k ShopIdempotencyKey
	err := tx.Where("identity_hash = ?", shopDigest([]any{c.TenantID, c.ActorID, scope, target, c.Key})).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if k.RequestHash != c.RequestHash || k.RequestHashKeyVersion != c.HashKeyVersion {
		return nil, ErrShopIdempotency
	}
	return &k, nil
}
func shopRemember(tx *gorm.DB, c ShopCommand, scope, target, kind, publicID string, metadata any) error {
	b, err := common.Marshal(metadata)
	if err != nil {
		return err
	}
	return tx.Create(&ShopIdempotencyKey{IdentityHash: shopDigest([]any{c.TenantID, c.ActorID, scope, target, c.Key}), TenantID: c.TenantID, ActorUserID: c.ActorID, Scope: scope, KeyHash: shopDigest(c.Key), RequestHash: c.RequestHash, RequestHashKeyVersion: c.HashKeyVersion, ResourceType: kind, ResourcePublicID: publicID, ResultMetadataJSON: string(b), CreatedAt: shopNow(c.Now)}).Error
}
func GetShopIdempotency(db *gorm.DB, scope ShopScope, action, target, key string) (*ShopIdempotencyKey, error) {
	if err := shopAuthorize(db, ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin}, scope.Admin); err != nil {
		return nil, err
	}
	var k ShopIdempotencyKey
	err := db.Where("identity_hash = ?", shopDigest([]any{scope.TenantID, scope.UserID, action, target, key})).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &k, err
}
func shopEvent(tx *gorm.DB, c ShopCommand, orderID, receiptID *int64, kind, visibility, note string, evidence []string) error {
	r, err := shopRecord("se_", shopNow(c.Now))
	if err != nil {
		return err
	}
	ids, err := shopEvidenceJSON(tx, receiptID, evidence)
	if err != nil {
		return err
	}
	actor := "user"
	if c.Admin {
		actor = "admin"
	}
	if c.ActorID == 0 {
		actor = "system"
	}
	return tx.Create(&ShopEvent{ShopRecord: r, TenantID: PlatformTenantID, OrderID: orderID, ReceiptID: receiptID, Kind: kind, ActorID: c.ActorID, ActorKind: actor, Visibility: visibility, Note: note, EvidenceIDsJSON: ids}).Error
}
func shopEnqueue(tx *gorm.DB, topic, key, aggregate string, payload any, now int64) error {
	b, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&KKAIOutboxEvent{EventKey: key, Topic: topic, AggregateID: aggregate, Payload: string(b), Status: KKAIOutboxStatusPending, AvailableAt: now, CreatedAt: now}).Error
}
func shopBoundedText(s string, n int) bool { return len([]rune(s)) <= n }
func shopRequired(s string, n int) bool    { return strings.TrimSpace(s) != "" && shopBoundedText(s, n) }
func shopNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrShopNotFound
	}
	return err
}
func shopRef(id int64) *int64 { return &id }
func shopEventKey(kind string, id, version int64) string {
	return fmt.Sprintf("shop:%s:%d:%d", kind, id, version)
}
