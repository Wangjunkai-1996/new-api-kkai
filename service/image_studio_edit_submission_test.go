package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageStudioGenerationRequestHashRemainsBackwardCompatible(t *testing.T) {
	normalized := &NormalizedImageStudioSubmission{
		TokenID: 4, ProfileID: 8, SpecificationVersion: 3,
		Model: "gpt-image-2", Prompt: "a lighthouse",
		Parameters: map[string]any{"size": "1024x1024", "count": 2},
		Mode:       ImageStudioModeGeneration,
	}
	specification := ImageModelSpec{Parameters: []ImageParameterSpec{
		{Key: "size", RequestKey: "size"},
		{Key: "count", RequestKey: "n"},
	}}

	requestHash, err := imageStudioRequestHash(normalized, specification)
	require.NoError(t, err)
	legacyCanonical := []byte(`{"token_id":4,"profile_id":8,"specification_version":3,"model":"gpt-image-2","prompt":"a lighthouse","parameters":[{"key":"size","request_key":"size","value":"1024x1024"},{"key":"count","request_key":"n","value":2}]}`)
	digest := sha256.Sum256(legacyCanonical)
	assert.Equal(t, hex.EncodeToString(digest[:]), requestHash)
}

func TestNormalizeImageStudioEditRequiresExactModelAndBindsReference(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	require.NoError(t, db.Model(&profile).Update("model", ImageStudioEditModel).Error)
	profile.Model = ImageStudioEditModel
	reference := &ImageStudioReferenceMetadata{
		SHA256: strings.Repeat("A", 64), SizeBytes: 1234,
	}
	request := ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit the lighthouse",
		Mode: ImageStudioModeEdit, Reference: reference,
	}

	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, request)
	require.NoError(t, err)
	assert.Equal(t, ImageStudioModeEdit, normalized.Mode)
	require.NotNil(t, normalized.Reference)
	assert.Equal(t, strings.Repeat("a", 64), normalized.Reference.SHA256)
	assert.EqualValues(t, 1234, normalized.Reference.SizeBytes)
	assert.NotSame(t, reference, normalized.Reference)

	generation, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit the lighthouse",
	})
	require.NoError(t, err)
	assert.NotEqual(t, generation.RequestHash, normalized.RequestHash)

	changed := request
	changed.Reference = &ImageStudioReferenceMetadata{
		SHA256: strings.Repeat("b", 64), SizeBytes: 1234,
	}
	changedNormalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, changed)
	require.NoError(t, err)
	assert.NotEqual(t, normalized.RequestHash, changedNormalized.RequestHash)

	invalid := []ImageStudioSubmissionRequest{
		{TokenID: 4, Model: "gpt-image-2k", Prompt: "edit", Mode: ImageStudioModeEdit, Reference: reference},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit, Reference: &ImageStudioReferenceMetadata{SHA256: "invalid", SizeBytes: 1}},
		{TokenID: 4, Model: profile.Model, Prompt: "edit", Mode: ImageStudioModeEdit, Reference: &ImageStudioReferenceMetadata{SHA256: strings.Repeat("a", 64)}},
		{TokenID: 4, Model: profile.Model, Prompt: "generate", Reference: reference},
	}
	for _, candidate := range invalid {
		_, err := NormalizeImageStudioSubmission(context.Background(), db, 7, candidate)
		assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)
	}
}

func TestImageStudioEditQuoteRejectsDifferentUploadedReference(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	require.NoError(t, db.Model(&profile).Update("model", ImageStudioEditModel).Error)
	profile.Model = ImageStudioEditModel
	quoted, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit",
		Mode:      ImageStudioModeEdit,
		Reference: &ImageStudioReferenceMetadata{SHA256: strings.Repeat("a", 64), SizeBytes: 100},
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	quote, err := newImageStudioQuoteAt(
		quoted, 300, map[string]float64{"n": 1}, imageStudioQuoteTestSnapshot(quoted), now,
	)
	require.NoError(t, err)

	uploaded, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "edit", QuoteToken: quote.QuoteToken,
		Mode:      ImageStudioModeEdit,
		Reference: &ImageStudioReferenceMetadata{SHA256: strings.Repeat("b", 64), SizeBytes: 100},
	})
	require.NoError(t, err)
	_, err = ValidateImageStudioQuote(uploaded, now.Add(time.Minute))
	assert.ErrorIs(t, err, ErrImageStudioQuoteMismatch)
}
