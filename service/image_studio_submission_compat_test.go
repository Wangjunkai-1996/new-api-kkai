package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageStudioSingleReferenceRequestHashRemainsBackwardCompatible(t *testing.T) {
	reference := ImageStudioReferenceMetadata{SHA256: strings.Repeat("a", 64), SizeBytes: 1234}
	normalized := &NormalizedImageStudioSubmission{
		TokenID: 4, ProfileID: 8, SpecificationVersion: 3,
		Model: "gpt-image-2", Prompt: "edit a lighthouse",
		Parameters: map[string]any{"size": "1024x1024", "count": 1},
		Mode:       ImageStudioModeEdit,
		References: []ImageStudioReferenceMetadata{reference},
	}
	specification := ImageModelSpec{Parameters: []ImageParameterSpec{
		{Key: "size", RequestKey: "size"},
		{Key: "count", RequestKey: "n"},
	}}

	requestHash, err := imageStudioRequestHash(normalized, specification)
	require.NoError(t, err)
	legacyCanonical := []byte(fmt.Sprintf(
		`{"token_id":4,"profile_id":8,"specification_version":3,"model":"gpt-image-2","prompt":"edit a lighthouse","parameters":[{"key":"size","request_key":"size","value":"1024x1024"},{"key":"count","request_key":"n","value":1}],"mode":"edit","reference":{"sha256":%q,"size_bytes":1234}}`,
		reference.SHA256,
	))
	digest := sha256.Sum256(legacyCanonical)
	assert.Equal(t, hex.EncodeToString(digest[:]), requestHash)
}

func TestNormalizeImageStudioReferenceFieldsAcceptsLegacyAndRejectsAmbiguousInput(t *testing.T) {
	reference := ImageStudioReferenceMetadata{SHA256: strings.Repeat("a", 64), SizeBytes: 1234}
	legacy := ImageStudioSubmissionRequest{Reference: &reference}

	require.NoError(t, NormalizeImageStudioReferenceFields(&legacy))
	assert.Nil(t, legacy.Reference)
	assert.Equal(t, []ImageStudioReferenceMetadata{reference}, legacy.References)
	assert.NotSame(t, &reference, &legacy.References[0])

	ambiguous := ImageStudioSubmissionRequest{
		Reference: &reference, References: []ImageStudioReferenceMetadata{reference},
	}
	assert.ErrorIs(t, NormalizeImageStudioReferenceFields(&ambiguous), ErrInvalidImageStudioSubmission)

	var decoded ImageStudioSubmissionRequest
	err := common.Unmarshal([]byte(`{"reference":null,"references":[]}`), &decoded)
	assert.ErrorIs(t, err, ErrInvalidImageStudioSubmission)
}
