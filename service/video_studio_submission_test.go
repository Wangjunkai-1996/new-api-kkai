package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type videoSubmissionTestStore struct{}

func (videoSubmissionTestStore) PresignUpload(context.Context, string, string, int64, time.Duration) (VideoAssetSignedRequest, error) {
	return VideoAssetSignedRequest{}, nil
}

func (videoSubmissionTestStore) PresignDownload(_ context.Context, key string, _ string, _ bool, _ time.Duration) (string, error) {
	return "https://assets.invalid/" + key, nil
}

func (videoSubmissionTestStore) Head(context.Context, string) (VideoAssetObjectMetadata, error) {
	return VideoAssetObjectMetadata{}, nil
}

func (videoSubmissionTestStore) Get(context.Context, string) (VideoAssetObject, error) {
	return VideoAssetObject{}, nil
}

func (videoSubmissionTestStore) Put(context.Context, string, string, io.Reader, int64) error {
	return nil
}

func (videoSubmissionTestStore) Delete(context.Context, []string) error {
	return nil
}

func newVideoSubmissionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-submission-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.KKAIVideoModelProfile{},
		&model.KKAIVideoSample{},
		&model.KKAIVideoAsset{},
	))
	return db
}

func createEnabledVideoSubmissionProfile(
	t *testing.T,
	db *gorm.DB,
	modelName string,
	specification VideoModelSpec,
	defaults map[string]any,
) model.KKAIVideoModelProfile {
	t.Helper()
	specificationJSON, err := common.Marshal(specification)
	require.NoError(t, err)
	defaultsJSON, err := common.Marshal(defaults)
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: modelName, DisplayName: modelName, SpecificationVersion: specification.Version,
		Specification: string(specificationJSON), DefaultParameters: string(defaultsJSON), Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&profile).Error)
	return profile
}

func createVideoSubmissionFixture(t *testing.T, db *gorm.DB) (model.KKAIVideoModelProfile, model.KKAIVideoSample, []model.KKAIVideoAsset) {
	t.Helper()
	specification := VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeFirstLastFrame},
		ReferenceInputs: []VideoReferenceInputSpec{
			{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
			{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
		},
	}
	specificationJSON, err := common.Marshal(specification)
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "video-model-v1", DisplayName: "Video Model", SpecificationVersion: 1,
		Specification: string(specificationJSON), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&profile).Error)

	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 1, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "first.jpg", MIMEType: "image/jpeg"},
		{OwnerUserID: 1, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "last.jpg", MIMEType: "image/jpeg"},
		{OwnerUserID: 1, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "sample.mp4", MIMEType: "video/mp4"},
	}
	for index := range assets {
		assets[index].CreatedAt = time.Now().Unix()
		assets[index].UpdatedAt = assets[index].CreatedAt
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	referenceIDs, err := common.Marshal([]int64{assets[0].ID, assets[1].ID})
	require.NoError(t, err)
	sample := model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Sample", Prompt: "A controlled camera move",
		Mode: VideoModeFirstLastFrame, ModelVersion: 1, Parameters: `{}`,
		ReferenceAssetIDs: string(referenceIDs), VideoAssetID: assets[2].ID, AspectRatio: 16.0 / 9.0,
		Status: model.VideoSampleStatusPublished, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&sample).Error)
	return profile, sample, assets
}

func TestNormalizeVideoStudioSubmissionDerivesModelFromSample(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, sample, _ := createVideoSubmissionFixture(t, db)
	require.NoError(t, db.Model(&sample).UpdateColumn("category", nil).Error)

	normalized, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, VideoStudioSubmissionRequest{
		SampleID: &sample.ID,
	})
	require.NoError(t, err)
	require.Equal(t, profile.Model, normalized.Model)
	require.Equal(t, sample.Prompt, normalized.Prompt)
	require.Equal(t, sample.Mode, normalized.Mode)
}

func TestNormalizeVideoStudioSubmissionRejectsMissingPersistedProfile(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	_, err := NormalizeVideoStudioSubmission(
		context.Background(), db, videoSubmissionTestStore{}, 42,
		VideoStudioSubmissionRequest{
			TokenID: 1, Model: "runtime-text-model", Mode: VideoModeTextToVideo, Prompt: "A calm orbit",
		},
	)
	require.ErrorIs(t, err, ErrVideoModelProfileNotFound)
}

