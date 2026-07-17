package model

const (
	KKAIOutboxStatusPending   = "pending"
	KKAIOutboxStatusDelivered = "delivered"
	KKAIOutboxStatusDead      = "dead"
)

// KKAIPolicyIncident is the durable, idempotent record for every KKAI risk
// decision. It intentionally contains identifiers and digests, never raw keys
// or request content.
type KKAIPolicyIncident struct {
	ID                     int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	EventID                string `json:"event_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	InputSHA256            string `json:"-" gorm:"type:char(64);not null"`
	Source                 string `json:"source" gorm:"type:varchar(32);not null;index"`
	OccurredAt             int64  `json:"occurred_at" gorm:"type:bigint;not null;index"`
	RequestID              string `json:"request_id" gorm:"type:varchar(64);not null;default:'';index"`
	UserID                 int    `json:"user_id" gorm:"not null;default:0;index"`
	TokenID                int    `json:"token_id" gorm:"not null;default:0;index"`
	ChannelID              int    `json:"channel_id" gorm:"not null;default:0;index"`
	ModelName              string `json:"model_name" gorm:"type:varchar(128);not null;default:'';index"`
	RuleVersion            string `json:"rule_version" gorm:"type:varchar(64);not null;default:''"`
	EvidenceSHA256         string `json:"evidence_sha256" gorm:"type:char(64);not null"`
	TokenFingerprint       string `json:"token_fingerprint" gorm:"type:varchar(80);not null;default:'';index"`
	UpstreamKeyFingerprint string `json:"upstream_key_fingerprint" gorm:"type:varchar(80);not null;default:'';index"`
	Decision               string `json:"decision" gorm:"type:varchar(32);not null"`
	Metadata               string `json:"metadata" gorm:"type:text;not null"`
	ActionTaken            string `json:"action_taken" gorm:"type:varchar(255);not null;default:''"`
	ActionResult           string `json:"action_result" gorm:"type:varchar(255);not null;default:''"`
	TokenDisabled          bool   `json:"token_disabled" gorm:"not null;default:false"`
	UserDisabled           bool   `json:"user_disabled" gorm:"not null;default:false"`
	UserDisableSkipped     bool   `json:"user_disable_skipped" gorm:"not null;default:false"`
	ChannelDisabled        bool   `json:"channel_disabled" gorm:"not null;default:false"`
	CreatedAt              int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt              int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (KKAIPolicyIncident) TableName() string {
	return "kkai_policy_incidents"
}

// KKAIOutboxEvent stores post-commit effects such as cache invalidation and
// operator notification. Workers retry these rows independently of the
// business transaction that produced them.
type KKAIOutboxEvent struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	EventKey    string `json:"event_key" gorm:"type:varchar(191);not null;uniqueIndex"`
	Topic       string `json:"topic" gorm:"type:varchar(128);not null;index"`
	AggregateID string `json:"aggregate_id" gorm:"type:varchar(128);not null;default:'';index"`
	Payload     string `json:"payload" gorm:"type:text;not null"`
	Status      string `json:"status" gorm:"type:varchar(16);not null;index"`
	Attempts    int    `json:"attempts" gorm:"not null;default:0"`
	AvailableAt int64  `json:"available_at" gorm:"type:bigint;not null;index"`
	LockedAt    int64  `json:"locked_at" gorm:"type:bigint;not null;default:0;index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);not null;default:''"`
	LastError   string `json:"last_error" gorm:"type:text;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	DeliveredAt int64  `json:"delivered_at" gorm:"type:bigint;not null;default:0"`
}

func (KKAIOutboxEvent) TableName() string {
	return "kkai_outbox"
}
