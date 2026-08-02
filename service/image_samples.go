package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const ImageCatalogOrphanTTL = 24 * time.Hour

var (
	ErrInvalidImageSample        = errors.New("invalid image sample")
	ErrImageSampleNotFound       = errors.New("image sample not found")
	ErrImageSampleConflict       = errors.New("image sample changed concurrently")
	ErrImageSampleNotPublishable = errors.New("image sample cannot be published")
	ErrImageSampleImmutable      = errors.New("image sample profile and asset cannot be changed")
	imageSampleCategoryPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

type ImageSampleInput struct {
	ModelProfileID int64          `json:"model_profile_id"`
	ImageAssetID   int64          `json:"image_asset_id"`
	Title          string         `json:"title"`
	Prompt         string         `json:"prompt"`
	Parameters     map[string]any `json:"parameters"`
	Category       string         `json:"category"`
	Status         string         `json:"status"`
	SortOrder      int            `json:"sort_order"`
}

type ImageSampleView struct {
	ID             int64          `json:"id"`
	ModelProfileID int64          `json:"model_profile_id"`
	ImageAssetID   int64          `json:"image_asset_id"`
	Model          string         `json:"model"`
	Title          string         `json:"title"`
	Prompt         string         `json:"prompt"`
	ModelVersion   int            `json:"model_version"`
	Parameters     map[string]any `json:"parameters"`
	Category       string         `json:"category"`
	Status         string         `json:"status"`
	SortOrder      int            `json:"sort_order"`
	Asset          ImageAssetView `json:"asset"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
}

type ImageSamplePage struct {
	Items      []ImageSampleView `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type imageSampleCursor struct {
	SortOrder int
	ID        int64
}

func CreateImageCatalogAsset(
	ctx context.Context,
	db *gorm.DB,
	store ImageAssetStore,
	validator ImageArchiveUploadValidator,
	adminUserID int,
	filename string,
	declaredMIME string,
	reader io.Reader,
) (*ImageAssetView, error) {
	if db == nil || store == nil || validator == nil || adminUserID <= 0 || reader == nil {
		return nil, ErrInvalidImageSample
	}
	filename = sanitizeImageAssetFilename(filename)
	if filename == "" {
		return nil, ErrInvalidImageSample
	}
	settings := image_studio_setting.Get()
	archive, err := validator.Ingest(reader, declaredMIME, settings.MaxOutputBytes, settings.MaxPixels)
	if err != nil {
		return nil, err
	}
	defer archive.Remove()
	file, err := os.Open(archive.Path)
	if err != nil {
		return nil, fmt.Errorf("open catalog image asset: %w", err)
	}
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: adminUserID, Scope: model.ImageAssetScopeCatalog, Kind: model.ImageAssetKindSample,
		State: model.ImageAssetStateStaging, Position: 0,
		ObjectKey:      "image-studio/catalog/" + uuid.NewString() + "-original",
		ThumbnailState: model.ImageThumbnailStatePending, OriginalFilename: filename,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&asset).Error; err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("create catalog image asset: %w", err)
	}
	putErr := store.Put(ctx, asset.ObjectKey, archive.MIMEType, file, archive.SizeBytes)
	closeErr := file.Close()
	if putErr != nil || closeErr != nil {
		_ = db.WithContext(ctx).Model(&model.KKAIImageAsset{}).Where("id = ?", asset.ID).
			Updates(map[string]any{
				"state": model.ImageAssetStateFailed, "thumbnail_state": model.ImageThumbnailStateFailed,
				"failure_reason": "object_store_write_failed", "updated_at": time.Now().Unix(),
			}).Error
		return nil, fmt.Errorf("store catalog image asset: %w", errors.Join(putErr, closeErr))
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND state = ?", asset.ID, model.ImageAssetStateStaging).
			Updates(map[string]any{
				"state": model.ImageAssetStateReady, "mime_type": archive.MIMEType,
				"size_bytes": archive.SizeBytes, "width": archive.Width, "height": archive.Height,
				"sha256": archive.SHA256, "updated_at": time.Now().Unix(),
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			if updated.Error != nil {
				return updated.Error
			}
			return ErrInvalidImageSample
		}
		return EnqueueKKAIOutboxEvent(
			ctx, tx, "image-thumbnail:"+strconv.FormatInt(asset.ID, 10)+":v1",
			ImageAssetThumbnailTopic, strconv.FormatInt(asset.ID, 10), time.Now(),
			imageThumbnailPayload{AssetID: asset.ID},
		)
	})
	if err != nil {
		_ = store.Delete(ctx, []string{asset.ObjectKey})
		return nil, fmt.Errorf("finalize catalog image asset: %w", err)
	}
	asset.State = model.ImageAssetStateReady
	asset.MIMEType = archive.MIMEType
	asset.SizeBytes = archive.SizeBytes
	asset.Width = archive.Width
	asset.Height = archive.Height
	asset.SHA256 = archive.SHA256
	view := imageAssetView(asset)
	return &view, nil
}