func TestNormalizeVideoStudioSubmissionRejectsDisabledProfileInsteadOfRuntimeFallback(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, _, _ := createVideoSubmissionFixture(t, db)
	require.NoError(t, db.Model(&model.KKAIVideoModelProfile{}).
		Where("id = ?", profile.ID).
		Update("enabled", false).Error)

	_, err := NormalizeVideoStudioSubmission(
		context.Background(), db, videoSubmissionTestStore{}, 42,
		VideoStudioSubmissionRequest{
			TokenID: 1, Model: profile.Model, Mode: VideoModeTextToVideo, Prompt: "Must stay disabled",
		},
	)
	require.ErrorIs(t, err, ErrVideoModelProfileNotFound)
}

func TestNormalizeVideoStudioSubmissionPersistedI2VRequiresImage(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile := createEnabledVideoSubmissionProfile(t, db, "wan2.7-i2v", VideoModelSpec{
		Version: 1, Modes: []string{VideoModeImageToVideo}, ReferenceInputs: []VideoReferenceInputSpec{{
			Role: model.VideoTaskAssetRoleReference, RequestKey: "image", Required: true,
		}},
	}, map[string]any{})
	asset := model.KKAIVideoAsset{
		OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "runtime-i2v.png", MIMEType: "image/png",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)
	request := VideoStudioSubmissionRequest{
		TokenID: 1, Model: "wan2.7-i2v", Mode: VideoModeImageToVideo, Prompt: "Animate the image",
	}
	_, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)

	request.ReferenceAssets = []VideoStudioReferenceAssetInput{{
		AssetID: asset.ID, Role: model.VideoTaskAssetRoleReference,
	}}
	normalized, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	require.Equal(t, profile.ID, normalized.ProfileID)
	require.Equal(t, model.VideoTaskAssetRoleReference, normalized.ReferenceAssets[0].Role)
}

