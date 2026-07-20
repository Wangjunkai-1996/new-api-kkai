package topuprecovery

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	SchemaVersion = 2
	ToolVersion   = "epay-rebate-outbox-v2"
)

type Manifest struct {
	SchemaVersion  int             `json:"schema_version"`
	ToolVersion    string          `json:"tool_version"`
	SourceRevision string          `json:"source_revision"`
	ActiveFromID   int64           `json:"active_from_topup_id"`
	CutoffID       int64           `json:"cutoff_topup_id"`
	QuotaPerUnit   string          `json:"quota_per_unit"`
	GeneratedAt    int64           `json:"generated_at"`
	Orders         []OrderEvidence `json:"orders"`
	SHA256         string          `json:"sha256"`
}

type OrderEvidence struct {
	TopUpID                int64  `json:"topup_id"`
	UserID                 int64  `json:"user_id"`
	InviterID              int64  `json:"inviter_id"`
	InviterGroup           string `json:"inviter_group"`
	CreditedQuota          int64  `json:"credited_quota"`
	EventKey               string `json:"event_key"`
	EventPayloadSHA256     string `json:"event_payload_sha256"`
	TradeNoSHA256          string `json:"trade_no_sha256"`
	SourceRowSHA256        string `json:"source_row_sha256"`
	ProviderResponseSHA256 string `json:"provider_response_sha256"`
	CompletedAt            int64  `json:"completed_at"`
}

type Result struct {
	Mode                      string `json:"mode"`
	ManifestSHA256            string `json:"manifest_sha256"`
	OrderCount                int    `json:"order_count"`
	UpdatedCount              int    `json:"updated_count"`
	AlreadySetCount           int    `json:"already_set_count"`
	OutboxCreatedCount        int    `json:"outbox_created_count"`
	OutboxAlreadyPresentCount int    `json:"outbox_already_present_count"`
	VerifiedCount             int    `json:"verified_count"`
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
	quotaPerUnit   float64
	now            func() time.Time
}

func New(db *gorm.DB, provider Provider, sourceRevision string, quotaPerUnit float64) *Service {
	return &Service{
		db:             db,
		provider:       provider,
		sourceRevision: sourceRevision,
		quotaPerUnit:   quotaPerUnit,
		now:            time.Now,
	}
}

func NewFromDatabase(db *gorm.DB, provider Provider, sourceRevision string) (*Service, error) {
	raw, found, err := loadOptionalOption(db, "QuotaPerUnit")
	if err != nil {
		return nil, err
	}
	if !found {
		raw = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	}
	quotaPerUnit, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) || quotaPerUnit <= 0 {
		return nil, ErrInvalidQuotaConfiguration
	}
	return New(db, provider, sourceRevision, quotaPerUnit), nil
}

func (service *Service) quotaPerUnitString() string {
	return strconv.FormatFloat(service.quotaPerUnit, 'f', -1, 64)
}

type topUpSource struct {
	ID              int64
	UserID          int64
	Amount          int64
	TradeNo         string
	PaymentProvider string
	CreateTime      int64
	CompleteTime    int64
	Status          string
	InviterID       int64
	InviterGroup    string
}

type sourceIdentity struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	Amount          int64  `json:"amount"`
	TradeNo         string `json:"trade_no"`
	PaymentProvider string `json:"payment_provider"`
	CreateTime      int64  `json:"create_time"`
	Status          string `json:"status"`
	InviterID       int64  `json:"inviter_id"`
	InviterGroup    string `json:"inviter_group"`
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
