package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ImageUploadValidateTopic         = "image.upload.validate.v1"
	imageUploadPartSize        int64 = 5 << 20
	imageUploadTTL                   = time.Hour
	imageUploadSignatureTTL          = 15 * time.Minute
	imageUploadRetention             = 24 * time.Hour
	imageUploadMaxUserSessions       = 2 * MaxImageStudioReferenceImages
	imageUploadMaxSessions           = 1024
	imageUploadMaxStoredBytes  int64 = 8 << 30
	imageUploadMaxValidating         = 64
)

var (
	ErrInvalidImageUpload         = errors.New("invalid image upload")
	ErrImageUploadExpired         = errors.New("image upload expired")
	ErrImageUploadConflict        = errors.New("image upload state or content conflicts with the original upload")
	ErrImageUploadCapacity        = errors.New("image upload capacity reached")
	ErrImageUploadUnavailable     = errors.New("image multipart upload unavailable")
	ErrImageUploadLengthNotSigned = errors.New("image upload length cannot be enforced by the storage signature")
)

type ImageUploadRequest struct {
	SizeBytes int64  `json:"size_bytes"`
	MIMEType  string `json:"mime_type"`
	SHA256    string `json:"sha256"`
}

type ImageUploadAssetView struct {
	ID              int64  `json:"id"`
	State           string `json:"state"`
	SizeBytes       int64  `json:"size_bytes"`
	MIMEType        string `json:"mime_type"`
	SHA256          string `json:"sha256"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	UploadExpiresAt int64  `json:"upload_expires_at"`
	FailureReason   string `json:"failure_reason,omitempty"`
}

type ImageUploadReservation struct {
	Asset        ImageUploadAssetView `json:"asset"`
	UploadMode   string               `json:"upload_mode"`
	PartSize     int64                `json:"part_size"`
	ExpiresAt    int64                `json:"expires_at"`
	MaxSizeBytes int64                `json:"max_size_bytes"`
}

type imageUploadRecord struct {
	asset  model.KKAIImageAsset
	upload model.KKAIImageReferenceUpload
}

type imageUploadPayload struct {
	AssetID int64 `json:"asset_id"`
}

func (record imageUploadRecord) view() ImageUploadReservation {
	asset := ImageUploadAssetView{
		ID: record.asset.ID, State: record.asset.State, SizeBytes: record.asset.SizeBytes,
		MIMEType: record.asset.MIMEType, SHA256: record.asset.SHA256,
		Width: record.asset.Width, Height: record.asset.Height,
		UploadExpiresAt: record.upload.UploadExpiresAt, FailureReason: record.asset.FailureReason,
	}
	if (asset.State == model.ImageAssetStatePendingUpload || asset.State == model.ImageAssetStateValidating) && record.upload.UploadExpiresAt <= time.Now().Unix() {
		asset.State, asset.FailureReason = model.ImageAssetStateFailed, "upload_expired"
	}
	return ImageUploadReservation{
		Asset: asset, UploadMode: "multipart", PartSize: record.upload.UploadPartSize,
		ExpiresAt: record.upload.UploadExpiresAt, MaxSizeBytes: image_studio_setting.Get().MaxReferenceBytes,
	}
}

func CreateImageReferenceUpload(ctx context.Context, db *gorm.DB, assetStore ImageAssetStore, userID int, key string, request ImageUploadRequest) (*ImageUploadReservation, error) {
	store, ok := assetStore.(ImageUploadStore)
	if !ok {
		return nil, ErrImageUploadUnavailable
	}
	key = strings.TrimSpace(key)
	request.MIMEType = strings.ToLower(strings.TrimSpace(request.MIMEType))
	request.SHA256 = strings.ToLower(strings.TrimSpace(request.SHA256))
	settings := image_studio_setting.Get()
	if db == nil || userID <= 0 || key == "" || len(key) > 128 ||
		request.SizeBytes <= 0 || request.SizeBytes > settings.MaxReferenceBytes ||
		!validSHA256Hex(request.SHA256) || !isSupportedImageMIME(request.MIMEType) {
		return nil, ErrInvalidImageUpload
	}
	var record imageUploadRecord
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		err := tx.Where("owner_user_id = ? AND upload_key = ?", userID, key).First(&record.upload).Error
		if err == nil {
			if err := tx.First(&record.asset, "id = ? AND owner_user_id = ? AND kind = ?", record.upload.AssetID, userID, model.ImageAssetKindReference).Error; err != nil {
				return err
			}
			if record.asset.SizeBytes != request.SizeBytes || record.asset.SHA256 != request.SHA256 || record.asset.MIMEType != request.MIMEType {
				return ErrImageUploadConflict
			}
			if record.asset.State != model.ImageAssetStateReady && (record.asset.State != model.ImageAssetStatePendingUpload || record.upload.UploadExpiresAt <= time.Now().Unix()) {
				if record.asset.State == model.ImageAssetStateValidating {
					return nil
				}
				return ErrImageUploadExpired
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var usage struct {
			Count int64
			Bytes int64
		}
		query := tx.Model(&model.KKAIImageAsset{}).Where("kind = ? AND deleted_at = 0", model.ImageAssetKindReference)
		if err := query.Select("COUNT(*) AS count, COALESCE(SUM(size_bytes), 0) AS bytes").Where("owner_user_id = ?", userID).Scan(&usage).Error; err != nil {
			return err
		}
		if usage.Count >= imageUploadMaxUserSessions || usage.Bytes > 2*settings.MaxReferenceTotalBytes-request.SizeBytes {
			return ErrImageUploadCapacity
		}
		usage.Count, usage.Bytes = 0, 0
		if err := tx.Model(&model.KKAIImageAsset{}).Where("kind = ? AND deleted_at = 0", model.ImageAssetKindReference).Select("COUNT(*) AS count, COALESCE(SUM(size_bytes), 0) AS bytes").Scan(&usage).Error; err != nil {
			return err
		}
		if usage.Count >= imageUploadMaxSessions || usage.Bytes > imageUploadMaxStoredBytes-request.SizeBytes {
			return ErrImageUploadCapacity
		}
		now := time.Now()
		record.asset = model.KKAIImageAsset{
			OwnerUserID: userID, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindReference,
			State: model.ImageAssetStatePendingUpload, ThumbnailState: model.ImageThumbnailStatePending,
			ObjectKey: fmt.Sprintf("image-studio/users/%d/uploads/%s/source", userID, uuid.NewString()),
			SizeBytes: request.SizeBytes, MIMEType: request.MIMEType, SHA256: request.SHA256,
			CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
		}
		if err := tx.Create(&record.asset).Error; err != nil {
			return err
		}
		record.upload = model.KKAIImageReferenceUpload{
			AssetID: record.asset.ID, OwnerUserID: userID, UploadPartSize: imageUploadPartSize,
			UploadKey: key, UploadExpiresAt: now.Add(imageUploadTTL).Unix(), CleanupAt: now.Add(imageUploadTTL).Unix(),
			CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
		}
		return tx.Create(&record.upload).Error
	})
	if err != nil {
		return nil, err
	}
	if record.asset.State == model.ImageAssetStatePendingUpload && record.upload.UploadID == "" {
		// Retrying an interrupted create can race another caller. Only one ID is
		// bound, and loser sessions are aborted; GC also enumerates crash orphans.
		storageCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		uploadID, err := store.CreateMultipartUpload(storageCtx, record.asset.ObjectKey, record.asset.MIMEType)
		if err != nil {
			return nil, err
		}
		bound := false
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := model.LockImageStudioCapacityTx(tx); err != nil {
				return err
			}
			current, err := loadImageUpload(ctx, tx, userID, record.asset.ID)
			if err != nil {
				return err
			}
			if err := requirePendingImageUpload(current); err != nil {
				return err
			}
			if current.upload.UploadID != "" {
				record = current
				return nil
			}
			if err := tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", record.asset.ID).Updates(map[string]any{"upload_id": uploadID, "updated_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
			record.upload.UploadID, bound = uploadID, true
			return nil
		})
		if err != nil || !bound {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = store.AbortMultipartUpload(cleanupCtx, record.asset.ObjectKey, uploadID)
			cleanupCancel()
		}
		if err != nil {
			return nil, err
		}
	}
	view := record.view()
	return &view, nil
}

func loadImageUpload(ctx context.Context, db *gorm.DB, userID int, assetID int64) (imageUploadRecord, error) {
	var record imageUploadRecord
	if db == nil || userID <= 0 || assetID <= 0 {
		return record, ErrInvalidImageUpload
	}
	if err := db.WithContext(ctx).Where("asset_id = ? AND owner_user_id = ?", assetID, userID).First(&record.upload).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return record, ErrImageAssetNotFound
		}
		return record, err
	}
	if err := db.WithContext(ctx).Where("id = ? AND owner_user_id = ? AND kind = ?", assetID, userID, model.ImageAssetKindReference).First(&record.asset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return record, ErrImageAssetNotFound
		}
		return record, err
	}
	return record, nil
}

func requirePendingImageUpload(record imageUploadRecord) error {
	if record.upload.UploadExpiresAt <= time.Now().Unix() {
		return ErrImageUploadExpired
	}
	if record.asset.State != model.ImageAssetStatePendingUpload {
		return ErrImageUploadConflict
	}
	return nil
}

func GetImageReferenceUpload(ctx context.Context, db *gorm.DB, userID int, assetID int64) (*ImageUploadReservation, error) {
	record, err := loadImageUpload(ctx, db, userID, assetID)
	if err != nil {
		return nil, err
	}
	view := record.view()
	return &view, nil
}

func SignImageReferenceUploadPart(ctx context.Context, db *gorm.DB, assetStore ImageAssetStore, userID int, assetID int64, partNumber int32) (*VideoAssetSignedRequest, error) {
	store, ok := assetStore.(ImageUploadStore)
	if !ok {
		return nil, ErrImageUploadUnavailable
	}
	record, err := loadImageUpload(ctx, db, userID, assetID)
	if err != nil {
		return nil, err
	}
	if err := requirePendingImageUpload(record); err != nil {
		return nil, err
	}
	if record.upload.UploadID == "" || partNumber <= 0 || int(partNumber) > videoMultipartPartCount(record.asset.SizeBytes, record.upload.UploadPartSize) {
		return nil, ErrInvalidImageUpload
	}
	length := videoMultipartPartLength(record.asset.SizeBytes, record.upload.UploadPartSize, int(partNumber))
	expires := min(imageUploadSignatureTTL, time.Until(time.Unix(record.upload.UploadExpiresAt, 0)))
	signed, err := store.PresignImageUploadPart(ctx, record.asset.ObjectKey, record.upload.UploadID, partNumber, length, expires)
	return &signed, err
}

func ListImageReferenceUploadParts(ctx context.Context, db *gorm.DB, assetStore ImageAssetStore, userID int, assetID int64) ([]VideoAssetUploadedPart, error) {
	store, ok := assetStore.(ImageUploadStore)
	if !ok {
		return nil, ErrImageUploadUnavailable
	}
	record, err := loadImageUpload(ctx, db, userID, assetID)
	if err != nil {
		return nil, err
	}
	if err := requirePendingImageUpload(record); err != nil {
		return nil, err
	}
	if record.upload.UploadID == "" {
		return nil, ErrImageUploadConflict
	}
	parts, err := store.ListUploadedParts(ctx, record.asset.ObjectKey, record.upload.UploadID)
	if err != nil {
		return nil, err
	}
	if err := validateUploadedVideoParts(model.KKAIVideoAsset{SizeBytes: record.asset.SizeBytes, UploadPartSize: record.upload.UploadPartSize}, parts, false); err != nil {
		return nil, ErrInvalidImageUpload
	}
	return parts, nil
}

func CompleteImageReferenceUpload(ctx context.Context, db *gorm.DB, userID int, assetID int64, parts []VideoAssetCompletedPart) (*ImageUploadAssetView, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrInvalidImageUpload
	}
	var record imageUploadRecord
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		var err error
		record, err = loadImageUpload(ctx, tx, userID, assetID)
		if err != nil {
			return err
		}
		if len(parts) != videoMultipartPartCount(record.asset.SizeBytes, record.upload.UploadPartSize) {
			return ErrInvalidImageUpload
		}
		parts = append([]VideoAssetCompletedPart(nil), parts...)
		sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
		for i := range parts {
			etag := strings.TrimSpace(parts[i].ETag)
			if strings.HasPrefix(etag, "\"") != strings.HasSuffix(etag, "\"") {
				return ErrInvalidImageUpload
			}
			etag = strings.TrimSuffix(strings.TrimPrefix(etag, "\""), "\"")
			if parts[i].PartNumber != int32(i+1) || !videoMultipartETagPattern.MatchString(etag) {
				return ErrInvalidImageUpload
			}
			// ETags are opaque and case-sensitive. Canonical quotes match S3's
			// completion representation without changing their contents.
			parts[i].ETag = "\"" + etag + "\""
		}
		encoded, err := common.Marshal(parts)
		if err != nil {
			return err
		}
		if record.upload.UploadManifest != "" {
			if record.upload.UploadManifest != string(encoded) {
				return ErrImageUploadConflict
			}
			return nil
		}
		if err := requirePendingImageUpload(record); err != nil {
			return err
		}
		if record.upload.UploadID == "" {
			return ErrImageUploadConflict
		}
		var waiting int64
		if err := tx.Model(&model.KKAIImageAsset{}).Where("kind = ? AND state = ?", model.ImageAssetKindReference, model.ImageAssetStateValidating).Count(&waiting).Error; err != nil {
			return err
		}
		if waiting >= imageUploadMaxValidating {
			return ErrImageUploadCapacity
		}
		now := time.Now()
		if err := tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", assetID).Updates(map[string]any{"upload_manifest": string(encoded), "updated_at": now.Unix()}).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND state = ?", assetID, model.ImageAssetStatePendingUpload).Updates(map[string]any{"state": model.ImageAssetStateValidating, "updated_at": now.Unix()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrImageUploadConflict
		}
		if err := EnqueueKKAIOutboxEvent(ctx, tx, fmt.Sprintf("image-upload:%d:validate:v1", assetID), ImageUploadValidateTopic, strconv.FormatInt(assetID, 10), now, imageUploadPayload{AssetID: assetID}); err != nil {
			return err
		}
		record.asset.State = model.ImageAssetStateValidating
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := record.view().Asset
	return &view, nil
}

func AbortImageReferenceUpload(ctx context.Context, db *gorm.DB, userID int, assetID int64) (*ImageUploadAssetView, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrInvalidImageUpload
	}
	var record imageUploadRecord
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		var err error
		record, err = loadImageUpload(ctx, tx, userID, assetID)
		if err != nil {
			return err
		}
		if record.asset.State == model.ImageAssetStateDeleted || record.asset.State == model.ImageAssetStateDeleting {
			return nil
		}
		var refs int64
		if err := tx.Model(&model.KKAIImageGenerationReference{}).Where("asset_id = ?", assetID).Count(&refs).Error; err != nil {
			return err
		}
		if refs != 0 {
			return ErrImageUploadConflict
		}
		now := time.Now().Unix()
		if err := tx.Model(&model.KKAIImageAsset{}).Where("id = ?", assetID).Updates(map[string]any{"state": model.ImageAssetStateDeleting, "failure_reason": "upload_aborted", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", assetID).Updates(map[string]any{"cleanup_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		record.asset.State, record.asset.FailureReason = model.ImageAssetStateDeleting, "upload_aborted"
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := record.view().Asset
	return &view, nil
}
