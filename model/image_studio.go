package model

const (
	ImageAssetScopeUser    = "user"
	ImageAssetScopeCatalog = "catalog"

	ImageAssetKindOutput = "output"
	ImageAssetKindSample = "sample"

	ImageAssetStateStaging = "staging"
	ImageAssetStateReady   = "ready"
	ImageAssetStateFailed  = "failed"
	ImageAssetStateDeleted = "deleted"

	ImageThumbnailStatePending = "pending"
	ImageThumbnailStateReady   = "ready"
	ImageThumbnailStateFailed  = "failed"

	ImageSampleStatusDraft     = "draft"
	ImageSampleStatusPublished = "published"

	ImageGenerationStatusSubmitting    = "submitting"
	ImageGenerationStatusRecovering    = "recovering"
	ImageGenerationStatusSucceeded     = "succeeded"
	ImageGenerationStatusPartial       = "partial"
	ImageGenerationStatusFailed        = "failed"
	ImageGenerationStatusArchiveFailed = "archive_failed"
	ImageGenerationStatusUnknown       = "unknown"

	ImageGenerationBillingStatePending    = "pending"
	ImageGenerationBillingStateReserved   = "reserved"
	ImageGenerationBillingStateProcessing = "processing"
	ImageGenerationBillingStateSettled    = "settled"
	ImageGenerationBillingStateRefunded   = "refunded"

	ImageIdempotencyResourceGeneration = "image_generation"
	ImageIdempotencyOperationSubmit    = "image.submit"
)

type KKAIImageModelProfile struct {
	ID                   int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Model                string `json:"model" gorm:"type:varchar(191);not null;uniqueIndex"`
	DisplayName          string `json:"display_name" gorm:"type:varchar(191);not null"`
	Description          string `json:"description" gorm:"type:text;not null"`
	ProviderLabel        string `json:"provider_label" gorm:"type:varchar(128);not null;default:''"`
	SpecificationVersion int    `json:"specification_version" gorm:"not null"`
	Specification        string `json:"-" gorm:"type:text;not null"`
	DefaultParameters    string `json:"-" gorm:"type:text;not null"`
	Enabled              bool   `json:"enabled" gorm:"not null"`
	SortOrder            int    `json:"sort_order" gorm:"not null;default:0;index"`
	CreatedAt            int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (KKAIImageModelProfile) TableName() string { return "kkai_image_model_profiles" }

type KKAIImageSample struct {
	ID             int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ModelProfileID int64  `json:"model_profile_id" gorm:"not null;index"`
	ImageAssetID   int64  `json:"image_asset_id" gorm:"not null;index"`
	Title          string `json:"title" gorm:"type:varchar(191);not null"`
	Prompt         string `json:"prompt" gorm:"type:text;not null"`
	ModelVersion   int    `json:"model_version" gorm:"not null"`
	Parameters     string `json:"-" gorm:"type:text;not null"`
	Category       string `json:"category" gorm:"type:varchar(32);not null"`
	Status         string `json:"status" gorm:"type:varchar(16);not null;index"`
	SortOrder      int    `json:"sort_order" gorm:"not null;default:0;index"`
	CreatedAt      int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (KKAIImageSample) TableName() string { return "kkai_image_samples" }

type KKAIImageGeneration struct {
	ID                   int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID               int    `json:"user_id" gorm:"not null;index"`
	TokenID              int    `json:"-" gorm:"not null"`
	ModelProfileID       int64  `json:"model_profile_id" gorm:"not null;index"`
	SampleID             *int64 `json:"sample_id,omitempty" gorm:"index"`
	SpecificationVersion int    `json:"specification_version" gorm:"not null"`
	Model                string `json:"model" gorm:"type:varchar(191);not null;index"`
	Prompt               string `json:"prompt" gorm:"type:text;not null"`
	Parameters           string `json:"-" gorm:"type:text;not null"`
	RequestHash          string `json:"-" gorm:"type:char(64);not null"`
	RequestID            string `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	Status               string `json:"status" gorm:"type:varchar(24);not null;index"`
	RequestedCount       int    `json:"requested_count" gorm:"not null"`
	SucceededCount       int    `json:"succeeded_count" gorm:"not null;default:0"`
	FinalQuota           int    `json:"final_quota" gorm:"not null;default:0"`
	BillingSource        string `json:"-" gorm:"type:varchar(16);not null;default:''"`
	BillingState         string `json:"-" gorm:"type:varchar(16);not null;default:'pending';index"`
	ReservedQuota        int    `json:"-" gorm:"not null;default:0"`
	SubscriptionID       int    `json:"-" gorm:"not null;default:0"`
	HeartbeatAt          int64  `json:"-" gorm:"type:bigint;not null;default:0;index"`
	FailureStage         string `json:"failure_stage,omitempty" gorm:"type:varchar(32);not null;default:''"`
	ErrorCode            string `json:"error_code,omitempty" gorm:"type:varchar(64);not null;default:''"`
	ErrorMessage         string `json:"error_message,omitempty" gorm:"type:varchar(1024);not null;default:''"`
	StartedAt            int64  `json:"started_at" gorm:"type:bigint;not null"`
	FinishedAt           int64  `json:"finished_at" gorm:"type:bigint;not null;default:0"`
	CreatedAt            int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"type:bigint;not null"`
	DeletedAt            int64  `json:"deleted_at" gorm:"type:bigint;not null;default:0;index"`
}

func (KKAIImageGeneration) TableName() string { return "kkai_image_generations" }

type KKAIImageAsset struct {
	ID                 int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	GenerationID       *int64 `json:"generation_id,omitempty" gorm:"index"`
	OwnerUserID        int    `json:"owner_user_id" gorm:"not null;default:0;index"`
	Scope              string `json:"scope" gorm:"type:varchar(16);not null;index"`
	Kind               string `json:"kind" gorm:"type:varchar(16);not null;index"`
	State              string `json:"state" gorm:"type:varchar(16);not null;index"`
	Position           int    `json:"position" gorm:"not null;default:0"`
	ObjectKey          string `json:"-" gorm:"type:varchar(191);not null;uniqueIndex"`
	ThumbnailObjectKey string `json:"-" gorm:"type:varchar(191);not null;default:''"`
	ThumbnailState     string `json:"thumbnail_state" gorm:"type:varchar(16);not null"`
	OriginalFilename   string `json:"original_filename" gorm:"type:varchar(255);not null;default:''"`
	MIMEType           string `json:"mime_type" gorm:"type:varchar(128);not null"`
	SizeBytes          int64  `json:"size_bytes" gorm:"type:bigint;not null;default:0"`
	Width              int    `json:"width" gorm:"not null;default:0"`
	Height             int    `json:"height" gorm:"not null;default:0"`
	SHA256             string `json:"sha256" gorm:"type:char(64);not null;default:'';index"`
	FailureReason      string `json:"failure_reason,omitempty" gorm:"type:varchar(1024);not null;default:''"`
	CreatedAt          int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt          int64  `json:"updated_at" gorm:"type:bigint;not null"`
	DeletedAt          int64  `json:"deleted_at" gorm:"type:bigint;not null;default:0;index"`
}

func (KKAIImageAsset) TableName() string { return "kkai_image_assets" }
