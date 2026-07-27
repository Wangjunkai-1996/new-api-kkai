package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrVideoAssetNotFound        = errors.New("video asset not found")
	ErrVideoAssetAccessDenied    = errors.New("video asset access denied")
	ErrInvalidVideoAssetUpload   = errors.New("invalid video asset upload")
	ErrVideoAssetUploadExpired   = errors.New("video asset upload expired")
	ErrVideoAssetUploadCompleted = errors.New("video asset upload is already complete")
	ErrVideoMultipartUnavailable = errors.New("video multipart storage is unavailable")
	videoAssetFilenamePattern    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

const (
	VideoUploadModeSingle    = model.VideoUploadModeSingle
	VideoUploadModeMultipart = model.VideoUploadModeMultipart
)

type VideoAssetUploadRequest struct {
	Purpose   string `json:"purpose"`
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Multipart bool   `json:"multipart,omitempty"`
}

type VideoAssetUpload struct {
	Asset        VideoAssetView                    `json:"asset"`
	UploadMode   string                            `json:"upload_mode"`
	PartSize     int64                             `json:"part_size,omitempty"`
	ExpiresAt    int64                             `json:"expires_at"`
	MaxSizeBytes int64                             `json:"max_size_bytes"`
	UploadLimits video_studio_setting.UploadLimits `json:"upload_limits"`
	Request      *VideoAssetSignedRequest          `json:"request,omitempty"`
}

type VideoAssetView struct {
	ID               int64   `json:"id"`
	Scope            string  `json:"scope"`
	Kind             string  `json:"kind"`
	State            string  `json:"state"`
	OriginalFilename string  `json:"original_filename"`
	MIMEType         string  `json:"mime_type"`
	SizeBytes        int64   `json:"size_bytes"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	DurationSeconds  float64 `json:"duration_seconds"`
	Codec            string  `json:"codec"`
	FailureReason    string  `json:"failure_reason,omitempty"`
	UploadMode       string  `json:"upload_mode,omitempty"`
	UploadPartSize   int64   `json:"upload_part_size,omitempty"`
	UploadExpiresAt  int64   `json:"upload_expires_at,omitempty"`
	ContentURL       string  `json:"content_url,omitempty"`
	PosterURL        string  `json:"poster_url,omitempty"`
	PreviewURL       string  `json:"preview_url,omitempty"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
}

func CreateVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	request VideoAssetUploadRequest,
) (*VideoAssetUpload, error) {
	settings := video_studio_setting.Get()
	uploadLimits := settings.UploadLimits()
	request.Purpose = strings.TrimSpace(request.Purpose)
	request.Filename = sanitizeVideoAssetFilename(request.Filename)
	request.MIMEType = strings.ToLower(strings.TrimSpace(strings.Split(request.MIMEType, ";")[0]))
	if db == nil || store == nil || userID <= 0 || request.Filename == "" || request.SizeBytes <= 0 {
		return nil, ErrInvalidVideoAssetUpload
	}
	scope := model.VideoAssetScopeUser
	kind := model.VideoAssetKindReference
	maxBytes := uploadLimits.ReferenceMaxBytes
	switch request.Purpose {
	case model.VideoAssetKindReference:
		if !isSupportedReferenceMIME(request.MIMEType) {
			return nil, ErrInvalidVideoAssetUpload
		}
	case model.VideoAssetKindSample:
		if !isAdmin || !isSupportedVideoMIME(request.MIMEType) {
			return nil, ErrInvalidVideoAssetUpload
		}
		scope = model.VideoAssetScopeCatalog
		kind = model.VideoAssetKindSample
		maxBytes = uploadLimits.SampleMaxBytes
	default:
		return nil, ErrInvalidVideoAssetUpload
	}
	if request.SizeBytes > maxBytes {
		return nil, ErrInvalidVideoAssetUpload
	}
	extension := strings.ToLower(filepath.Ext(request.Filename))
	if extension == "" {
		if extensions, _ := mime.ExtensionsByType(request.MIMEType); len(extensions) > 0 {
			extension = extensions[0]
		}
	}
	uploadMode := VideoUploadModeSingle
	uploadTTL := 15 * time.Minute
	partSize := int64(0)
	if request.Multipart {
		if _, ok := store.(VideoMultipartAssetStore); !ok {
			return nil, ErrVideoMultipartUnavailable
		}
		uploadMode = VideoUploadModeMultipart
		uploadTTL = videoMultipartUploadTTL
		partSize = videoMultipartPartSize
		if videoMultipartPartCount(request.SizeBytes, partSize) > videoMultipartMaximumParts {
			return nil, ErrInvalidVideoAssetUpload
		}
	}
	objectKey := fmt.Sprintf("users/%d/uploads/%s/source%s", userID, uuid.NewString(), extension)
	now := time.Now()
	asset := model.KKAIVideoAsset{
		OwnerUserID: userID, Scope: scope, Kind: kind, State: model.VideoAssetStatePendingUpload,
		ObjectKey: objectKey, OriginalFilename: request.Filename, MIMEType: request.MIMEType,
		SizeBytes: request.SizeBytes, UploadMode: uploadMode, UploadPartSize: partSize,
		UploadExpiresAt: now.Add(uploadTTL).Unix(),
		CreatedAt:       now.Unix(), UpdatedAt: now.Unix(),
	}
	if err := db.WithContext(ctx).Create(&asset).Error; err != nil {
		return nil, fmt.Errorf("create video asset upload reservation: %w", err)
	}
	if request.Multipart {
		upload, err := createMultipartVideoAssetUpload(ctx, db, store.(VideoMultipartAssetStore), asset)
		if err != nil {
			return nil, err
		}
		upload.MaxSizeBytes = maxBytes
		upload.UploadLimits = uploadLimits
		return upload, nil
	}
	signed, err := store.PresignUpload(ctx, objectKey, request.MIMEType, request.SizeBytes, uploadTTL)
	if err != nil {
		discardVideoAssetUploadReservation(ctx, db, asset.ID)
		return nil, fmt.Errorf("presign video asset upload: %w", err)
	}
	view := videoAssetView(asset)
	return &VideoAssetUpload{
		Asset: view, UploadMode: uploadMode, ExpiresAt: asset.UploadExpiresAt,
		MaxSizeBytes: maxBytes, UploadLimits: uploadLimits, Request: &signed,
	}, nil
}

func CompleteVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
	request VideoAssetCompleteRequest,
) (*VideoAssetView, error) {
	return completeVideoAssetUpload(ctx, db, store, userID, isAdmin, assetID, request)
}

func GetAuthorizedVideoAsset(ctx context.Context, db *gorm.DB, userID int, isAdmin bool, assetID int64) (*model.KKAIVideoAsset, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrVideoAssetNotFound
	}
	var asset model.KKAIVideoAsset
	if err := db.WithContext(ctx).First(&asset, "id = ? AND deleted_at = 0", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoAssetNotFound
		}
		return nil, err
	}
	if asset.Scope != model.VideoAssetScopeCatalog && asset.OwnerUserID != userID {
		return nil, ErrVideoAssetAccessDenied
	}
	if asset.Scope == model.VideoAssetScopeCatalog && !isAdmin {
		published, err := isPublishedVideoCatalogAsset(ctx, db, asset.ID)
		if err != nil {
			return nil, err
		}
		if !published {
			return nil, ErrVideoAssetAccessDenied
		}
	}
	return &asset, nil
}

func GetVideoAsset(ctx context.Context, db *gorm.DB, userID int, isAdmin bool, assetID int64) (*VideoAssetView, error) {
	asset, err := GetAuthorizedVideoAsset(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return nil, err
	}
	view := videoAssetView(*asset)
	return &view, nil
}

func GetVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
) (*VideoAssetView, error) {
	if db == nil || store == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrVideoAssetNotFound
	}
	var asset model.KKAIVideoAsset
	if err := db.WithContext(ctx).First(&asset, "id = ? AND deleted_at = 0", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoAssetNotFound
		}
		return nil, err
	}
	if asset.OwnerUserID != userID || (asset.Scope == model.VideoAssetScopeCatalog && !isAdmin) {
		return nil, ErrVideoAssetAccessDenied
	}
	if recovered, completed, err := recoverCompletedVideoAssetUpload(ctx, db, store, asset); err != nil {
		return nil, err
	} else if completed {
		return recovered, nil
	}
	view := videoAssetView(asset)
	return &view, nil
}