func TestNormalizeVideoStudioSubmissionPersistedVideoReferenceUsesSecondsContract(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	minimum, maximum, step := float64(4), float64(15), float64(1)
	profile := createEnabledVideoSubmissionProfile(t, db, "sd_2.0_special_1080p_with_video_ref", VideoModelSpec{
		Version: 2, Modes: []string{VideoModeImageToVideo}, Parameters: []VideoParameterSpec{{
			Key: "duration", Label: "Duration", Control: VideoControlNumber, RequestKey: "seconds",
			Required: true, Default: float64(5), Min: &minimum, Max: &maximum, Step: &step,
		}}, ReferenceInputs: []VideoReferenceInputSpec{{
			Role: model.VideoTaskAssetRoleReferenceVideo, RequestKey: "reference_video", Required: true,
		}},
	}, map[string]any{})
	now := time.Now().Unix()
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "wrong.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "reference.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	request := VideoStudioSubmissionRequest{
		TokenID: 1, Model: "sd_2.0_special_1080p_with_video_ref", Mode: VideoModeImageToVideo,
		Prompt: "Continue the movement", ReferenceAssets: []VideoStudioReferenceAssetInput{{
			AssetID: assets[0].ID, Role: model.VideoTaskAssetRoleReferenceVideo,
		}},
	}
	_, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)

	request.ReferenceAssets[0].AssetID = assets[1].ID
	normalized, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	require.Equal(t, profile.ID, normalized.ProfileID)
	require.Equal(t, 2, normalized.SpecificationVersion)
	require.Equal(t, map[string]any{"duration": float64(5)}, normalized.Parameters)
	require.Equal(t, model.VideoTaskAssetRoleReferenceVideo, normalized.ReferenceAssets[0].Role)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(normalized.TaskPayload, &payload))
	require.Equal(t, float64(5), payload["seconds"])
	require.Equal(t, "https://assets.invalid/reference.mp4", payload["reference_video"])
	require.NotContains(t, payload, "image")
	require.NotContains(t, payload, "images")
	metadata, ok := payload["metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(5), metadata["seconds"])
	require.Equal(t, "https://assets.invalid/reference.mp4", metadata["reference_video"])
	legacyVersion := *normalized
	legacyVersion.SpecificationVersion = 1
	require.NoError(t, ApplyVideoStudioEffectiveGroup(&legacyVersion, normalized.Group))
	require.NotEqual(t, legacyVersion.RequestHash, normalized.RequestHash)

	explicitDefault := request
	explicitDefault.Parameters = map[string]any{"duration": 5}
	explicitNormalized, err := NormalizeVideoStudioSubmission(
		context.Background(), db, videoSubmissionTestStore{}, 42, explicitDefault,
	)
	require.NoError(t, err)
	require.Equal(t, normalized.Parameters, explicitNormalized.Parameters)
	require.Equal(t, normalized.RequestHash, explicitNormalized.RequestHash)

	for _, duration := range []int{4, 15} {
		t.Run(fmt.Sprintf("accepts %d seconds", duration), func(t *testing.T) {
			candidate := request
			candidate.Parameters = map[string]any{"duration": duration}
			got, err := NormalizeVideoStudioSubmission(
				context.Background(), db, videoSubmissionTestStore{}, 42, candidate,
			)
			require.NoError(t, err)
			require.Equal(t, float64(duration), got.Parameters["duration"])
		})
	}

	invalidDurations := []struct {
		name  string
		value any
	}{
		{name: "below minimum", value: 3},
		{name: "above maximum", value: 16},
		{name: "fractional", value: 5.5},
		{name: "string", value: "5"},
		{name: "boolean", value: true},
	}
	for _, test := range invalidDurations {
		t.Run("rejects "+test.name, func(t *testing.T) {
			candidate := request
			candidate.Parameters = map[string]any{"duration": test.value}
			_, err := NormalizeVideoStudioSubmission(
				context.Background(), db, videoSubmissionTestStore{}, 42, candidate,
			)
			require.ErrorIs(t, err, ErrInvalidVideoParameters)
		})
	}
}

func TestNormalizeVideoStudioSubmissionProjectsSeedance25AudioDefaultAndExplicitFalse(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	minimum, maximum, step := float64(4), float64(30), float64(1)
	profile := createEnabledVideoSubmissionProfile(t, db, "seedance-2.5", VideoModelSpec{
		Version: 2,
		Modes:   []string{VideoModeTextToVideo},
		Parameters: []VideoParameterSpec{
			{
				Key: "duration", Label: "Duration", Control: VideoControlNumber, Required: true,
				Min: &minimum, Max: &maximum, Step: &step,
			},
			{
				Key: "ratio", Label: "Ratio", Control: VideoControlSelect, Required: true,
				Options: []VideoParameterOption{{Label: "16:9", Value: "16:9"}},
			},
			{
				Key: "resolution", Label: "Resolution", Control: VideoControlSelect, Required: true,
				Options: []VideoParameterOption{{Label: "720p", Value: "720p"}},
			},
			{Key: "generate_audio", Label: "Generate audio", Control: VideoControlSwitch},
		},
	}, map[string]any{
		"duration": 5, "ratio": "16:9", "resolution": "720p", "generate_audio": true,
	})

	tests := []struct {
		name       string
		parameters map[string]any
		wantAudio  bool
	}{
		{name: "profile default", wantAudio: true},
		{name: "explicit false", parameters: map[string]any{"generate_audio": false}, wantAudio: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeVideoStudioSubmission(
				context.Background(), db, videoSubmissionTestStore{}, 42,
				VideoStudioSubmissionRequest{
					TokenID: 1, Model: profile.Model, Mode: VideoModeTextToVideo,
					Prompt: "A cinematic sunrise", Parameters: test.parameters,
				},
			)
			require.NoError(t, err)
			assert.Equal(t, test.wantAudio, normalized.Parameters["generate_audio"])

			var payload map[string]any
			require.NoError(t, common.Unmarshal(normalized.TaskPayload, &payload))
			assert.Equal(t, test.wantAudio, payload["generate_audio"])
			metadata, ok := payload["metadata"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, test.wantAudio, metadata["generate_audio"])
			assert.Equal(t, payload["generate_audio"], metadata["generate_audio"])
		})
	}
}

