package kkaimigrate

import "time"

// Frozen v12 schema snapshots. Never edit these after release; add a new migration.
// These deliberately do not import application models or execute their hooks.

type tenantV12Tenant struct {
	ID          int64  `gorm:"primaryKey"`
	Status      string `gorm:"type:varchar(16);not null;index"`
	DisplayName string `gorm:"type:varchar(128);not null;default:''"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (tenantV12Tenant) TableName() string { return "tenants" }

type tenantV12TenantDomain struct {
	ID        int64  `gorm:"primaryKey"`
	TenantID  int64  `gorm:"not null;index"`
	Hostname  string `gorm:"type:varchar(253);not null;uniqueIndex"`
	Prefix    string `gorm:"type:varchar(63);not null;default:''"`
	Status    string `gorm:"type:varchar(16);not null;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (tenantV12TenantDomain) TableName() string { return "tenant_domains" }

type tenantV12TenantMembership struct {
	ID        int64  `gorm:"primaryKey"`
	UserID    int    `gorm:"not null;index;uniqueIndex:idx_tenant_memberships_identity,priority:1"`
	TenantID  int64  `gorm:"not null;index;uniqueIndex:idx_tenant_memberships_identity,priority:2"`
	Role      string `gorm:"type:varchar(16);not null"`
	Status    string `gorm:"type:varchar(16);not null;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (tenantV12TenantMembership) TableName() string { return "tenant_memberships" }

type tenantV12ResellerApplication struct {
	ID              int64  `gorm:"primaryKey"`
	ApplicantUserID int    `gorm:"not null;index"`
	TenantID        int64  `gorm:"not null;default:0;index"`
	BusinessName    string `gorm:"type:varchar(128);not null;default:''"`
	ContactName     string `gorm:"type:varchar(64);not null;default:''"`
	ContactEmail    string `gorm:"type:varchar(254);not null;default:''"`
	Notes           string `gorm:"type:text;not null"`
	Status          string `gorm:"type:varchar(16);not null;index"`
	ReviewedBy      int    `gorm:"not null;default:0"`
	ReviewedAt      *time.Time
	RejectionReason string `gorm:"type:text;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (tenantV12ResellerApplication) TableName() string { return "reseller_applications" }

type tenantV12TenantProfile struct {
	ID              int64  `gorm:"primaryKey"`
	TenantID        int64  `gorm:"not null;uniqueIndex"`
	SiteName        string `gorm:"type:varchar(128);not null;default:''"`
	LogoURL         string `gorm:"type:varchar(2048);not null;default:''"`
	LogoData        []byte
	LogoMIME        string `gorm:"type:varchar(32)"`
	Contact         string `gorm:"type:varchar(512);not null;default:''"`
	Announcement    string `gorm:"type:text;not null"`
	HomepageContent string `gorm:"type:text;not null"`
	UpdatedBy       int    `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (tenantV12TenantProfile) TableName() string { return "tenant_profiles" }

type tenantV12TenantCatalogEntry struct {
	ID         int64  `gorm:"primaryKey"`
	TenantID   int64  `gorm:"not null;index;uniqueIndex:idx_tenant_catalog_model,priority:1"`
	SalesGroup string `gorm:"column:sales_group;type:varchar(64);not null;uniqueIndex:idx_tenant_catalog_model,priority:2"`
	Model      string `gorm:"type:varchar(128);not null;uniqueIndex:idx_tenant_catalog_model,priority:3"`
	Status     string `gorm:"type:varchar(16);not null;index"`
	IsDefault  bool   `gorm:"not null"`
	SortOrder  int    `gorm:"not null;default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (tenantV12TenantCatalogEntry) TableName() string { return "tenant_catalog_entries" }

type tenantV12TenantPriceVersion struct {
	ID                 int64  `gorm:"primaryKey"`
	TenantID           int64  `gorm:"not null;index;uniqueIndex:idx_tenant_price_version,priority:1"`
	Version            int64  `gorm:"not null;uniqueIndex:idx_tenant_price_version,priority:4"`
	Model              string `gorm:"type:varchar(128);not null;uniqueIndex:idx_tenant_price_version,priority:2"`
	Group              string `gorm:"column:group;type:varchar(64);not null;uniqueIndex:idx_tenant_price_version,priority:3"`
	SupplierPriceMinor int64  `gorm:"not null"`
	SalesMultiplier    string `gorm:"type:varchar(64);not null"`
	EffectiveAt        int64  `gorm:"not null;index"`
	CreatedAt          int64  `gorm:"not null"`
}

func (tenantV12TenantPriceVersion) TableName() string { return "tenant_price_versions" }

type tenantV12PricingQuote struct {
	ID                    int64  `gorm:"primaryKey"`
	TenantID              int64  `gorm:"not null;index"`
	CustomerID            int    `gorm:"not null;index"`
	RequestID             string `gorm:"type:varchar(128);not null;uniqueIndex"`
	PriceVersionID        int64  `gorm:"not null;index"`
	Model                 string `gorm:"type:varchar(128);not null"`
	Group                 string `gorm:"column:group;type:varchar(64);not null"`
	SupplierPriceMinor    int64  `gorm:"not null"`
	SupplierMinorPerQuota string `gorm:"type:varchar(64);not null"`
	SaleMinorPerQuota     string `gorm:"type:varchar(64);not null"`
	SupplySnapshot        string `gorm:"type:text;not null"`
	SalesMultiplier       string `gorm:"type:varchar(64);not null"`
	SaleMinor             int64  `gorm:"not null"`
	PaymentFeeMinor       int64  `gorm:"not null"`
	ProfitMinor           int64  `gorm:"not null"`
	ProfitMinorExact      string `gorm:"type:varchar(128);not null"`
	QuotedQuota           int64  `gorm:"not null"`
	Currency              string `gorm:"type:varchar(8);not null"`
	PayloadHash           string `gorm:"type:char(64);not null"`
	CreatedAt             int64  `gorm:"not null"`
}

func (tenantV12PricingQuote) TableName() string { return "pricing_quotes" }

type tenantV12CustomerPaymentLot struct {
	ID              int64  `gorm:"primaryKey"`
	TenantID        int64  `gorm:"not null;index"`
	CustomerID      int    `gorm:"not null;index"`
	TopUpID         int64  `gorm:"not null;uniqueIndex"`
	OperationID     string `gorm:"type:varchar(128);not null;uniqueIndex"`
	PaidMinor       int64  `gorm:"not null"`
	PaymentFeeMinor int64  `gorm:"not null"`
	CreditedQuota   int64  `gorm:"not null"`
	RefundedQuota   int64  `gorm:"not null;default:0"`
	ConsumedQuota   int64  `gorm:"not null"`
	RemainingQuota  int64  `gorm:"not null"`
	RefundStatus    string `gorm:"type:varchar(16);not null"`
	CreatedAt       int64  `gorm:"not null"`
	UpdatedAt       int64  `gorm:"not null"`
	PayloadHash     string `gorm:"type:char(64);not null"`
}

func (tenantV12CustomerPaymentLot) TableName() string { return "customer_payment_lots" }

type tenantV12ResellerWallet struct {
	ID               int64  `gorm:"primaryKey"`
	TenantID         int64  `gorm:"not null;uniqueIndex"`
	PendingMinor     int64  `gorm:"not null"`
	AvailableMinor   int64  `gorm:"not null"`
	FrozenMinor      int64  `gorm:"not null"`
	DebtMinor        int64  `gorm:"not null"`
	ExactProfitCarry string `gorm:"type:varchar(128);not null;default:'0'"`
	Version          int64  `gorm:"not null"`
	CreatedAt        int64  `gorm:"not null"`
	UpdatedAt        int64  `gorm:"not null"`
}

func (tenantV12ResellerWallet) TableName() string { return "reseller_wallets" }

type tenantV12ResellerLedgerEntry struct {
	ID                 int64  `gorm:"primaryKey"`
	TenantID           int64  `gorm:"not null;index"`
	WalletID           int64  `gorm:"not null;index"`
	OperationID        string `gorm:"type:varchar(192);not null;uniqueIndex"`
	Kind               string `gorm:"type:varchar(32);not null;index"`
	AmountMinor        int64  `gorm:"not null"`
	SaleMinor          int64  `gorm:"not null"`
	PaymentFeeMinor    int64  `gorm:"not null"`
	SupplierMinorExact string `gorm:"type:varchar(128);not null"`
	ExactProfitMinor   string `gorm:"type:varchar(128);not null"`
	PaymentAllocations string `gorm:"type:text;not null"`
	ActorID            int    `gorm:"not null"`
	Reason             string `gorm:"type:text;not null"`
	PendingDelta       int64  `gorm:"not null"`
	AvailableDelta     int64  `gorm:"not null"`
	FrozenDelta        int64  `gorm:"not null"`
	DebtDelta          int64  `gorm:"not null"`
	QuoteID            int64  `gorm:"not null;index"`
	SourceReference    string `gorm:"type:varchar(192);not null;index"`
	ReversalOf         string `gorm:"type:varchar(192);not null;index"`
	AvailableAt        int64  `gorm:"not null;index"`
	PayloadHash        string `gorm:"type:char(64);not null"`
	CreatedAt          int64  `gorm:"not null;index"`
}

func (tenantV12ResellerLedgerEntry) TableName() string { return "reseller_ledger_entries" }

type tenantV12ResellerWithdrawal struct {
	ID                int64  `gorm:"primaryKey"`
	TenantID          int64  `gorm:"not null;index;uniqueIndex:idx_reseller_withdrawal_idempotency,priority:1"`
	WalletID          int64  `gorm:"not null;index"`
	IdempotencyKey    string `gorm:"type:varchar(128);not null;uniqueIndex:idx_reseller_withdrawal_idempotency,priority:2"`
	AmountMinor       int64  `gorm:"not null"`
	FeeMinor          int64  `gorm:"not null"`
	NetMinor          int64  `gorm:"not null"`
	Status            string `gorm:"type:varchar(16);not null;index"`
	PaymentRef        string `gorm:"type:varchar(192);not null"`
	AlipayAccount     string `gorm:"type:varchar(254);not null"`
	AlipayName        string `gorm:"type:varchar(128);not null"`
	FeePolicySnapshot string `gorm:"type:varchar(512);not null"`
	ReviewedBy        int    `gorm:"not null"`
	ReviewedAt        int64  `gorm:"not null"`
	PaidAt            int64  `gorm:"not null"`
	Timeline          string `gorm:"type:text;not null"`
	PayloadHash       string `gorm:"type:char(64);not null"`
	ReviewNote        string `gorm:"type:text;not null"`
	CreatedAt         int64  `gorm:"not null"`
	UpdatedAt         int64  `gorm:"not null"`
}

func (tenantV12ResellerWithdrawal) TableName() string { return "reseller_withdrawals" }

type tenantV12TenantConsumption struct {
	ID            int64  `gorm:"primaryKey"`
	RequestID     string `gorm:"type:varchar(128);not null;uniqueIndex"`
	TenantID      int64  `gorm:"not null;index"`
	CustomerID    int    `gorm:"not null;index"`
	TokenID       int    `gorm:"not null"`
	QuoteID       int64  `gorm:"not null"`
	ReservedQuota int64  `gorm:"not null"`
	ActualQuota   int64  `gorm:"not null"`
	Status        string `gorm:"type:varchar(24);not null;index"`
	ChannelID     int    `gorm:"not null"`
	LastError     string `gorm:"type:text"`
	CreatedAt     int64
	UpdatedAt     int64
}

func (tenantV12TenantConsumption) TableName() string { return "tenant_consumptions" }

func tenantV12NewModels() []any {
	return []any{&tenantV12Tenant{}, &tenantV12TenantDomain{}, &tenantV12TenantMembership{}, &tenantV12ResellerApplication{}, &tenantV12TenantProfile{}, &tenantV12TenantCatalogEntry{}, &tenantV12TenantPriceVersion{}, &tenantV12PricingQuote{}, &tenantV12CustomerPaymentLot{}, &tenantV12ResellerWallet{}, &tenantV12ResellerLedgerEntry{}, &tenantV12ResellerWithdrawal{}, &tenantV12TenantConsumption{}, &tenantV12CustomerPaymentReversal{}}
}

type tenantV12CustomerPaymentReversal struct {
	ID                       int64  `gorm:"primaryKey"`
	TenantID                 int64  `gorm:"not null;index;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:1"`
	IdempotencyKey           string `gorm:"type:varchar(128);not null;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:2"`
	PaymentLotID             int64  `gorm:"not null;uniqueIndex"`
	TopUpID                  int64  `gorm:"not null;uniqueIndex"`
	CustomerID               int    `gorm:"not null;index"`
	Kind                     string `gorm:"type:varchar(16);not null"`
	PaidMinor                int64  `gorm:"not null"`
	PaymentFeeMinor          int64  `gorm:"not null"`
	RemovedQuota             int64  `gorm:"not null"`
	ReversedProfitMinorExact string `gorm:"type:varchar(128);not null"`
	LedgerEntryIDs           string `gorm:"type:text;not null"`
	ActorID                  int    `gorm:"not null"`
	Reason                   string `gorm:"type:text;not null"`
	PaymentReference         string `gorm:"type:varchar(192);not null"`
	PayloadHash              string `gorm:"type:char(64);not null"`
	CreatedAt                int64  `gorm:"not null"`
}

func (tenantV12CustomerPaymentReversal) TableName() string { return "customer_payment_reversals" }
