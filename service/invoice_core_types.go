package service

import (
	"context"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"time"
)

const InvoiceNotificationTopic = "invoice.notification.v1"

type InvoiceService struct{ DB *gorm.DB }
type InvoiceScope struct {
	TenantID int64
	UserID   int
	Admin    bool
}
type InvoiceError struct {
	Status      int
	Code        string
	Message     string
	FieldErrors map[string]string
}

func (e *InvoiceError) Error() string { return e.Code }
func invoiceError(status int, code string) error {
	return &InvoiceError{Status: status, Code: code, Message: code}
}

type InvoiceProfileData struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	TaxID       string `json:"tax_id"`
	Email       string `json:"email"`
	Address     string `json:"address,omitempty"`
	Phone       string `json:"phone,omitempty"`
	BankName    string `json:"bank_name,omitempty"`
	BankAccount string `json:"bank_account,omitempty"`
}
type InvoiceProfileDTO struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	InvoiceProfileData
}
type InvoiceTypePolicy struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	RequiredFields []string `json:"required_fields"`
}
type InvoiceCurrencyPolicy struct {
	Currency string `json:"currency"`
	Exponent int    `json:"exponent"`
}
type InvoicePolicy struct {
	Enabled            bool                    `json:"enabled"`
	DisabledReason     string                  `json:"disabled_reason,omitempty"`
	PolicyVersion      string                  `json:"policy_version"`
	MinimumAmountMinor string                  `json:"minimum_amount_minor"`
	SellerID           string                  `json:"seller_id"`
	SellerName         string                  `json:"seller_name"`
	TaxGroup           string                  `json:"tax_group"`
	InvoiceTypes       []InvoiceTypePolicy     `json:"invoice_types"`
	Currencies         []InvoiceCurrencyPolicy `json:"currencies"`
	Sources            []string                `json:"sources"`
	Providers          []string                `json:"providers"`
	Timezone           string                  `json:"timezone"`
	SLA                string                  `json:"sla,omitempty"`
	Limits             map[string]int          `json:"limits"`
}
type InvoiceOrderRef struct {
	SourceType string `json:"source_type"`
	OrderID    string `json:"order_id"`
}
type InvoiceCandidate struct {
	InvoiceOrderRef
	OrderNo                string   `json:"order_no"`
	PaidAt                 string   `json:"paid_at"`
	PaymentLabel           string   `json:"payment_label"`
	AmountMinor            *string  `json:"amount_minor"`
	Currency               string   `json:"currency"`
	CurrencyExponent       int      `json:"currency_exponent"`
	Eligible               bool     `json:"eligible"`
	ReasonCodes            []string `json:"reason_codes"`
	MergeGroupID           string   `json:"merge_group_id"`
	AllowedInvoiceTypes    []string `json:"allowed_invoice_types"`
	AllowedActions         []string `json:"allowed_actions"`
	ClaimedRequestPublicID string   `json:"claimed_request_public_id,omitempty"`
	canonicalKey           string
	sourceID               int64
	paidAt                 int64
	amount                 int64
}
type InvoiceFilter struct {
	P           int    `form:"p"`
	PageSize    int    `form:"page_size"`
	Status      string `form:"status"`
	Group       string `form:"group"`
	Queue       string `form:"queue"`
	Q           string `form:"q"`
	SourceType  string `form:"source_type"`
	Currency    string `form:"currency"`
	From        string `form:"from"`
	To          string `form:"to"`
	Eligibility string `form:"eligibility"`
	OrderID     string `form:"order_id"`
	UnreadOnly  bool   `form:"unread_only"`
}
type InvoicePage[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	P        int   `json:"p"`
	PageSize int   `json:"page_size"`
}
type InvoiceQuoteInput struct {
	Items         []InvoiceOrderRef `json:"items"`
	InvoiceType   string            `json:"invoice_type"`
	PolicyVersion string            `json:"policy_version"`
}
type InvoiceQuote struct {
	Items         []InvoiceCandidate `json:"items"`
	AmountMinor   string             `json:"amount_minor"`
	Currency      string             `json:"currency"`
	MergeGroupID  string             `json:"merge_group_id"`
	Eligible      bool               `json:"eligible"`
	PolicyVersion string             `json:"policy_version"`
}
type InvoiceCreateInput struct {
	InvoiceQuoteInput
	ProfileID     string              `json:"profile_id"`
	Profile       *InvoiceProfileData `json:"profile"`
	Note          string              `json:"note"`
	UserConfirmed bool                `json:"user_confirmed"`
}
type InvoiceProfileInput struct {
	InvoiceProfileData
	Version int64 `json:"version"`
}
type InvoiceActionInput struct {
	Version              int64  `json:"version"`
	DeliveryVersion      int64  `json:"delivery_version"`
	Reason               string `json:"reason"`
	Message              string `json:"message"`
	Category             string `json:"category"`
	Description          string `json:"description"`
	EvidenceAttachmentID string `json:"evidence_attachment_id"`
	AttachmentID         string `json:"attachment_id"`
	InvoiceNo            string `json:"invoice_no"`
	IssuedOn             string `json:"issued_on"`
	AmountMinor          string `json:"amount_minor"`
	Currency             string `json:"currency"`
	TaxMinor             string `json:"tax_minor"`
	NotIssuedConfirmed   bool   `json:"not_issued_confirmed"`
	DisputeID            string `json:"-"`
}
type InvoiceAttachmentDTO struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	MIMEType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	ExpiresAt string `json:"expires_at,omitempty"`
}
type InvoiceDeliveryDTO struct {
	DeliveryVersion int64                `json:"delivery_version"`
	File            InvoiceAttachmentDTO `json:"file"`
	InvoiceNo       string               `json:"invoice_no"`
	IssuedOn        string               `json:"issued_on"`
	AmountMinor     string               `json:"amount_minor"`
	Currency        string               `json:"currency"`
	TaxMinor        string               `json:"tax_minor"`
	CreatedAt       string               `json:"created_at"`
}
type InvoiceDisputeDTO struct {
	ID              string                `json:"id"`
	DeliveryVersion int64                 `json:"delivery_version"`
	Round           int64                 `json:"round"`
	Category        string                `json:"category"`
	Description     string                `json:"description"`
	Reply           string                `json:"reply"`
	Status          string                `json:"status"`
	Evidence        *InvoiceAttachmentDTO `json:"evidence,omitempty"`
	CreatedAt       string                `json:"created_at"`
}
type InvoiceEventDTO struct {
	ID              string                `json:"id"`
	Kind            string                `json:"kind"`
	Status          string                `json:"status"`
	ActorAdmin      bool                  `json:"actor_admin"`
	Description     string                `json:"description,omitempty"`
	Attachment      *InvoiceAttachmentDTO `json:"attachment,omitempty"`
	DeliveryVersion int64                 `json:"delivery_version"`
	CreatedAt       string                `json:"created_at"`
}
type InvoiceDTO struct {
	PublicID                  string               `json:"public_id"`
	DisplayNo                 string               `json:"display_no"`
	UserID                    int                  `json:"user_id"`
	Username                  string               `json:"username,omitempty"`
	Status                    string               `json:"status"`
	Version                   int64                `json:"version"`
	PolicyVersion             string               `json:"policy_version"`
	SellerID                  string               `json:"seller_id"`
	SellerName                string               `json:"seller_name"`
	InvoiceType               string               `json:"invoice_type"`
	Profile                   InvoiceProfileData   `json:"profile"`
	Note                      string               `json:"note,omitempty"`
	Reason                    string               `json:"reason,omitempty"`
	AmountMinor               string               `json:"amount_minor"`
	Currency                  string               `json:"currency"`
	CurrencyExponent          int                  `json:"currency_exponent"`
	ItemsCount                int                  `json:"items_count"`
	SourceSummary             []string             `json:"source_summary"`
	Items                     []InvoiceCandidate   `json:"items,omitempty"`
	CurrentDeliveryVersion    int64                `json:"current_delivery_version"`
	CurrentDelivery           *InvoiceDeliveryDTO  `json:"current_delivery"`
	DeliveredVersions         []InvoiceDeliveryDTO `json:"delivered_versions,omitempty"`
	ActiveDispute             *InvoiceDisputeDTO   `json:"active_dispute"`
	Events                    []InvoiceEventDTO    `json:"events,omitempty"`
	AllowedActions            []string             `json:"allowed_actions"`
	HoldFromStatus            string               `json:"hold_from_status,omitempty"`
	CreatedAt                 string               `json:"created_at"`
	UpdatedAt                 string               `json:"updated_at"`
	NotificationDeliveryState string               `json:"notification_delivery_state"`
}
type InvoiceMoneyTotal struct {
	Currency         string `json:"currency"`
	CurrencyExponent int    `json:"currency_exponent"`
	AmountMinor      string `json:"amount_minor"`
	Count            int64  `json:"count"`
}
type InvoiceSummary struct {
	PendingReview    int64 `json:"pending_review"`
	ApprovedForIssue int64 `json:"approved_for_issue"`
	Disputed         int64 `json:"disputed"`
	FinanceHold      int64 `json:"finance_hold"`
	All              int64 `json:"all"`

	EligibleCount             int64               `json:"eligible_count"`
	Totals                    []InvoiceMoneyTotal `json:"totals"`
	ProcessingCount           int64               `json:"processing_count"`
	AwaitingConfirmationCount int64               `json:"awaiting_confirmation_count"`
	UnreadCount               int64               `json:"unread_count"`
	Counts                    map[string]int64    `json:"-"`
}
type InvoiceNotification struct {
	InvoiceEventDTO
	RequestPublicID string `json:"request_public_id"`
	ReadAt          string `json:"read_at,omitempty"`
}
type InvoiceNotificationPage struct {
	InvoicePage[InvoiceNotification]
	UnreadCount int64 `json:"unread_count"`
}

func invoiceTime(timestamp int64) string {
	if timestamp <= 0 {
		return ""
	}
	return time.Unix(timestamp, 0).UTC().Format(time.RFC3339)
}
func (s InvoiceService) database(ctx context.Context) *gorm.DB {
	db := s.DB
	if db == nil {
		db = model.DB
	}
	return db.WithContext(ctx)
}
