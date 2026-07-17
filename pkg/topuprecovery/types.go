package topuprecovery

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const (
	SchemaVersion = 1
	ToolVersion   = "epay-success-time-v1"
)

type Manifest struct {
	SchemaVersion  int             `json:"schema_version"`
	ToolVersion    string          `json:"tool_version"`
	SourceRevision string          `json:"source_revision"`
	ActiveFromID   int64           `json:"active_from_topup_id"`
	CutoffID       int64           `json:"cutoff_topup_id"`
	GeneratedAt    int64           `json:"generated_at"`
	Orders         []OrderEvidence `json:"orders"`
	SHA256         string          `json:"sha256"`
}

type OrderEvidence struct {
	TopUpID                int64  `json:"topup_id"`
	UserID                 int64  `json:"user_id"`
	TradeNoSHA256          string `json:"trade_no_sha256"`
	SourceRowSHA256        string `json:"source_row_sha256"`
	ProviderResponseSHA256 string `json:"provider_response_sha256"`
	CompletedAt            int64  `json:"completed_at"`
}

type Result struct {
	Mode            string `json:"mode"`
	ManifestSHA256  string `json:"manifest_sha256"`
	OrderCount      int    `json:"order_count"`
	UpdatedCount    int    `json:"updated_count"`
	AlreadySetCount int    `json:"already_set_count"`
	VerifiedCount   int    `json:"verified_count"`
}

type ProviderOrder struct {
	Code           int
	Status         int
	TradeNo        string
	ServiceTradeNo string
	PaymentType    string
	EndTime        string
	CompletedAt    int64
}

type Provider interface {
	Lookup(context.Context, string) (ProviderOrder, error)
}

type Service struct {
	db             *gorm.DB
	provider       Provider
	sourceRevision string
	now            func() time.Time
}

func New(db *gorm.DB, provider Provider, sourceRevision string) *Service {
	return &Service{
		db:             db,
		provider:       provider,
		sourceRevision: sourceRevision,
		now:            time.Now,
	}
}

type topUpSource struct {
	ID              int64
	UserID          int64
	TradeNo         string
	PaymentProvider string
	CreateTime      int64
	CompleteTime    int64
	Status          string
}

type sourceIdentity struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	TradeNo         string `json:"trade_no"`
	PaymentProvider string `json:"payment_provider"`
	CreateTime      int64  `json:"create_time"`
	Status          string `json:"status"`
}

type providerIdentity struct {
	Code           int    `json:"code"`
	Status         int    `json:"status"`
	TradeNo        string `json:"trade_no"`
	ServiceTradeNo string `json:"out_trade_no"`
	PaymentType    string `json:"type"`
	EndTime        string `json:"endtime"`
	CompletedAt    int64  `json:"completed_at"`
}
