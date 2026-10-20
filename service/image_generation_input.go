package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type ImageGenerationInputReference struct {
	ID        int64  `json:"id"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	MIMEType  string `json:"mime_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type ImageGenerationInput struct {
	GenerationID        int64                           `json:"generation_id"`
	Mode                string                          `json:"mode"`
	ReferencesAvailable bool                            `json:"references_available"`
	References          []ImageGenerationInputReference `json:"references"`
}

// GetImageGenerationInput describes reusable inputs without dispatching a new
// request or changing the lifetime of reference uploads.
func GetImageGenerationInput(ctx context.Context, db *gorm.DB, userID int, generationID int64) (*ImageGenerationInput, error) {
	generation, err := GetOwnedImageGeneration(ctx, db, userID, generationID)
	if err != nil {
		return nil, err
	}
	input := &ImageGenerationInput{GenerationID: generation.ID, Mode: "unknown", References: []ImageGenerationInputReference{}}
	if !db.WithContext(ctx).Migrator().HasTable(&model.KKAIImageExecution{}) {
		return input, nil
	}
	var execution model.KKAIImageExecution
	err = db.WithContext(ctx).Select("payload").Where("generation_id = ?", generation.ID).Take(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return input, nil
	}
	if err != nil {
		return nil, err
	}
	// Read only the input identity; quote, token and provider details stay private.
	var payload struct {
		Version    int `json:"version"`
		Submission struct {
			UserID int
			Mode   string
		} `json:"submission"`
		AssetIDs []int64 `json:"asset_ids"`
	}
	if common.UnmarshalJsonStr(execution.Payload, &payload) != nil || payload.Version != 1 || payload.Submission.UserID != userID {
		return input, nil
	}
	switch payload.Submission.Mode {
	case ImageStudioModeGeneration:
		if len(payload.AssetIDs) == 0 {
			input.Mode, input.ReferencesAvailable = ImageStudioModeGeneration, true
		}
		return input, nil
	case ImageStudioModeEdit:
		if len(payload.AssetIDs) == 0 || len(payload.AssetIDs) > MaxImageStudioReferenceImages {
			return input, nil
		}
	default:
		return input, nil
	}
	seen := make(map[int64]bool, len(payload.AssetIDs))
	for _, id := range payload.AssetIDs {
		if id <= 0 || seen[id] {
			return input, nil
		}
		seen[id] = true
	}
	var assets []model.KKAIImageAsset
	if err := db.WithContext(ctx).Where("id IN ? AND owner_user_id = ? AND kind = ? AND state = ? AND deleted_at = 0", payload.AssetIDs, userID, model.ImageAssetKindReference, model.ImageAssetStateReady).Find(&assets).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]model.KKAIImageAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	input.Mode, input.ReferencesAvailable = ImageStudioModeEdit, len(assets) == len(payload.AssetIDs)
	for _, id := range payload.AssetIDs {
		asset, found := byID[id]
		if !found {
			continue
		}
		input.References = append(input.References, ImageGenerationInputReference{
			ID: asset.ID, SHA256: asset.SHA256, SizeBytes: asset.SizeBytes,
			MIMEType: asset.MIMEType, Width: asset.Width, Height: asset.Height,
		})
	}
	return input, nil
}
