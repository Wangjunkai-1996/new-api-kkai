package model

const (
	VideoAssetScopeUser    = "user"
	VideoAssetScopeCatalog = "catalog"

	VideoAssetKindReference = "reference"
	VideoAssetKindOutput    = "output"
	VideoAssetKindSample    = "sample"

	VideoAssetStatePendingUpload = "pending_upload"
	VideoAssetStateUploaded      = "uploaded"
	VideoAssetStateProcessing    = "processing"
	VideoAssetStateReady         = "ready"
	VideoAssetStateFailed        = "failed"
	VideoAssetStateDeleting      = "deleting"
	VideoAssetStateDeleted       = "deleted"

	VideoUploadModeSingle    = "single"
	VideoUploadModeMultipart = "multipart"

	VideoSampleStatusDraft     = "draft"
	VideoSampleStatusPublished = "published"

	VideoTaskAssetRoleReference     = "reference"
	VideoTaskAssetRoleFirstFrame    = "first_frame"
	VideoTaskAssetRoleLastFrame     = "last_frame"
	VideoTaskAssetRoleOutput        = "output"
	VideoIdempotencyResourceTask    = "task"
	VideoIdempotencyOperationSubmit = "video.submit"
)

type KKAIVideoModelProfile struct {
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

func (KKAIVideoModelProfile) TableName() string { return "kkai_video_model_profiles" }

type KKAIVideoSample struct {
	ID                int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	ModelProfileID    int64   `json:"model_profile_id" gorm:"not null;index"`
	Title             string  `json:"title" gorm:"type:varchar(191);not null"`
	Prompt            string  `json:"prompt" gorm:"type:text;not null"`
	Mode              string  `json:"mode" gorm:"type:varchar(32);not null;index"`
	ModelVersion      int     `json:"model_version" gorm:"not null"`
	Parameters        string  `json:"-" gorm:"type:text;not null"`
	ReferenceAssetIDs string  `json:"-" gorm:"type:text;not null"`
	VideoAssetID      int64   `json:"video_asset_id" gorm:"not null;index"`
	AspectRatio       float64 `json:"aspect_ratio" gorm:"not null"`
	Status            string  `json:"status" gorm:"type:varchar(16);not null;index"`
	SortOrder         int     `json:"sort_order" gorm:"not null;default:0;index"`
	CreatedAt         int64   `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt         int64   `json:"updated_at" gorm:"type:bigint;not null"`
}

func (KKAIVideoSample) TableName() string { return "kkai_video_samples" }

type KKAIVideoGeneration struct {
	ID             int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID         int    `json:"user_id" gorm:"not null;index"`
	TaskID         int64  `json:"task_id" gorm:"not null;uniqueIndex"`
	ModelProfileID int64  `json:"model_profile_id" gorm:"not null;index"`
	SampleID       *int64 `json:"sample_id,omitempty" gorm:"index"`
	Model          string `json:"model" gorm:"type:varchar(191);not null;index"`
	Mode           string `json:"mode" gorm:"type:varchar(32);not null;index"`
	Prompt         string `json:"prompt" gorm:"type:text;not null"`
	Parameters     string `json:"-" gorm:"type:text;not null"`
	CreatedAt      int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"type:bigint;not null"`
	DeletedAt      int64  `json:"deleted_at" gorm:"type:bigint;not null;default:0;index"`
}

func (KKAIVideoGeneration) TableName() string { return "kkai_video_generations" }

type KKAIVideoAsset struct {
	ID                int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerUserID       int     `json:"owner_user_id" gorm:"not null;default:0;index"`
	Scope             string  `json:"scope" gorm:"type:varchar(16);not null;index"`
	Kind              string  `json:"kind" gorm:"type:varchar(16);not null;index"`
	State             string  `json:"state" gorm:"type:varchar(24);not null;index"`
	ObjectKey         string  `json:"-" gorm:"type:varchar(191);not null;uniqueIndex"`
	PosterObjectKey   string  `json:"-" gorm:"type:varchar(512);not null;default:''"`
	PreviewObjectKey  string  `json:"-" gorm:"type:varchar(512);not null;default:''"`
	ArchiveSourceURL  string  `json:"-" gorm:"type:text;not null"`
	OriginalFilename  string  `json:"original_filename" gorm:"type:varchar(255);not null;default:''"`
	MIMEType          string  `json:"mime_type" gorm:"type:varchar(128);not null"`
	SizeBytes         int64   `json:"size_bytes" gorm:"type:bigint;not null;default:0"`
	Width             int     `json:"width" gorm:"not null;default:0"`
	Height            int     `json:"height" gorm:"not null;default:0"`
	DurationSeconds   float64 `json:"duration_seconds" gorm:"not null;default:0"`
	Codec             string  `json:"codec" gorm:"type:varchar(64);not null;default:''"`
	SHA256            string  `json:"sha256" gorm:"type:char(64);not null;default:'';index"`
	FailureReason     string  `json:"failure_reason,omitempty" gorm:"type:varchar(1024);not null;default:''"`
	UploadMode        string  `json:"upload_mode" gorm:"type:varchar(16);not null;default:''"`
	MultipartUploadID string  `json:"-" gorm:"type:varchar(512);not null;default:''"`
	UploadPartSize    int64   `json:"upload_part_size" gorm:"type:bigint;not null;default:0"`
	UploadExpiresAt   int64   `json:"upload_expires_at" gorm:"type:bigint;not null;default:0;index"`
	CreatedAt         int64   `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt         int64   `json:"updated_at" gorm:"type:bigint;not null"`
	DeletedAt         int64   `json:"deleted_at" gorm:"type:bigint;not null;default:0;index"`
}

func (KKAIVideoAsset) TableName() string { return "kkai_video_assets" }

type KKAIVideoTaskAsset struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID    int64  `json:"task_id" gorm:"not null;uniqueIndex:ux_kkai_video_task_asset_role,priority:1;index"`
	AssetID   int64  `json:"asset_id" gorm:"not null;index"`
	Role      string `json:"role" gorm:"type:varchar(32);not null;uniqueIndex:ux_kkai_video_task_asset_role,priority:2"`
	Position  int    `json:"position" gorm:"not null;default:0;uniqueIndex:ux_kkai_video_task_asset_role,priority:3"`
	CreatedAt int64  `json:"created_at" gorm:"type:bigint;not null;index"`
}

func (KKAIVideoTaskAsset) TableName() string { return "kkai_video_task_assets" }

type KKAIIdempotencyKey struct {
	ID           int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID       int    `json:"user_id" gorm:"not null;uniqueIndex:ux_kkai_idempotency_scope,priority:1"`
	Operation    string `json:"operation" gorm:"type:varchar(64);not null;uniqueIndex:ux_kkai_idempotency_scope,priority:2"`
	Key          string `json:"key" gorm:"type:varchar(128);not null;uniqueIndex:ux_kkai_idempotency_scope,priority:3"`
	RequestHash  string `json:"request_hash" gorm:"type:char(64);not null"`
	ResourceType string `json:"resource_type" gorm:"type:varchar(32);not null;default:''"`
	ResourceID   string `json:"resource_id" gorm:"type:varchar(128);not null;default:''"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint;not null;index"`
	ExpiresAt    int64  `json:"expires_at" gorm:"type:bigint;not null;index"`
}

func (KKAIIdempotencyKey) TableName() string { return "kkai_idempotency_keys" }