func TestNormalizeVideoStudioSubmissionFiltersModeScopedProfileDefaults(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	durationMin, durationMax, durationStep := float64(4), float64(12), float64(1)
	strengthMin, strengthMax, strengthStep := float64(0), float64(1), float64(0.1)
	profile := createEnabledVideoSubmissionProfile(t, db, "multi-mode-model", VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo, VideoModeImageToVideo},
		Parameters: []VideoParameterSpec{
			{Key: "duration", Label: "Duration", Control: VideoControlNumber, Min: &durationMin, Max: &durationMax, Step: &durationStep},
			{Key: "strength", Label: "Strength", Control: VideoControlNumber, Modes: []string{VideoModeImageToVideo}, Min: &strengthMin, Max: &strengthMax, Step: &strengthStep},
		},
		ReferenceInputs: []VideoReferenceInputSpec{{
			Role: model.VideoTaskAssetRoleReference, RequestKey: "image", Required: true,
		}},
	}, map[string]any{"duration": 6, "strength": 0.8})
	asset := model.KKAIVideoAsset{
		OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "mode-default.png", MIMEType: "image/png",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&asset).Error)

	textRequest := VideoStudioSubmissionRequest{
		TokenID: 1, Model: profile.Model, Mode: VideoModeTextToVideo, Prompt: "Text mode",
	}
	text, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, textRequest)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"duration": float64(6)}, text.Parameters)

	imageRequest := VideoStudioSubmissionRequest{
		TokenID: 1, Model: profile.Model, Mode: VideoModeImageToVideo, Prompt: "Image mode",
		ReferenceAssets: []VideoStudioReferenceAssetInput{{AssetID: asset.ID, Role: model.VideoTaskAssetRoleReference}},
	}
	image, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, imageRequest)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"duration": float64(6), "strength": float64(0.8)}, image.Parameters)

	textRequest.Parameters = map[string]any{"strength": 0.5}
	_, err = NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, textRequest)
	require.ErrorIs(t, err, ErrInvalidVideoParameters)

	require.NoError(t, db.Model(&profile).Update("default_parameters", `{"unknown":true}`).Error)
	textRequest.Parameters = nil
	_, err = NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, textRequest)
	require.ErrorIs(t, err, ErrInvalidVideoParameters)
}

func TestNormalizeVideoStudioSubmissionUsesPersistedSampleReferenceMapping(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, sample, assets := createVideoSubmissionFixture(t, db)
	reversedSpecification := VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeFirstLastFrame},
		ReferenceInputs: []VideoReferenceInputSpec{
			{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_source", Required: true},
			{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_source", Required: true},
		},
	}
	specificationJSON, err := common.Marshal(reversedSpecification)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoModelProfile{}).Where("id = ?", profile.ID).
		Update("specification", string(specificationJSON)).Error)
	referenceSnapshots, err := common.Marshal([]VideoSampleReferenceSnapshot{
		{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_source"},
		{AssetID: assets[0].ID, Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_source"},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoSample{}).Where("id = ?", sample.ID).
		Update("reference_asset_ids", string(referenceSnapshots)).Error)

	normalized, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, VideoStudioSubmissionRequest{
		SampleID: &sample.ID,
	})
	require.NoError(t, err)
	require.Equal(t, []string{model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame}, []string{
		normalized.ReferenceAssets[0].Role,
		normalized.ReferenceAssets[1].Role,
	})
	require.Equal(t, "https://assets.invalid/first.jpg", normalized.ReferenceAssets[0].SignedURL)
	require.Equal(t, "first_source", normalized.ReferenceAssets[0].RequestKey)
	require.Equal(t, "https://assets.invalid/last.jpg", normalized.ReferenceAssets[1].SignedURL)
	require.Equal(t, "last_source", normalized.ReferenceAssets[1].RequestKey)

	payload := map[string]any{}
	require.NoError(t, common.Unmarshal(normalized.TaskPayload, &payload))
	require.Equal(t, "https://assets.invalid/last.jpg", payload["last_source"])
	require.Equal(t, "https://assets.invalid/first.jpg", payload["first_source"])
}

func TestNormalizeVideoStudioSubmissionCanonicalizesReferenceOrder(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, _, assets := createVideoSubmissionFixture(t, db)
	base := VideoStudioSubmissionRequest{
		Model: profile.Model, Mode: VideoModeFirstLastFrame, Prompt: "A controlled camera move",
		ReferenceAssets: []VideoStudioReferenceAssetInput{
			{AssetID: assets[0].ID, Role: model.VideoTaskAssetRoleFirstFrame},
			{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleLastFrame},
		},
	}
	first, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, base)
	require.NoError(t, err)
	base.ReferenceAssets[0], base.ReferenceAssets[1] = base.ReferenceAssets[1], base.ReferenceAssets[0]
	second, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, base)
	require.NoError(t, err)

	require.Equal(t, first.RequestHash, second.RequestHash)
	require.Equal(t, model.VideoTaskAssetRoleFirstFrame, second.ReferenceAssets[0].Role)
	require.Equal(t, model.VideoTaskAssetRoleLastFrame, second.ReferenceAssets[1].Role)
}

