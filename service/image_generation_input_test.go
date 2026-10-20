package service

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageGenerationInputLegacyAndOwnership(t *testing.T) {
	db := newImageLibraryTestDB(t)
	generation := createImageLibraryGeneration(t, db, 7, "legacy input")
	for _, executionTable := range []bool{false, true} {
		if executionTable {
			require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}))
		}
		input, err := GetImageGenerationInput(context.Background(), db, 7, generation.ID)
		require.NoError(t, err)
		assert.Equal(t, "unknown", input.Mode)
		assert.False(t, input.ReferencesAvailable)
		assert.Empty(t, input.References)
		assert.NotNil(t, input.References)
	}
	_, err := GetImageGenerationInput(context.Background(), db, 8, generation.ID)
	assert.ErrorIs(t, err, ErrImageGenerationNotFound)
	require.NoError(t, db.Model(&generation).Update("deleted_at", 1).Error)
	_, err = GetImageGenerationInput(context.Background(), db, 7, generation.ID)
	assert.ErrorIs(t, err, ErrImageGenerationNotFound)
}

func TestImageGenerationInputPreservesReferenceOrderWithoutWrites(t *testing.T) {
	db := newImageLibraryTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}))
	generation := createImageLibraryGeneration(t, db, 7, "edit input")
	assets := []model.KKAIImageAsset{
		{OwnerUserID: 7, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateReady, ObjectKey: "first-input", SHA256: strings.Repeat("a", 64), MIMEType: "image/png", SizeBytes: 100, Width: 20, Height: 10},
		{OwnerUserID: 7, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateReady, ObjectKey: "second-input", SHA256: strings.Repeat("b", 64), MIMEType: "image/webp", SizeBytes: 200, Width: 10, Height: 20},
	}
	require.NoError(t, db.Create(&assets).Error)
	seedImageGenerationInputExecution(t, db, generation, ImageStudioModeEdit, []int64{assets[1].ID, assets[0].ID})
	textGeneration := createImageLibraryGeneration(t, db, 7, "text input")
	seedImageGenerationInputExecution(t, db, textGeneration, ImageStudioModeGeneration, nil)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error) })

	input, err := GetImageGenerationInput(context.Background(), db, 7, generation.ID)
	require.NoError(t, err)
	assert.Equal(t, generation.ID, input.GenerationID)
	assert.Equal(t, ImageStudioModeEdit, input.Mode)
	assert.True(t, input.ReferencesAvailable)
	assert.Equal(t, []ImageGenerationInputReference{
		{ID: assets[1].ID, SHA256: assets[1].SHA256, SizeBytes: 200, MIMEType: "image/webp", Width: 10, Height: 20},
		{ID: assets[0].ID, SHA256: assets[0].SHA256, SizeBytes: 100, MIMEType: "image/png", Width: 20, Height: 10},
	}, input.References)
	input, err = GetImageGenerationInput(context.Background(), db, 7, textGeneration.ID)
	require.NoError(t, err)
	assert.Equal(t, ImageStudioModeGeneration, input.Mode)
	assert.True(t, input.ReferencesAvailable)
	assert.Empty(t, input.References)
}

func TestImageGenerationInputRejectsUnavailableReferences(t *testing.T) {
	for _, test := range []struct {
		name    string
		changes map[string]any
		missing bool
	}{
		{name: "missing", missing: true},
		{name: "another owner", changes: map[string]any{"owner_user_id": 8}},
		{name: "output instead of reference", changes: map[string]any{"kind": model.ImageAssetKindOutput}},
		{name: "expired and cleaned", changes: map[string]any{"state": model.ImageAssetStateDeleted}},
		{name: "deletion in progress", changes: map[string]any{"state": model.ImageAssetStateDeleting}},
		{name: "soft deleted", changes: map[string]any{"deleted_at": 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newImageLibraryTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}))
			generation := createImageLibraryGeneration(t, db, 7, test.name)
			retained := model.KKAIImageAsset{OwnerUserID: 7, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateReady, ObjectKey: "retained-input"}
			require.NoError(t, db.Create(&retained).Error)
			asset := model.KKAIImageAsset{OwnerUserID: 7, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateReady, ObjectKey: "input"}
			require.NoError(t, db.Create(&asset).Error)
			seedImageGenerationInputExecution(t, db, generation, ImageStudioModeEdit, []int64{retained.ID, asset.ID})
			if test.missing {
				require.NoError(t, db.Delete(&asset).Error)
			} else {
				require.NoError(t, db.Model(&asset).Updates(test.changes).Error)
			}
			input, err := GetImageGenerationInput(context.Background(), db, 7, generation.ID)
			require.NoError(t, err)
			assert.Equal(t, ImageStudioModeEdit, input.Mode)
			assert.False(t, input.ReferencesAvailable)
			require.Len(t, input.References, 1)
			assert.Equal(t, retained.ID, input.References[0].ID)
		})
	}
}

func TestImageGenerationInputDamagedPayloadNeverBecomesGeneration(t *testing.T) {
	for _, test := range []struct {
		name    string
		version int
		owner   int
		mode    string
		ids     []int64
		raw     string
	}{
		{name: "invalid JSON", raw: "{broken"},
		{name: "unknown version", version: 2, owner: 7, mode: ImageStudioModeGeneration},
		{name: "wrong payload owner", version: 1, owner: 8, mode: ImageStudioModeGeneration},
		{name: "unknown mode", version: 1, owner: 7, mode: "future-mode"},
		{name: "generation with references", version: 1, owner: 7, mode: ImageStudioModeGeneration, ids: []int64{1}},
		{name: "edit without references", version: 1, owner: 7, mode: ImageStudioModeEdit},
		{name: "duplicate references", version: 1, owner: 7, mode: ImageStudioModeEdit, ids: []int64{1, 1}},
		{name: "invalid reference ID", version: 1, owner: 7, mode: ImageStudioModeEdit, ids: []int64{-1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newImageLibraryTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}))
			generation := createImageLibraryGeneration(t, db, 7, test.name)
			payload := test.raw
			if payload == "" {
				encoded, err := common.Marshal(ImageExecutionPayload{Version: test.version, Submission: NormalizedImageStudioSubmission{UserID: test.owner, Mode: test.mode}, AssetIDs: test.ids})
				require.NoError(t, err)
				payload = string(encoded)
			}
			require.NoError(t, db.Create(&model.KKAIImageExecution{GenerationID: generation.ID, Payload: payload}).Error)
			input, err := GetImageGenerationInput(context.Background(), db, 7, generation.ID)
			require.NoError(t, err)
			assert.Equal(t, "unknown", input.Mode)
			assert.False(t, input.ReferencesAvailable)
			assert.Empty(t, input.References)
		})
	}
}

func seedImageGenerationInputExecution(t *testing.T, db *gorm.DB, generation model.KKAIImageGeneration, mode string, ids []int64) {
	t.Helper()
	encoded, err := common.Marshal(ImageExecutionPayload{
		Version: 1, Submission: NormalizedImageStudioSubmission{UserID: generation.UserID, Mode: mode}, AssetIDs: ids,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIImageExecution{GenerationID: generation.ID, Payload: string(encoded)}).Error)
}
