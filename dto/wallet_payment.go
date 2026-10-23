package dto

// Wallet money and concurrency counters are decimal strings at the API boundary.
type WalletMoney struct {
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
	Exponent    int    `json:"exponent"`
}
type WalletCreditDisplay struct {
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
}
type WalletReview struct {
	ReasonCode    string `json:"reason_code"`
	OwnerLabel    string `json:"owner_label"`
	LastCheckedAt *int64 `json:"last_checked_at"`
}
type WalletLegacyDisplay struct {
	Amount *string `json:"amount"`
	Money  *string `json:"money"`
}
type WalletOrder struct {
	TradeNo           string               `json:"trade_no"`
	Version           string               `json:"version"`
	DisplayStatus     string               `json:"display_status"`
	StatusGroup       string               `json:"status_group"`
	AllowedActions    []string             `json:"allowed_actions"`
	ExpectedPayment   *WalletMoney         `json:"expected_payment"`
	PricingMode       string               `json:"pricing_mode"`
	PayablePayment    *WalletMoney         `json:"payable_payment"`
	PricingNoticeCode *string              `json:"pricing_notice_code"`
	CreditedQuota     *string              `json:"credited_quota"`
	CreditDisplay     *WalletCreditDisplay `json:"credit_display"`
	PaymentProvider   string               `json:"payment_provider"`
	PaymentMethod     string               `json:"payment_method"`
	CreatedAt         int64                `json:"created_at"`
	DeadlineAt        *int64               `json:"deadline_at"`
	DeadlineSource    string               `json:"deadline_source"`
	LocalEndReason    *string              `json:"local_end_reason"`
	LocalEndedAt      *int64               `json:"local_ended_at"`
	CloseReason       *string              `json:"close_reason"`
	ClosedAt          *int64               `json:"closed_at"`
	CompletedAt       *int64               `json:"completed_at"`
	LastCheckedAt     *int64               `json:"last_checked_at"`
	ReasonCode        *string              `json:"reason_code"`
	ReceivedPayment   *WalletMoney         `json:"received_payment"`
	Review            *WalletReview        `json:"review"`
	LegacyDisplay     *WalletLegacyDisplay `json:"legacy_display"`
}
type WalletFlow struct {
	ID       *string      `json:"id"`
	Revision string       `json:"revision"`
	Phase    string       `json:"phase"`
	Order    *WalletOrder `json:"order"`
}
type WalletState struct {
	Flow                  WalletFlow `json:"flow"`
	CanCreate             bool       `json:"can_create"`
	CreateBlockReasonCode *string    `json:"create_block_reason_code"`
	OtherUnfinishedCount  int64      `json:"other_unfinished_count"`
	ServerTime            int64      `json:"server_time"`
}
type WalletCheckout struct {
	Kind   string            `json:"kind"`
	URL    string            `json:"url,omitempty"`
	Method string            `json:"method,omitempty"`
	Action string            `json:"action,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}
type WalletQuoteRequest struct {
	PaymentProvider    string `json:"payment_provider"`
	PaymentMethod      string `json:"payment_method"`
	Amount             string `json:"amount,omitempty"`
	ProductID          string `json:"product_id,omitempty"`
	WaffoPayMethodType string `json:"waffo_pay_method_type,omitempty"`
	WaffoPayMethodName string `json:"waffo_pay_method_name,omitempty"`
}
type WalletQuote struct {
	QuoteToken        string              `json:"quote_token"`
	ExpiresAt         int64               `json:"expires_at"`
	ExpectedPayment   *WalletMoney        `json:"expected_payment"`
	PricingMode       string              `json:"pricing_mode"`
	PayablePayment    *WalletMoney        `json:"payable_payment"`
	PricingNoticeCode *string             `json:"pricing_notice_code"`
	CreditedQuota     string              `json:"credited_quota"`
	CreditDisplay     WalletCreditDisplay `json:"credit_display"`
	PaymentProvider   string              `json:"payment_provider"`
	PaymentMethod     string              `json:"payment_method"`
	ServerTime        int64               `json:"server_time"`
}
type WalletTimelineEvent struct {
	EventType   string `json:"event_type"`
	OccurredAt  int64  `json:"occurred_at"`
	SummaryCode string `json:"summary_code"`
}
