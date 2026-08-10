package service

import "errors"

const (
	RiskSourceEdgeGuard      = "edge_guard"
	RiskSourceUpstreamPolicy = "upstream_policy"
	RiskSourceManualReview   = "manual_review"

	RiskDecisionObserve = "observe"
	RiskDecisionReject  = "reject"
	RiskDecisionDisable = "disable"

	KKAIOutboxTopicRiskActionCommitted = "kkai.risk.action.committed"
)

var (
	ErrRiskActionInvalidInput        = errors.New("invalid risk action input")
	ErrRiskActionIdempotencyConflict = errors.New("risk action idempotency conflict")
	ErrRiskActionTokenNotFound       = errors.New("risk action token not found")
	ErrRiskActionUserNotFound        = errors.New("risk action user not found")
	ErrRiskActionChannelNotFound     = errors.New("risk action channel not found")
	ErrRiskActionIdentityMismatch    = errors.New("risk action identity fingerprint mismatch")
)

type RiskDurableActions struct {
	DisableToken   bool `json:"disable_token"`
	DisableUser    bool `json:"disable_user"`
	DisableChannel bool `json:"disable_channel"`
}

type RiskActionInput struct {
	EventID                string
	Source                 string
	OccurredAt             int64
	RequestID              string
	UserID                 int
	TokenID                int
	ChannelID              int
	ModelName              string
	RuleVersion            string
	EvidenceSHA256         string
	TokenFingerprint       string
	UpstreamKeyFingerprint string
	Decision               string
	Metadata               map[string]any
	Actions                RiskDurableActions
}

type RiskActionResult struct {
	IncidentID         int64
	Replayed           bool
	TokenDisabled      bool
	UserDisabled       bool
	UserDisableSkipped bool
	ChannelDisabled    bool
}

type normalizedRiskAction struct {
	RiskActionInput
	MetadataJSON string
	InputSHA256  string
}

type riskActionOutboxPayload struct {
	IncidentID                     int64  `json:"incident_id"`
	EventID                        string `json:"event_id"`
	RequestID                      string `json:"request_id"`
	UserID                         int    `json:"user_id"`
	TokenID                        int    `json:"token_id"`
	ChannelID                      int    `json:"channel_id"`
	TokenDisabled                  bool   `json:"token_disabled"`
	UserDisabled                   bool   `json:"user_disabled"`
	UserDisableSkipped             bool   `json:"user_disable_skipped"`
	ChannelDisabled                bool   `json:"channel_disabled"`
	UserCacheInvalidationRequired  bool   `json:"user_cache_invalidation_required,omitempty"`
	TokenCacheInvalidationRequired bool   `json:"token_cache_invalidation_required,omitempty"`
}