func CreateImageSample(ctx context.Context, db *gorm.DB, input ImageSampleInput) (*ImageSampleView, error) {
	normalized, profile, asset, parameters, err := normalizeImageSampleInput(ctx, db, input)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	sample := model.KKAIImageSample{
		ModelProfileID: profile.ID, ImageAssetID: asset.ID, Title: normalized.Title,
		Prompt: normalized.Prompt, ModelVersion: profile.SpecificationVersion,
		Parameters: string(encoded), Category: normalized.Category, Status: normalized.Status,
		SortOrder: normalized.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentAsset model.KKAIImageAsset
		if err := lockRowsForUpdate(tx).First(
			&currentAsset,
			"id = ? AND scope = ? AND kind = ? AND state = ? AND deleted_at = 0",
			asset.ID, model.ImageAssetScopeCatalog, model.ImageAssetKindSample, model.ImageAssetStateReady,
		).Error; err != nil {
			return imageSampleLookupError(err)
		}
		return tx.Create(&sample).Error
	}); err != nil {
		return nil, fmt.Errorf("create image sample: %w", err)
	}
	return GetImageSample(ctx, db, sample.ID, true, nil)
}

func ReconcileOrphanedImageCatalogAssets(
	ctx context.Context,
	db *gorm.DB,
	olderThan time.Time,
	limit int,
) (int, error) {
	if db == nil || olderThan.IsZero() || limit <= 0 || limit > 500 {
		return 0, ErrInvalidImageSample
	}
	var ids []int64
	if err := db.WithContext(ctx).Model(&model.KKAIImageAsset{}).
		Where(
			"scope = ? AND kind = ? AND deleted_at = 0 AND created_at <= ? AND "+
				"NOT EXISTS (SELECT 1 FROM kkai_image_samples WHERE kkai_image_samples.image_asset_id = kkai_image_assets.id)",
			model.ImageAssetScopeCatalog, model.ImageAssetKindSample, olderThan.Unix(),
		).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("list orphaned image catalog assets: %w", err)
	}
	reconciled := 0
	for _, assetID := range ids {
		removed, err := reconcileOrphanedImageCatalogAsset(ctx, db, assetID, olderThan)
		if err != nil {
			return reconciled, fmt.Errorf("reconcile orphaned image catalog asset %d: %w", assetID, err)
		}
		if removed {
			reconciled++
		}
	}
	return reconciled, nil
}

func reconcileOrphanedImageCatalogAsset(
	ctx context.Context,
	db *gorm.DB,
	assetID int64,
	olderThan time.Time,
) (bool, error) {
	removed := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var asset model.KKAIImageAsset
		if err := lockRowsForUpdate(tx).First(
			&asset,
			"id = ? AND scope = ? AND kind = ? AND deleted_at = 0 AND created_at <= ?",
			assetID, model.ImageAssetScopeCatalog, model.ImageAssetKindSample, olderThan.Unix(),
		).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var references int64
		if err := tx.Model(&model.KKAIImageSample{}).
			Where("image_asset_id = ?", asset.ID).Count(&references).Error; err != nil {
			return err
		}
		if references != 0 {
			return nil
		}
		now := time.Now()
		updated := tx.Model(&model.KKAIImageAsset{}).
			Where("id = ? AND deleted_at = 0", asset.ID).
			Updates(map[string]any{
				"state":      model.ImageAssetStateDeleted,
				"deleted_at": now.Unix(),
				"updated_at": now.Unix(),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return nil
		}
		if err := EnqueueKKAIOutboxEvent(
			ctx, tx, "image-delete:"+strconv.FormatInt(asset.ID, 10)+":v1",
			ImageAssetDeleteTopic, strconv.FormatInt(asset.ID, 10), now,
			imageAssetDeletePayload{
				AssetID: asset.ID, ObjectKey: asset.ObjectKey,
				ThumbnailObjectKey: asset.ThumbnailObjectKey,
			},
		); err != nil {
			return err
		}
		removed = true
		return nil
	})
	return removed, err
}