func TestNormalizeVideoStudioSubmissionRejectsUnpublishedCatalogReference(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, _, assets := createVideoSubmissionFixture(t, db)
	hidden := model.KKAIVideoAsset{
		OwnerUserID: 1, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "hidden.jpg", MIMEType: "image/jpeg",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&hidden).Error)

	_, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, VideoStudioSubmissionRequest{
		Model: profile.Model, Mode: VideoModeFirstLastFrame, Prompt: "A controlled camera move",
		ReferenceAssets: []VideoStudioReferenceAssetInput{
			{AssetID: hidden.ID, Role: model.VideoTaskAssetRoleFirstFrame},
			{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleLastFrame},
		},
	})
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)
}

func TestVideoStudioIdempotencyFingerprintBindsCreativeRequestOnly(t *testing.T) {
	assetID := int64(91)
	base := VideoStudioSubmissionRequest{
		TokenID: 101, Model: " video-model-v1 ", Group: " default ", Mode: VideoModeImageToVideo,
		Prompt: " A controlled camera move ", Parameters: map[string]any{"duration": float64(5)},
		ReferenceAssets: []VideoStudioReferenceAssetInput{{AssetID: assetID, Role: model.VideoTaskAssetRoleReference}},
		MaxQuota:        intPointer(1000), QuoteHash: strings.Repeat("a", 64), QuoteExpiresAt: 100,
	}
	first, err := VideoStudioIdempotencyFingerprint(base)
	require.NoError(t, err)

	refreshedQuote := base
	refreshedQuote.MaxQuota = intPointer(2000)
	refreshedQuote.QuoteHash = strings.Repeat("b", 64)
	refreshedQuote.QuoteExpiresAt = 200
	second, err := VideoStudioIdempotencyFingerprint(refreshedQuote)
	require.NoError(t, err)
	require.Equal(t, first, second)

	changedPrompt := base
	changedPrompt.Prompt = "A different camera move"
	changedPromptHash, err := VideoStudioIdempotencyFingerprint(changedPrompt)
	require.NoError(t, err)
	require.NotEqual(t, first, changedPromptHash)

	changedParameters := base
	changedParameters.Parameters = map[string]any{"duration": float64(6)}
	changedParametersHash, err := VideoStudioIdempotencyFingerprint(changedParameters)
	require.NoError(t, err)
	require.NotEqual(t, first, changedParametersHash)

	changedAssets := base
	changedAssets.ReferenceAssets = []VideoStudioReferenceAssetInput{{AssetID: assetID + 1, Role: model.VideoTaskAssetRoleReference}}
	changedAssetsHash, err := VideoStudioIdempotencyFingerprint(changedAssets)
	require.NoError(t, err)
	require.NotEqual(t, first, changedAssetsHash)

	changedToken := base
	changedToken.TokenID++
	changedTokenHash, err := VideoStudioIdempotencyFingerprint(changedToken)
	require.NoError(t, err)
	require.NotEqual(t, first, changedTokenHash)
}

