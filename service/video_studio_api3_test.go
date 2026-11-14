package service

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioAPI3KeepsOldQuoteAndIdempotencyAcrossProfileUpgrade(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.KKAIVideoTaskAsset{}, &model.Task{}, &model.KKAIIdempotencyKey{}))
	spec := VideoModelSpec{Version: 1, Modes: []string{VideoModeTextToVideo, VideoModeImageToVideo}, ReferenceInputs: []VideoReferenceInputSpec{{Role: "reference", RequestKey: "reference_image", Required: true}}}
	profile := createEnabledVideoSubmissionProfile(t, db, "sd_2.5_special_720p", spec, map[string]any{})
	require.NoError(t, db.Create(&model.Ability{Group: VideoStudioTokenGroup, Model: profile.Model, ChannelId: 1, Enabled: true}).Error)
	asset := model.KKAIVideoAsset{ID: 11, OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "old.png", MIMEType: "image/png"}
	require.NoError(t, db.Create(&asset).Error)
	request := VideoStudioSubmissionRequest{LegacyAPI3: true, TokenID: 7, Model: profile.Model, Group: "Seedance 视频", Mode: VideoModeImageToVideo, Prompt: "move", ReferenceAssets: []VideoStudioReferenceAssetInput{{AssetID: 11, Role: "reference"}}}
	before, err := NormalizeVideoStudioSubmission(t.Context(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	// This exact JSON is the accepted API3 semantic wire contract, independently
	// of API4's version/position fields and the current implementation helpers.
	oldCanonical := fmt.Sprintf(`{"token_id":7,"profile_id":%d,"specification_version":1,"model":"sd_2.5_special_720p","group":"Seedance 视频","mode":"image_to_video","prompt":"move","parameters":[],"references":[{"asset_id":11,"role":"reference","request_key":"reference_image"}]}`, profile.ID)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(oldCanonical))), before.RequestHash)
	oldFingerprint := `{"token_id":7,"model":"sd_2.5_special_720p","group":"Seedance 视频","mode":"image_to_video","prompt":"move","parameters":{},"references":[{"asset_id":11,"role":"reference"}]}`
	fingerprint, err := VideoStudioIdempotencyFingerprint(request)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(oldFingerprint))), fingerprint)
	now := time.Now()
	quote := newVideoStudioQuoteAt(before, 10, nil, now)
	reservation, err := ReserveIdempotencyKey(t.Context(), db, IdempotencyReservationRequest{UserID: 42, Operation: model.VideoIdempotencyOperationSubmit, Key: "old-tab", RequestHash: fingerprint})
	require.NoError(t, err)
	require.NoError(t, BindIdempotencyResource(t.Context(), db, reservation.Record.ID, model.VideoIdempotencyResourceTask, "old-task"))
	updated, err := UpdateVideoModelProfile(t.Context(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: profile.DisplayName, Enabled: true, Specification: VideoModelSpec{Version: 2, Modes: []string{VideoModeTextToVideo, VideoModeReferenceToVideo}, ReferencePolicy: videoTestReferencePolicy()}, DefaultParameters: map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.SpecificationVersion)
	// Editing presentation metadata must not require another spec bump just
	// because the stored document now also contains the frozen API3 snapshot.
	_, err = UpdateVideoModelProfile(t.Context(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: "New display name", Enabled: true, Specification: updated.Specification, DefaultParameters: updated.DefaultParameters})
	require.NoError(t, err)
	_, err = UpdateVideoModelProfileAPI3(t.Context(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: "Legacy display name", Enabled: true, Specification: spec, DefaultParameters: map[string]any{}})
	require.NoError(t, err)
	currentProfile, err := GetVideoModelProfileByID(t.Context(), db, profile.ID)
	require.NoError(t, err)
	assert.Equal(t, updated.Specification, currentProfile.Specification)
	after, err := NormalizeVideoStudioSubmission(t.Context(), db, videoSubmissionTestStore{}, 42, request)
	require.NoError(t, err)
	assert.Equal(t, before.RequestHash, after.RequestHash)
	after.QuoteHash, after.QuoteExpiresAt = quote.RequestHash, quote.ExpiresAt
	require.NoError(t, ValidateVideoStudioQuote(after, now))
	replay, err := ReserveIdempotencyKey(t.Context(), db, IdempotencyReservationRequest{UserID: 42, Operation: model.VideoIdempotencyOperationSubmit, Key: "old-tab", RequestHash: fingerprint})
	require.NoError(t, err)
	assert.False(t, replay.Created)
	assert.Equal(t, "old-task", replay.Record.ResourceID)
	legacy, err := GetVideoModelProfileByIDAPI3(t.Context(), db, profile.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, legacy.SpecificationVersion)
	assert.Equal(t, spec, legacy.Specification)
	// Old tabs retain metadata-free assets; the new contract never bypasses inspection.
	request.LegacyAPI3 = false
	request.SpecificationVersion = 2
	request.Mode = VideoModeReferenceToVideo
	request.ReferenceAssets = []VideoStudioReferenceAssetInput{{AssetID: 11, Role: "reference_image", Position: intPointer(0)}}
	_, err = NormalizeVideoStudioSubmission(t.Context(), db, videoSubmissionTestStore{}, 42, request)
	require.Error(t, err)
}