func SignAuthorizedVideoAsset(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
	variant string,
	attachment bool,
) (string, error) {
	asset, err := GetAuthorizedVideoAsset(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return "", err
	}
	if asset.State != model.VideoAssetStateReady {
		return "", ErrVideoAssetNotFound
	}
	key := asset.ObjectKey
	filename := asset.OriginalFilename
	switch variant {
	case "":
	case "poster":
		key = asset.PosterObjectKey
		filename = "poster.jpg"
	case "preview":
		key = asset.PreviewObjectKey
		filename = "preview.mp4"
	default:
		return "", ErrVideoAssetNotFound
	}
	if key == "" {
		return "", ErrVideoAssetNotFound
	}
	settings := video_studio_setting.Get()
	return store.PresignDownload(ctx, key, filename, attachment, time.Duration(settings.SignedURLSeconds)*time.Second)
}

func videoAssetView(asset model.KKAIVideoAsset) VideoAssetView {
	view := VideoAssetView{
		ID: asset.ID, Scope: asset.Scope, Kind: asset.Kind, State: asset.State,
		OriginalFilename: asset.OriginalFilename, MIMEType: asset.MIMEType, SizeBytes: asset.SizeBytes,
		Width: asset.Width, Height: asset.Height, DurationSeconds: asset.DurationSeconds,
		Codec: asset.Codec, FailureReason: asset.FailureReason, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt,
	}
	if asset.State == model.VideoAssetStatePendingUpload {
		view.UploadMode = asset.UploadMode
		view.UploadPartSize = asset.UploadPartSize
		view.UploadExpiresAt = asset.UploadExpiresAt
	}
	if asset.State == model.VideoAssetStateReady {
		view.ContentURL = videoAssetContentPath(asset.ID, "")
		if asset.PosterObjectKey != "" {
			view.PosterURL = videoAssetContentPath(asset.ID, "poster")
		}
		if asset.PreviewObjectKey != "" {
			view.PreviewURL = videoAssetContentPath(asset.ID, "preview")
		}
	}
	return view
}

func isPublishedVideoCatalogAsset(ctx context.Context, db *gorm.DB, assetID int64) (bool, error) {
	needle := "%" + strconv.FormatInt(assetID, 10) + "%"
	var samples []model.KKAIVideoSample
	err := db.WithContext(ctx).Model(&model.KKAIVideoSample{}).
		Joins("JOIN kkai_video_model_profiles ON kkai_video_model_profiles.id = kkai_video_samples.model_profile_id").
		Where("kkai_video_samples.status = ? AND kkai_video_model_profiles.enabled = ?", model.VideoSampleStatusPublished, true).
		Where("kkai_video_samples.video_asset_id = ? OR kkai_video_samples.reference_asset_ids LIKE ?", assetID, needle).
		Select("kkai_video_samples.id, kkai_video_samples.video_asset_id, kkai_video_samples.reference_asset_ids").
		Find(&samples).Error
	if err != nil {
		return false, fmt.Errorf("check published video catalog asset: %w", err)
	}
	for _, sample := range samples {
		if sample.VideoAssetID == assetID {
			return true, nil
		}
		referenceIDs, err := decodeVideoSampleReferenceAssetIDs(sample.ReferenceAssetIDs)
		if err != nil {
			return false, fmt.Errorf("decode published video sample references: %w", err)
		}
		for _, referenceID := range referenceIDs {
			if referenceID == assetID {
				return true, nil
			}
		}
	}
	return false, nil
}

func sanitizeVideoAssetFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = videoAssetFilenamePattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if len(value) > 191 {
		extension := filepath.Ext(value)
		base := strings.TrimSuffix(value, extension)
		maxBase := 191 - len(extension)
		if maxBase < 1 {
			return ""
		}
		value = base[:maxBase] + extension
	}
	return value
}

func isSupportedReferenceMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func isSupportedVideoMIME(value string) bool {
	switch value {
	case "video/mp4", "video/webm", "video/quicktime":
		return true
	default:
		return false
	}
}
