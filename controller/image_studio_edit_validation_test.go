package controller

import (
	"image/color"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImageStudioEditSubmissionCleansArchivesOnValidationFailure(t *testing.T) {
	first := imageStudioEditTestPNG(t, color.RGBA{R: 255, A: 255})
	second := imageStudioEditTestPNG(t, color.RGBA{B: 255, A: 255})
	wide := imageStudioEditTestPNGSize(t, 2, 1, color.RGBA{G: 255, A: 255})

	tests := []struct {
		name        string
		images      [][]byte
		mimes       []string
		mutate      func([]service.ImageStudioReferenceMetadata)
		maxBytes    int64
		maxTotal    int64
		maxPixels   int64
		expectedErr error
	}{
		{
			name: "hash mismatch after two ingests", images: [][]byte{first, second},
			mimes: []string{"image/png", "image/png"},
			mutate: func(references []service.ImageStudioReferenceMetadata) {
				references[1].SHA256 = strings.Repeat("f", 64)
			},
			maxBytes: 1 << 20, maxTotal: 2 << 20, maxPixels: 10,
			expectedErr: service.ErrInvalidImageStudioSubmission,
		},
		{
			name: "declared mime mismatch after first ingest", images: [][]byte{first, second},
			mimes:    []string{"image/png", "image/jpeg"},
			maxBytes: 1 << 20, maxTotal: 2 << 20, maxPixels: 10,
			expectedErr: service.ErrImageArchiveMIMERejected,
		},
		{
			name: "pixel limit after first ingest", images: [][]byte{first, wide},
			mimes:    []string{"image/png", "image/png"},
			maxBytes: 1 << 20, maxTotal: 2 << 20, maxPixels: 1,
			expectedErr: service.ErrImageArchivePixelsExceeded,
		},
		{
			name: "aggregate bytes after first ingest", images: [][]byte{first, second},
			mimes:    []string{"image/png", "image/png"},
			maxBytes: int64(max(len(first), len(second))),
			maxTotal: int64(len(first) + len(second) - 1), maxPixels: 10,
			expectedErr: service.ErrImageArchiveTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
			references := imageStudioEditTestReferences(test.images)
			if test.mutate != nil {
				test.mutate(references)
			}
			requestJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
				TokenID: 1, Model: service.ImageStudioEditModel, Prompt: "edit", References: references,
			})
			require.NoError(t, err)
			body, contentType := imageStudioEditMultipartBodyWithMIMEs(t, requestJSON, test.images, test.mimes)
			ctx, _ := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", body)
			ctx.Request.Header.Set("Content-Type", contentType)

			_, archives, err := parseImageStudioEditSubmission(ctx, test.maxBytes, test.maxTotal, test.maxPixels)
			assert.ErrorIs(t, err, test.expectedErr)
			assert.Empty(t, archives)
			common.CleanupBodyStorage(ctx)
			entries, readErr := os.ReadDir(tempDir)
			require.NoError(t, readErr)
			assert.Empty(t, entries)
		})
	}
}