func TestVideoStudioAPI3CatalogPreservesSampleShapeAndReuse(t *testing.T) {
	db := newVideoSubmissionTestDB(t)
	profile := createEnabledVideoSubmissionProfile(t, db, "legacy-sample-model", VideoModelSpec{Version: 3, Modes: []string{VideoModeImageToVideo}, ReferenceInputs: []VideoReferenceInputSpec{{Role: "reference", RequestKey: "image", Required: true}}}, map[string]any{})
	ref := model.KKAIVideoAsset{OwnerUserID: 42, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "ref.png", MIMEType: "image/png"}
	video := model.KKAIVideoAsset{Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "result.mp4", MIMEType: "video/mp4"}
	require.NoError(t, db.Create(&ref).Error)
	require.NoError(t, db.Create(&video).Error)
	refs := fmt.Sprintf(`[{"asset_id":%d,"role":"reference","request_key":"image"}]`, ref.ID)
	sample := model.KKAIVideoSample{ModelProfileID: profile.ID, Title: "old sample", Prompt: "move", Mode: VideoModeImageToVideo, ModelVersion: 3, Parameters: `{}`, ReferenceAssetIDs: refs, VideoAssetID: video.ID, AspectRatio: 1, Category: model.VideoSampleCategoryOther, Status: model.VideoSampleStatusPublished}
	require.NoError(t, db.Create(&sample).Error)
	page, err := ListVideoSamplesAPI3(t.Context(), db, "", "", "", 20, false, []string{profile.Model})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	body, err := common.Marshal(page.Items[0])
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, common.Unmarshal(body, &view))
	assert.Equal(t, []any{float64(ref.ID)}, view["reference_asset_ids"])
	assert.Equal(t, []any{fmt.Sprintf("/api/video-studio/assets/%d/content", ref.ID)}, view["reference_content_urls"])
	normalized, err := NormalizeVideoStudioSubmission(t.Context(), db, videoSubmissionTestStore{}, 42, VideoStudioSubmissionRequest{LegacyAPI3: true, TokenID: 7, SampleID: &sample.ID})
	require.NoError(t, err)
	assert.Equal(t, sample.Prompt, normalized.Prompt)
	require.Len(t, normalized.ReferenceAssets, 1)
	assert.Equal(t, ref.ID, normalized.ReferenceAssets[0].Asset.ID)
	current, err := ListVideoSamples(t.Context(), db, "", "", "", 20, false, []string{profile.Model})
	require.NoError(t, err)
	assert.Empty(t, current.Items)
}

func TestVideoStudioAPI3UploadKeepsOldVideoLimitWithoutAdmittingAudio(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	request := VideoAssetUploadRequest{Purpose: model.VideoUploadPurposeReferenceVideo, Filename: "reference.mp4", MIMEType: "video/mp4", SizeBytes: 60_000_000}
	upload, err := CreateVideoAssetUpload(WithVideoStudioAPI3(t.Context()), db, store, 42, false, request)
	require.NoError(t, err)
	assert.Equal(t, upload.UploadLimits.ArchiveMaxBytes, upload.MaxSizeBytes)
	_, err = CreateVideoAssetUpload(t.Context(), db, store, 42, false, request)
	require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
	_, err = CreateVideoAssetUpload(WithVideoStudioAPI3(t.Context()), db, store, 42, false, VideoAssetUploadRequest{Purpose: model.VideoUploadPurposeReferenceAudio, Filename: "voice.mp3", MIMEType: "audio/mpeg", SizeBytes: 1024})
	require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
}

