package model

const (
	ImageAssetKindReference      = "reference"
	ImageAssetStatePendingUpload = "pending_upload"
	ImageAssetStateValidating    = "validating"
	ImageAssetStateDeleting      = "deleting"

	ImageGenerationExecutionQueued      = "queued"
	ImageGenerationExecutionPreparing   = "preparing"
	ImageGenerationExecutionDispatching = "dispatching"
	ImageGenerationExecutionResultReady = "result_ready"
	ImageGenerationExecutionUnknown     = "unknown"
	ImageGenerationExecutionDone        = "done"
)

// KKAIImageReferenceUpload keeps the asynchronous upload extension separate
// from the v7 asset shape so disabled runtimes remain usable before its expand.
type KKAIImageReferenceUpload struct {
	AssetID         int64  `json:"asset_id" gorm:"primaryKey;autoIncrement:false"`
	OwnerUserID     int    `json:"-" gorm:"not null;uniqueIndex:ux_image_reference_upload_key"`
	UploadKey       string `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:ux_image_reference_upload_key"`
	UploadID        string `json:"-" gorm:"type:varchar(512);not null"`
	UploadPartSize  int64  `json:"-" gorm:"type:bigint;not null"`
	UploadExpiresAt int64  `json:"upload_expires_at" gorm:"type:bigint;not null;index"`
	UploadManifest  string `json:"-" gorm:"type:text;not null"`
	CleanupAt       int64  `json:"-" gorm:"type:bigint;not null;index"`
	CreatedAt       int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (KKAIImageReferenceUpload) TableName() string { return "kkai_image_reference_uploads" }

// KKAIImageExecution persists only asynchronous execution state. The existing
// generation remains the source of truth for ownership, deliverability and money.
type KKAIImageExecution struct {
	GenerationID        int64  `json:"generation_id" gorm:"primaryKey;autoIncrement:false"`
	State               string `json:"state" gorm:"type:varchar(24);not null;index"`
	Fence               string `json:"-" gorm:"type:varchar(128);not null"`
	EventID             int64  `json:"-" gorm:"type:bigint;not null"`
	Payload             string `json:"-" gorm:"type:text;not null"`
	DispatchChannelID   int    `json:"-" gorm:"not null;default:0"`
	DispatchStartedAt   int64  `json:"-" gorm:"type:bigint;not null;default:0"`
	UpstreamRequestID   string `json:"-" gorm:"type:varchar(191);not null;default:''"`
	QueueDeadlineAt     int64  `json:"-" gorm:"type:bigint;not null"`
	ExecutionDeadlineAt int64  `json:"-" gorm:"type:bigint;not null;default:0"`
	RecoveryDeadlineAt  int64  `json:"-" gorm:"type:bigint;not null;default:0"`
	CreatedAt           int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt           int64  `json:"updated_at" gorm:"type:bigint;not null;index"`
}

func (KKAIImageExecution) TableName() string { return "kkai_image_executions" }

type KKAIImageGenerationReference struct {
	GenerationID int64 `json:"generation_id" gorm:"primaryKey;autoIncrement:false"`
	Position     int   `json:"position" gorm:"primaryKey;autoIncrement:false"`
	AssetID      int64 `json:"asset_id" gorm:"not null;index"`
	CreatedAt    int64 `json:"created_at" gorm:"type:bigint;not null"`
}

func (KKAIImageGenerationReference) TableName() string { return "kkai_image_generation_references" }
