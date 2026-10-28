package kkaimigrate

// Frozen schema 19 contract. Runtime model edits must never change this shape.

type ShopRecordV19 struct {
	ID        int64  `gorm:"primaryKey"`
	PublicID  string `gorm:"type:varchar(64);not null;uniqueIndex"`
	Version   int64  `gorm:"not null"`
	CreatedAt int64  `gorm:"autoCreateTime:false;not null"`
	UpdatedAt int64  `gorm:"autoUpdateTime:false;not null"`
}
type shopSettingsV19 struct {
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

func (shopSettingsV19) TableName() string { return "shop_settings" }

type shopUserStateV19 struct {
	UserID    int `gorm:"primaryKey;autoIncrement:false"`
	TenantID  int64
	Version   int64
	RetiredAt int64
	UpdatedAt int64 `gorm:"autoUpdateTime:false"`
}

func (shopUserStateV19) TableName() string { return "shop_user_states" }

type shopCategoryV19 struct {
	ShopRecordV19
	TenantID  int64  `gorm:"index:idx_shop_category_sort,priority:1"`
	Name      string `gorm:"type:varchar(120)"`
	SortOrder int    `gorm:"index:idx_shop_category_sort,priority:2"`
}

func (shopCategoryV19) TableName() string { return "shop_categories" }

type shopProductV19 struct {
	ShopRecordV19
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

func (shopProductV19) TableName() string { return "shop_products" }

type shopOrderV19 struct {
	ShopRecordV19
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

func (shopOrderV19) TableName() string { return "shop_orders" }

type shopStockUnitV19 struct {
	ShopRecordV19
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

func (shopStockUnitV19) TableName() string { return "shop_stock_units" }

type shopPaymentAttemptV19 struct {
	ShopRecordV19
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

func (shopPaymentAttemptV19) TableName() string { return "shop_payment_attempts" }

type shopPaymentReceiptV19 struct {
	ShopRecordV19
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

func (shopPaymentReceiptV19) TableName() string { return "shop_payment_receipts" }

type shopDeliveryV19 struct {
	ShopRecordV19
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

func (shopDeliveryV19) TableName() string { return "shop_deliveries" }

type shopAftersaleV19 struct {
	ShopRecordV19
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

func (shopAftersaleV19) TableName() string { return "shop_aftersales" }

type shopEventV19 struct {
	ShopRecordV19
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

func (shopEventV19) TableName() string { return "shop_events" }

type shopRefundCaseV19 struct {
	ShopRecordV19
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

func (shopRefundCaseV19) TableName() string { return "shop_refund_cases" }

type shopRefundAttemptV19 struct {
	ShopRecordV19
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

func (shopRefundAttemptV19) TableName() string { return "shop_refund_attempts" }

type shopPaymentAdjustmentV19 struct {
	ShopRecordV19
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

func (shopPaymentAdjustmentV19) TableName() string { return "shop_payment_adjustments" }

type shopPaymentEvidenceV19 struct {
	ShopRecordV19
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

func (shopPaymentEvidenceV19) TableName() string { return "shop_payment_evidences" }

type shopIdempotencyKeyV19 struct {
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

func (shopIdempotencyKeyV19) TableName() string { return "shop_idempotency_keys" }

func shopV19Models() []any {
	return []any{&shopSettingsV19{}, &shopUserStateV19{}, &shopCategoryV19{}, &shopProductV19{}, &shopOrderV19{}, &shopStockUnitV19{}, &shopPaymentAttemptV19{}, &shopPaymentReceiptV19{}, &shopDeliveryV19{}, &shopAftersaleV19{}, &shopEventV19{}, &shopRefundCaseV19{}, &shopRefundAttemptV19{}, &shopPaymentAdjustmentV19{}, &shopPaymentEvidenceV19{}, &shopIdempotencyKeyV19{}}
}
