package model

import "gorm.io/gorm"

const (
	InvoicePendingReview          = "pending_review"
	InvoiceApprovedForIssue       = "approved_for_issue"
	InvoiceDelivered              = "delivered"
	InvoiceConfirmed              = "confirmed"
	InvoiceDisputed               = "disputed"
	InvoiceAwaitingReconfirmation = "awaiting_reconfirmation"
	InvoiceFinanceHold            = "finance_hold"
	InvoiceRejected               = "rejected"
)

type InvoiceRequest struct {
	ID                     int64  `gorm:"primaryKey"`
	PublicID               string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID               int64  `gorm:"not null;index:idx_invoice_owner,priority:1;index:idx_invoice_status,priority:1"`
	UserID                 int    `gorm:"not null;index:idx_invoice_owner,priority:2"`
	Status                 string `gorm:"type:varchar(32);not null;index:idx_invoice_status,priority:2"`
	Version                int64  `gorm:"not null"`
	PolicyVersion          string `gorm:"type:varchar(64);not null"`
	SellerID               string `gorm:"type:varchar(64);not null"`
	SellerName             string `gorm:"type:varchar(200);not null"`
	TaxGroup               string `gorm:"type:varchar(64);not null"`
	ProfileID              string `gorm:"type:varchar(64);not null"`
	ProfileCipher          string `gorm:"type:text;not null"`
	NoteCipher             string `gorm:"type:text;not null"`
	InvoiceType            string `gorm:"type:varchar(64);not null"`
	Currency               string `gorm:"type:varchar(8);not null"`
	CurrencyExponent       int    `gorm:"not null"`
	AmountMinor            int64  `gorm:"not null"`
	CurrentDeliveryVersion int64  `gorm:"not null"`
	ActiveDisputeID        int64  `gorm:"not null"`
	HoldFromStatus         string `gorm:"type:varchar(32);not null"`
	ReasonCipher           string `gorm:"type:text;not null"`
	EverDelivered          bool   `gorm:"not null"`
	IssuedEvidence         bool   `gorm:"not null"`
	CreatedAt              int64  `gorm:"not null;autoCreateTime:false;index:idx_invoice_owner,priority:3;index:idx_invoice_status,priority:3"`
	UpdatedAt              int64  `gorm:"not null;autoUpdateTime:false"`
	ReviewedAt             int64  `gorm:"not null"`
	DeliveredAt            int64  `gorm:"not null"`
	ConfirmedAt            int64  `gorm:"not null"`
}

type InvoiceRequestItem struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null"`
	UserID              int    `gorm:"not null"`
	RequestID           int64  `gorm:"not null;index"`
	CanonicalPaymentKey string `gorm:"type:varchar(64);not null;index"`
	SourceType          string `gorm:"type:varchar(32);not null"`
	SourceID            int64  `gorm:"not null"`
	OrderID             string `gorm:"type:varchar(128);not null"`
	OrderNo             string `gorm:"type:varchar(255);not null;index"`
	AmountMinor         int64  `gorm:"not null"`
	Currency            string `gorm:"type:varchar(8);not null"`
	PaidAt              int64  `gorm:"not null"`
	PaymentLabel        string `gorm:"type:varchar(64);not null"`
	TaxGroup            string `gorm:"type:varchar(64);not null"`
}

type InvoiceOrderClaim struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null;uniqueIndex:idx_invoice_claim,priority:1"`
	CanonicalPaymentKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_claim,priority:2"`
	RequestID           int64  `gorm:"not null;index"`
	CreatedAt           int64  `gorm:"not null;autoCreateTime:false"`
}

type InvoiceAttachment struct {
	ID         int64  `gorm:"primaryKey"`
	PublicID   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID   int64  `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:1"`
	RequestID  int64  `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:2"`
	UploaderID int    `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:3"`
	Purpose    string `gorm:"type:varchar(32);not null;uniqueIndex:idx_invoice_upload,priority:4"`
	ObjectKey  string `gorm:"type:varchar(255);not null"`
	FileName   string `gorm:"type:varchar(255);not null"`
	MIMEType   string `gorm:"type:varchar(64);not null"`
	Size       int64  `gorm:"not null"`
	SHA256     string `gorm:"type:varchar(64);not null"`
	Status     string `gorm:"type:varchar(32);not null"`
	UploadKey  string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_upload,priority:5"`
	BoundAt    int64  `gorm:"not null"`
	ExpiresAt  int64  `gorm:"not null;index"`
	CreatedAt  int64  `gorm:"not null;autoCreateTime:false"`
}