func TestVideoStudioAPI3SamplesRetainSnapshotVersionAndRequireAPI4Copy(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	spec := VideoModelSpec{Version: 1, Modes: []string{VideoModeTextToVideo, VideoModeImageToVideo}, ReferenceInputs: []VideoReferenceInputSpec{{Role: "reference", RequestKey: "reference_image", Required: true}}}
	profile := createEnabledVideoSubmissionProfile(t, db, "sd_2.5_special_720p", spec, map[string]any{})
	require.NoError(t, db.Create(&model.Ability{Group: VideoStudioTokenGroup, Model: profile.Model, ChannelId: 1, Enabled: true}).Error)
	ref := model.KKAIVideoAsset{OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "legacy-ref.png", MIMEType: "image/png"}
	video := model.KKAIVideoAsset{OwnerUserID: 42, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "sample.mp4", MIMEType: "video/mp4", PosterObjectKey: "poster.jpg", PreviewObjectKey: "preview.mp4"}
	require.NoError(t, db.Create(&ref).Error)
	require.NoError(t, db.Create(&video).Error)
	legacyInput := VideoSampleInputAPI3{ModelProfileID: profile.ID, Title: "Old sample", Prompt: "move", Mode: VideoModeImageToVideo, Parameters: map[string]any{}, ReferenceAssetIDs: []int64{ref.ID}, VideoAssetID: video.ID, AspectRatio: 1, Status: model.VideoSampleStatusPublished}
	legacy, err := CreateVideoSampleAPI3(t.Context(), db, 42, legacyInput)
	require.NoError(t, err)
	_, err = UpdateVideoModelProfile(t.Context(), db, profile.ID, VideoModelProfileInput{Model: profile.Model, DisplayName: profile.DisplayName, Enabled: true, Specification: VideoModelSpec{Version: 2, Modes: []string{VideoModeTextToVideo, VideoModeReferenceToVideo}, ReferencePolicy: videoTestReferencePolicy()}, DefaultParameters: map[string]any{}})
	require.NoError(t, err)
	legacyInput.Title = "Edited from old tab"
	updated, err := UpdateVideoSampleAPI3(t.Context(), db, legacy.ID, 42, legacyInput)
	require.NoError(t, err)
	assert.Equal(t, 1, updated.ModelVersion)
	created, err := CreateVideoSampleAPI3(t.Context(), db, 42, legacyInput)
	require.NoError(t, err)
	assert.Equal(t, 1, created.ModelVersion)

	var before model.KKAIVideoSample
	require.NoError(t, db.First(&before, legacy.ID).Error)
	var eventsBefore int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&eventsBefore).Error)
	currentInput := VideoSampleInput{ModelProfileID: profile.ID, Title: "New format sample", Prompt: "new prompt", Mode: VideoModeTextToVideo, Parameters: map[string]any{}, VideoAssetID: video.ID, AspectRatio: 1, Status: model.VideoSampleStatusPublished}
	_, err = UpdateVideoSample(t.Context(), db, legacy.ID, 42, currentInput)
	require.ErrorIs(t, err, ErrVideoSampleRequiresCopy)
	var after model.KKAIVideoSample
	require.NoError(t, db.First(&after, legacy.ID).Error)
	assert.Equal(t, before, after)
	var eventsAfter int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&eventsAfter).Error)
	assert.Equal(t, eventsBefore, eventsAfter)
	require.NoError(t, db.First(&ref, ref.ID).Error)
	assert.Equal(t, model.VideoAssetStateReady, ref.State)

	copy, err := CreateVideoSample(t.Context(), db, 42, currentInput)
	require.NoError(t, err)
	assert.NotEqual(t, legacy.ID, copy.ID)
	assert.Equal(t, 2, copy.ModelVersion)
	reused, err := NormalizeVideoStudioSubmission(t.Context(), db, videoSubmissionTestStore{}, 42, VideoStudioSubmissionRequest{LegacyAPI3: true, TokenID: 7, SampleID: &legacy.ID})
	require.NoError(t, err)
	assert.Equal(t, 1, reused.SpecificationVersion)
	require.Len(t, reused.ReferenceAssets, 1)
	assert.Equal(t, ref.ID, reused.ReferenceAssets[0].Asset.ID)
}