func UpdateImageSample(ctx context.Context, db *gorm.DB, id int64, input ImageSampleInput) (*ImageSampleView, error) {
	if db == nil || id <= 0 {
		return nil, ErrImageSampleNotFound
	}
	var current model.KKAIImageSample
	if err := db.WithContext(ctx).First(&current, "id = ?", id).Error; err != nil {
		return nil, imageSampleLookupError(err)
	}
	if input.ModelProfileID != current.ModelProfileID || input.ImageAssetID != current.ImageAssetID {
		return nil, ErrImageSampleImmutable
	}
	normalized, profile, _, parameters, err := normalizeImageSampleInput(ctx, db, input)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now <= current.UpdatedAt {
		now = current.UpdatedAt + 1
	}
	updated := db.WithContext(ctx).Model(&model.KKAIImageSample{}).Where("id = ? AND updated_at = ?", id, current.UpdatedAt).
		Updates(map[string]any{
			"title": normalized.Title, "prompt": normalized.Prompt,
			"model_version": profile.SpecificationVersion, "parameters": string(encoded),
			"category": normalized.Category, "status": normalized.Status,
			"sort_order": normalized.SortOrder, "updated_at": now,
		})
	if updated.Error != nil {
		return nil, fmt.Errorf("update image sample: %w", updated.Error)
	}
	if updated.RowsAffected != 1 {
		return nil, ErrImageSampleConflict
	}
	return GetImageSample(ctx, db, id, true, nil)
}

func DeleteImageSample(ctx context.Context, db *gorm.DB, id int64) error {
	if db == nil || id <= 0 {
		return ErrImageSampleNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sample model.KKAIImageSample
		if err := tx.First(&sample, "id = ?", id).Error; err != nil {
			return imageSampleLookupError(err)
		}
		if err := tx.Delete(&sample).Error; err != nil {
			return err
		}
		var references int64
		if err := tx.Model(&model.KKAIImageSample{}).Where("image_asset_id = ?", sample.ImageAssetID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return nil
		}
		var asset model.KKAIImageAsset
		if err := tx.First(&asset, "id = ? AND deleted_at = 0", sample.ImageAssetID).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND deleted_at = 0", asset.ID).
			Updates(map[string]any{
				"state": model.ImageAssetStateDeleted, "deleted_at": now.Unix(), "updated_at": now.Unix(),
			}).Error; err != nil {
			return err
		}
		return EnqueueKKAIOutboxEvent(
			ctx, tx, "image-delete:"+strconv.FormatInt(asset.ID, 10)+":v1",
			ImageAssetDeleteTopic, strconv.FormatInt(asset.ID, 10), now,
			imageAssetDeletePayload{
				AssetID: asset.ID, ObjectKey: asset.ObjectKey, ThumbnailObjectKey: asset.ThumbnailObjectKey,
			},
		)
	})
}

func ListImageSamples(
	ctx context.Context,
	db *gorm.DB,
	modelName string,
	category string,
	cursor string,
	limit int,
	includeDrafts bool,
	allowedModels []string,
) (*ImageSamplePage, error) {
	modelName = strings.TrimSpace(modelName)
	category = strings.TrimSpace(category)
	if db == nil || len(modelName) > 191 || (category != "" && !imageSampleCategoryPattern.MatchString(category)) {
		return nil, ErrInvalidImageSample
	}
	if limit == 0 {
		limit = 24
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidImageSample
	}
	var pageCursor *imageSampleCursor
	if cursor != "" {
		parsed, err := parseImageSampleCursor(cursor)
		if err != nil {
			return nil, ErrInvalidImageSample
		}
		pageCursor = parsed
	}
	query := db.WithContext(ctx).Model(&model.KKAIImageSample{}).
		Joins("JOIN kkai_image_model_profiles ON kkai_image_model_profiles.id = kkai_image_samples.model_profile_id")
	if !includeDrafts {
		if len(allowedModels) == 0 {
			return &ImageSamplePage{Items: []ImageSampleView{}}, nil
		}
		query = query.Where(
			"kkai_image_samples.status = ? AND kkai_image_model_profiles.enabled = ? AND "+
				"kkai_image_samples.model_version = kkai_image_model_profiles.specification_version AND "+
				"kkai_image_model_profiles.model IN ?",
			model.ImageSampleStatusPublished, true, allowedModels,
		)
	}
	if modelName != "" {
		query = query.Where("kkai_image_model_profiles.model = ?", modelName)
	}
	if category != "" {
		query = query.Where("kkai_image_samples.category = ?", category)
	}
	if pageCursor != nil {
		query = query.Where(
			"(kkai_image_samples.sort_order > ?) OR "+
				"(kkai_image_samples.sort_order = ? AND kkai_image_samples.id < ?)",
			pageCursor.SortOrder, pageCursor.SortOrder, pageCursor.ID,
		)
	}
	var samples []model.KKAIImageSample
	if err := query.Select("kkai_image_samples.*").
		Order("kkai_image_samples.sort_order ASC, kkai_image_samples.id DESC").
		Limit(limit + 1).Find(&samples).Error; err != nil {
		return nil, fmt.Errorf("list image samples: %w", err)
	}
	hasMore := len(samples) > limit
	if hasMore {
		samples = samples[:limit]
	}
	page := &ImageSamplePage{Items: make([]ImageSampleView, 0, len(samples))}
	if len(samples) == 0 {
		return page, nil
	}
	profileIDs := make([]int64, 0, len(samples))
	assetIDs := make([]int64, 0, len(samples))
	for _, sample := range samples {
		profileIDs = append(profileIDs, sample.ModelProfileID)
		assetIDs = append(assetIDs, sample.ImageAssetID)
	}
	var profiles []model.KKAIImageModelProfile
	if err := db.WithContext(ctx).Select("id", "model").Where("id IN ?", profileIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	var assets []model.KKAIImageAsset
	if err := db.WithContext(ctx).Where("id IN ? AND deleted_at = 0", assetIDs).Find(&assets).Error; err != nil {
		return nil, err
	}
	profilesByID := make(map[int64]model.KKAIImageModelProfile, len(profiles))
	for _, profile := range profiles {
		profilesByID[profile.ID] = profile
	}
	assetsByID := make(map[int64]model.KKAIImageAsset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}
	for _, sample := range samples {
		profile, profileExists := profilesByID[sample.ModelProfileID]
		asset, assetExists := assetsByID[sample.ImageAssetID]
		if !profileExists || !assetExists {
			return nil, ErrImageSampleNotPublishable
		}
		view, err := buildImageSampleView(sample, profile, asset)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, view)
	}
	if hasMore {
		page.NextCursor = formatImageSampleCursor(samples[len(samples)-1])
	}
	return page, nil
}