func TestVideoStudioQuoteSignatureExpiresAndCannotBeExtended(t *testing.T) {
	now := time.Unix(1_785_070_000, 0)
	normalized := &NormalizedVideoStudioSubmission{UserID: 7, RequestHash: strings.Repeat("a", 64)}
	quote := newVideoStudioQuoteAt(normalized, 1000, nil, now)
	normalized.QuoteHash = quote.RequestHash
	normalized.QuoteExpiresAt = quote.ExpiresAt

	require.NoError(t, ValidateVideoStudioQuote(normalized, now.Add(4*time.Minute)))
	require.ErrorIs(t, ValidateVideoStudioQuote(normalized, now.Add(6*time.Minute)), ErrVideoStudioQuoteExpired)

	normalized.QuoteExpiresAt++
	require.ErrorIs(t, ValidateVideoStudioQuote(normalized, now), ErrVideoStudioQuoteMismatch)
}

func TestVideoStudioQuoteRequestHashBindsSelectedToken(t *testing.T) {
	specification := VideoModelSpec{Version: 1, Modes: []string{VideoModeTextToVideo}}
	first := &NormalizedVideoStudioSubmission{
		UserID: 42, TokenID: 101, ProfileID: 1, SpecificationVersion: 1,
		Model: "video-model-v1", Mode: VideoModeTextToVideo, Prompt: "A controlled camera move",
		Parameters: map[string]any{}, specification: specification,
	}
	require.NoError(t, ApplyVideoStudioEffectiveGroup(first, VideoStudioTokenGroup))
	second := *first
	second.TokenID = 202
	require.NoError(t, ApplyVideoStudioEffectiveGroup(&second, VideoStudioTokenGroup))

	require.NotEqual(t, first.RequestHash, second.RequestHash)
}