type InvoiceDelivery struct {
	ID              int64  `gorm:"primaryKey"`
	TenantID        int64  `gorm:"not null"`
	RequestID       int64  `gorm:"not null;uniqueIndex:idx_invoice_delivery,priority:1"`
	DeliveryVersion int64  `gorm:"not null;uniqueIndex:idx_invoice_delivery,priority:2"`
	AttachmentID    int64  `gorm:"not null"`
	InvoiceNo       string `gorm:"type:varchar(128);not null"`
	IssuedOn        string `gorm:"type:varchar(10);not null"`
	AmountMinor     int64  `gorm:"not null"`
	Currency        string `gorm:"type:varchar(8);not null"`
	TaxMinor        int64  `gorm:"not null"`
	ActorID         int    `gorm:"not null"`
	CreatedAt       int64  `gorm:"not null;autoCreateTime:false"`
}

type InvoiceDispute struct {
	ID                   int64  `gorm:"primaryKey"`
	PublicID             string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID             int64  `gorm:"not null"`
	RequestID            int64  `gorm:"not null;index"`
	DeliveryVersion      int64  `gorm:"not null"`
	Round                int64  `gorm:"not null"`
	Category             string `gorm:"type:varchar(32);not null"`
	DescriptionCipher    string `gorm:"type:text;not null"`
	EvidenceAttachmentID int64  `gorm:"not null"`
	Status               string `gorm:"type:varchar(32);not null"`
	ReplyCipher          string `gorm:"type:text;not null"`
	ActorID              int    `gorm:"not null"`
	CreatedAt            int64  `gorm:"not null;autoCreateTime:false"`
	UpdatedAt            int64  `gorm:"not null;autoUpdateTime:false"`
}

type InvoiceEvent struct {
	ID                int64  `gorm:"primaryKey"`
	PublicID          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID          int64  `gorm:"not null;index:idx_invoice_event,priority:1"`
	RequestID         int64  `gorm:"not null;index:idx_invoice_event,priority:2"`
	UserID            int    `gorm:"not null;index"`
	ActorID           int    `gorm:"not null"`
	ActorAdmin        bool   `gorm:"not null"`
	Kind              string `gorm:"type:varchar(32);not null"`
	Status            string `gorm:"type:varchar(32);not null"`
	DeliveryVersion   int64  `gorm:"not null"`
	DescriptionCipher string `gorm:"type:text;not null"`
	AttachmentID      int64  `gorm:"not null"`
	CreatedAt         int64  `gorm:"not null;autoCreateTime:false;index:idx_invoice_event,priority:3"`
}

type InvoiceEventRead struct {
	ID       int64 `gorm:"primaryKey"`
	TenantID int64 `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:1"`
	EventID  int64 `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:2"`
	UserID   int   `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:3"`
	ReadAt   int64 `gorm:"not null"`
}

type InvoiceOperation struct {
	ID           int64  `gorm:"primaryKey"`
	TenantID     int64  `gorm:"not null;uniqueIndex:idx_invoice_operation,priority:1"`
	ActorID      int    `gorm:"not null;uniqueIndex:idx_invoice_operation,priority:2"`
	Operation    string `gorm:"type:varchar(128);not null;uniqueIndex:idx_invoice_operation,priority:3"`
	OperationKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_operation,priority:4"`
	Digest       string `gorm:"type:varchar(64);not null"`
	ResultID     string `gorm:"type:varchar(64);not null"`
	ResultCode   string `gorm:"type:varchar(64);not null"`
	CreatedAt    int64  `gorm:"not null;autoCreateTime:false"`
}

// InvoiceLockForUpdate is the common cross-dialect row lock used by invoice services.
func InvoiceLockForUpdate(tx *gorm.DB) *gorm.DB { return lockForUpdate(tx) }

type InvoiceProfile struct {
	ID         int64  `gorm:"primaryKey"`
	PublicID   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID   int64  `gorm:"not null;index:idx_invoice_profile_owner,priority:1"`
	UserID     int    `gorm:"not null;index:idx_invoice_profile_owner,priority:2"`
	DataCipher string `gorm:"type:text;not null"`
	Version    int64  `gorm:"not null"`
	DeletedAt  int64  `gorm:"not null"`
	CreatedAt  int64  `gorm:"not null;autoCreateTime:false"`
	UpdatedAt  int64  `gorm:"not null;autoUpdateTime:false"`
}

// InvoicePaymentReceipt is created only from an authenticated provider callback,
// never checkout estimates or administrator completion. ReceiptKey hashes the
// provider transaction identity so the same payment cannot fund two orders.
type InvoicePaymentReceipt struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null"`
	UserID              int    `gorm:"not null"`
	TopUpID             int64  `gorm:"not null;uniqueIndex"`
	Provider            string `gorm:"type:varchar(64);not null"`
	ReceiptKey          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	AmountMinor         int64  `gorm:"not null"`
	Currency            string `gorm:"type:varchar(8);not null"`
	Status              string `gorm:"type:varchar(32);not null"`
	ExceptionReceiptKey string `gorm:"type:varchar(64);not null"`
	PaidAt              int64  `gorm:"not null"`
	CreatedAt           int64  `gorm:"not null;autoCreateTime:false"`
}