func GetImageSample(
	ctx context.Context,
	db *gorm.DB,
	id int64,
	includeDrafts bool,
	allowedModels []string,
) (*ImageSampleView, error) {
	if db == nil || id <= 0 {
		return nil, ErrImageSampleNotFound
	}
	var sample model.KKAIImageSample
	query := db.WithContext(ctx).Model(&model.KKAIImageSample{}).
		Joins("JOIN kkai_image_model_profiles ON kkai_image_model_profiles.id = kkai_image_samples.model_profile_id")
	if !includeDrafts {
		if len(allowedModels) == 0 {
			return nil, ErrImageSampleNotFound
		}
		query = query.Where(
			"kkai_image_samples.status = ? AND kkai_image_model_profiles.enabled = ? AND "+
				"kkai_image_samples.model_version = kkai_image_model_profiles.specification_version AND "+
				"kkai_image_model_profiles.model IN ?",
			model.ImageSampleStatusPublished, true, allowedModels,
		)
	}
	if err := query.Select("kkai_image_samples.*").First(&sample, "kkai_image_samples.id = ?", id).Error; err != nil {
		return nil, imageSampleLookupError(err)
	}
	view, err := imageSampleView(ctx, db, sample)
	return &view, err
}

func normalizeImageSampleInput(
	ctx context.Context,
	db *gorm.DB,
	input ImageSampleInput,
) (ImageSampleInput, model.KKAIImageModelProfile, model.KKAIImageAsset, map[string]any, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Category = strings.TrimSpace(strings.ToLower(input.Category))
	input.Status = strings.TrimSpace(strings.ToLower(input.Status))
	if db == nil || input.ModelProfileID <= 0 || input.ImageAssetID <= 0 || input.Title == "" || len(input.Title) > 191 ||
		input.Prompt == "" || len(input.Prompt) > 8000 || !imageSampleCategoryPattern.MatchString(input.Category) ||
		(input.Status != model.ImageSampleStatusDraft && input.Status != model.ImageSampleStatusPublished) ||
		input.SortOrder < -100000 || input.SortOrder > 100000 {
		return ImageSampleInput{}, model.KKAIImageModelProfile{}, model.KKAIImageAsset{}, nil, ErrInvalidImageSample
	}
	var profile model.KKAIImageModelProfile
	if err := db.WithContext(ctx).First(&profile, "id = ?", input.ModelProfileID).Error; err != nil {
		return ImageSampleInput{}, profile, model.KKAIImageAsset{}, nil, ErrImageModelProfileNotFound
	}
	specification, defaults, err := decodeImageModelProfile(profile)
	if err != nil {
		return ImageSampleInput{}, profile, model.KKAIImageAsset{}, nil, err
	}
	parameters := make(map[string]any, len(defaults)+len(input.Parameters))
	for key, value := range defaults {
		parameters[key] = value
	}
	for key, value := range input.Parameters {
		parameters[key] = value
	}
	parameters, err = ValidateImageParameters(specification, parameters, true)
	if err != nil {
		return ImageSampleInput{}, profile, model.KKAIImageAsset{}, nil, err
	}
	var asset model.KKAIImageAsset
	if err := db.WithContext(ctx).First(&asset, "id = ? AND deleted_at = 0", input.ImageAssetID).Error; err != nil {
		return ImageSampleInput{}, profile, asset, nil, ErrImageAssetNotFound
	}
	if asset.Scope != model.ImageAssetScopeCatalog || asset.Kind != model.ImageAssetKindSample || asset.State != model.ImageAssetStateReady {
		return ImageSampleInput{}, profile, asset, nil, ErrImageSampleNotPublishable
	}
	if input.Status == model.ImageSampleStatusPublished && !profile.Enabled {
		return ImageSampleInput{}, profile, asset, nil, ErrImageSampleNotPublishable
	}
	return input, profile, asset, parameters, nil
}