func TestValidateVideoModelSpecRequiresImageMappings(t *testing.T) {
	tests := []VideoModelSpec{
		{
			Version: 1, Modes: []string{VideoModeImageToVideo},
			ReferenceInputs: []VideoReferenceInputSpec{{
				Role: model.VideoTaskAssetRoleReference, RequestKey: "image", Required: false,
			}},
		},
		{
			Version: 1, Modes: []string{VideoModeFirstLastFrame},
			ReferenceInputs: []VideoReferenceInputSpec{
				{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
				{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: false},
			},
		},
	}
	for _, specification := range tests {
		require.ErrorIs(t, ValidateVideoModelSpec(specification, map[string]any{}), ErrInvalidVideoModelSpec)
	}
}

func TestValidateVideoModelSpecRejectsRequestKeyCollisions(t *testing.T) {
	tests := []struct {
		name          string
		parameters    []VideoParameterSpec
		referenceKeys []string
	}{
		{
			name: "parameter overwrites model envelope",
			parameters: []VideoParameterSpec{{
				Key: "variant", RequestKey: "model", Label: "Variant", Control: VideoControlSelect,
				Options: []VideoParameterOption{{Label: "A", Value: "a"}},
			}},
			referenceKeys: []string{"first_image", "last_image"},
		},
		{
			name: "parameter overwrites image compatibility alias",
			parameters: []VideoParameterSpec{{
				Key: "variant", RequestKey: "image", Label: "Variant", Control: VideoControlSelect,
				Options: []VideoParameterOption{{Label: "A", Value: "a"}},
			}},
			referenceKeys: []string{"first_image", "last_image"},
		},
		{name: "references share request key", referenceKeys: []string{"frame", "frame"}},
		{name: "reference overwrites metadata envelope", referenceKeys: []string{"metadata", "last_image"}},
		{name: "reference overwrites images alias", referenceKeys: []string{"images", "last_image"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			specification := VideoModelSpec{
				Version: 1, Modes: []string{VideoModeFirstLastFrame}, Parameters: test.parameters,
				ReferenceInputs: []VideoReferenceInputSpec{
					{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: test.referenceKeys[0], Required: true},
					{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: test.referenceKeys[1], Required: true},
				},
			}

			require.ErrorIs(t, ValidateVideoModelSpec(specification, map[string]any{}), ErrInvalidVideoModelSpec)
		})
	}
}

func TestValidateVideoModelSpecAllowsSingleReferenceImageAlias(t *testing.T) {
	specification := VideoModelSpec{
		Version: 1, Modes: []string{VideoModeImageToVideo},
		ReferenceInputs: []VideoReferenceInputSpec{{
			Role: model.VideoTaskAssetRoleReference, RequestKey: "image", Required: true,
		}},
	}

	require.NoError(t, ValidateVideoModelSpec(specification, map[string]any{}))
}

func TestVideoStudioRequestHashBindsEffectiveGroupAndSpecificationMapping(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile, _, assets := createVideoSubmissionFixture(t, db)
	request := VideoStudioSubmissionRequest{
		Model: profile.Model, Mode: VideoModeFirstLastFrame, Prompt: "A controlled camera move",
		ReferenceAssets: []VideoStudioReferenceAssetInput{
			{AssetID: assets[0].ID, Role: model.VideoTaskAssetRoleFirstFrame},
			{AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleLastFrame},
		},
	}

	first, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	require.NoError(t, ApplyVideoStudioEffectiveGroup(first, "default"))
	firstHash := first.RequestHash

	require.NoError(t, ApplyVideoStudioEffectiveGroup(first, "vip"))
	require.NotEqual(t, firstHash, first.RequestHash)

	updatedSpecification := VideoModelSpec{
		Version: 2,
		Modes:   []string{VideoModeFirstLastFrame},
		ReferenceInputs: []VideoReferenceInputSpec{
			{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "start_image", Required: true},
			{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "end_image", Required: true},
		},
	}
	encoded, err := common.Marshal(updatedSpecification)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoModelProfile{}).Where("id = ?", profile.ID).Updates(map[string]any{
		"specification_version": 2,
		"specification":         string(encoded),
	}).Error)

	second, err := NormalizeVideoStudioSubmission(context.Background(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	require.NoError(t, ApplyVideoStudioEffectiveGroup(second, "vip"))
	require.NotEqual(t, first.RequestHash, second.RequestHash)
}

func TestValidateVideoModelSpecRejectsUnsafeBillingMultiplierBounds(t *testing.T) {
	tests := []VideoParameterSpec{
		{
			Key: "duration", Label: "Duration", Control: VideoControlNumber,
			Min: floatPointer(1), Max: floatPointer(relaycommon.MaxTaskDurationSeconds + 1), Step: floatPointer(1),
		},
		{
			Key: "copies", RequestKey: "count", Label: "Count", Control: VideoControlNumber,
			Min: floatPointer(1), Max: floatPointer(dto.MaxImageN + 1), Step: floatPointer(1),
		},
		{
			Key: "batch", Label: "Batch", Control: VideoControlSelect,
			Options: []VideoParameterOption{{Label: "Unsafe", Value: dto.MaxImageN + 1}},
		},
	}

	for _, parameter := range tests {
		specification := VideoModelSpec{
			Version:    1,
			Modes:      []string{VideoModeTextToVideo},
			Parameters: []VideoParameterSpec{parameter},
		}
		require.ErrorIs(t, ValidateVideoModelSpec(specification, map[string]any{}), ErrInvalidVideoModelSpec)
	}
}

func TestValidateVideoParametersDefendsAgainstPersistedUnsafeMultiplierSpec(t *testing.T) {
	specification := VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeTextToVideo},
		Parameters: []VideoParameterSpec{{
			Key: "duration", Label: "Duration", Control: VideoControlNumber,
			Min: floatPointer(1), Max: floatPointer(999999), Step: floatPointer(1),
		}},
	}

	_, err := ValidateVideoParameters(specification, VideoModeTextToVideo, map[string]any{
		"duration": float64(relaycommon.MaxTaskDurationSeconds + 1),
	}, false)
	require.ErrorIs(t, err, ErrInvalidVideoParameters)
}

func floatPointer(value int) *float64 {
	converted := float64(value)
	return &converted
}

func intPointer(value int) *int {
	return &value
}