func imageSampleView(ctx context.Context, db *gorm.DB, sample model.KKAIImageSample) (ImageSampleView, error) {
	var profile model.KKAIImageModelProfile
	if err := db.WithContext(ctx).Select("model").First(&profile, "id = ?", sample.ModelProfileID).Error; err != nil {
		return ImageSampleView{}, err
	}
	var asset model.KKAIImageAsset
	if err := db.WithContext(ctx).First(&asset, "id = ? AND deleted_at = 0", sample.ImageAssetID).Error; err != nil {
		return ImageSampleView{}, err
	}
	return buildImageSampleView(sample, profile, asset)
}

func buildImageSampleView(
	sample model.KKAIImageSample,
	profile model.KKAIImageModelProfile,
	asset model.KKAIImageAsset,
) (ImageSampleView, error) {
	parameters := map[string]any{}
	if err := common.UnmarshalJsonStr(sample.Parameters, &parameters); err != nil {
		return ImageSampleView{}, fmt.Errorf("decode image sample parameters: %w", err)
	}
	return ImageSampleView{
		ID: sample.ID, ModelProfileID: sample.ModelProfileID, ImageAssetID: sample.ImageAssetID,
		Model: profile.Model, Title: sample.Title, Prompt: sample.Prompt, ModelVersion: sample.ModelVersion,
		Parameters: parameters, Category: sample.Category, Status: sample.Status, SortOrder: sample.SortOrder,
		Asset: imageAssetView(asset), CreatedAt: sample.CreatedAt, UpdatedAt: sample.UpdatedAt,
	}, nil
}

func imageAssetView(asset model.KKAIImageAsset) ImageAssetView {
	view := ImageAssetView{
		ID: asset.ID, Position: asset.Position, State: asset.State,
		ThumbnailState: asset.ThumbnailState, MIMEType: asset.MIMEType,
		SizeBytes: asset.SizeBytes, Width: asset.Width, Height: asset.Height,
		FailureReason: asset.FailureReason,
	}
	if asset.State == model.ImageAssetStateReady {
		view.ContentURL = imageAssetContentPath(asset.ID, false)
		view.DownloadURL = imageAssetDownloadPath(asset.ID)
		if asset.ThumbnailState == model.ImageThumbnailStateReady && asset.ThumbnailObjectKey != "" {
			view.ThumbnailURL = imageAssetContentPath(asset.ID, true)
		}
	}
	return view
}

func sanitizeImageAssetFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._-", character) {
			return character
		}
		return '-'
	}, value)
	value = strings.Trim(value, ".-")
	if len(value) > 191 {
		value = value[:191]
	}
	return value
}

func parseImageSampleCursor(value string) (*imageSampleCursor, error) {
	sortOrderText, idText, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return nil, ErrInvalidImageSample
	}
	sortOrder, sortErr := strconv.Atoi(sortOrderText)
	id, idErr := strconv.ParseInt(idText, 10, 64)
	if sortErr != nil || idErr != nil || sortOrder < -100000 || sortOrder > 100000 || id <= 0 {
		return nil, ErrInvalidImageSample
	}
	return &imageSampleCursor{SortOrder: sortOrder, ID: id}, nil
}

func formatImageSampleCursor(sample model.KKAIImageSample) string {
	return strconv.Itoa(sample.SortOrder) + ":" + strconv.FormatInt(sample.ID, 10)
}

func imageSampleLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrImageSampleNotFound
	}
	return fmt.Errorf("get image sample: %w", err)
}
