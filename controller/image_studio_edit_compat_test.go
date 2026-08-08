package controller

import (
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareImageStudioEditQuoteAcceptsLegacyReferenceMetadata(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	reference := service.ImageStudioReferenceMetadata{
		SHA256: strings.Repeat("a", 64), SizeBytes: 1234,
	}
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit a lighthouse",
		Reference: &reference,
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits/quote", body)

	PrepareImageStudioRequest(ctx)

	require.False(t, ctx.IsAborted(), recorder.Body.String())
	normalized, ok := imageStudioNormalizedSubmission(ctx)
	require.True(t, ok)
	assert.Equal(t, []service.ImageStudioReferenceMetadata{reference}, normalized.References)
}

func TestParseImageStudioEditSubmissionAcceptsLegacyReferenceAndRejectsAmbiguousMetadata(t *testing.T) {
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", t.TempDir())
	imageBytes := imageStudioEditTestPNG(t, color.RGBA{G: 255, A: 255})
	reference := imageStudioEditTestReferences([][]byte{imageBytes})[0]
	settings := image_studio_setting.Get()

	legacyJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: 4, Model: service.ImageStudioEditModel, Prompt: "edit",
		QuoteToken: "quote-token", Reference: &reference,
	})
	require.NoError(t, err)
	body, contentType := imageStudioEditMultipartBody(t, legacyJSON, [][]byte{imageBytes}, false)
	ctx, _ := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", body)
	ctx.Request.Header.Set("Content-Type", contentType)
	defer common.CleanupBodyStorage(ctx)

	request, archives, err := parseImageStudioEditSubmission(
		ctx, settings.MaxReferenceBytes, settings.MaxReferenceTotalBytes, settings.MaxPixels,
	)
	require.NoError(t, err)
	defer func() {
		for _, archive := range archives {
			archive.Remove()
		}
	}()
	assert.Nil(t, request.Reference)
	assert.Equal(t, []service.ImageStudioReferenceMetadata{reference}, request.References)
	require.Len(t, archives, 1)

	ambiguousJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: 4, Model: service.ImageStudioEditModel, Prompt: "edit",
		QuoteToken: "quote-token", Reference: &reference,
		References: []service.ImageStudioReferenceMetadata{reference},
	})
	require.NoError(t, err)
	ambiguousBody, ambiguousContentType := imageStudioEditMultipartBody(t, ambiguousJSON, [][]byte{imageBytes}, false)
	ambiguousCtx, _ := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", ambiguousBody)
	ambiguousCtx.Request.Header.Set("Content-Type", ambiguousContentType)
	defer common.CleanupBodyStorage(ambiguousCtx)

	_, _, err = parseImageStudioEditSubmission(
		ambiguousCtx, settings.MaxReferenceBytes, settings.MaxReferenceTotalBytes, settings.MaxPixels,
	)
	assert.ErrorIs(t, err, service.ErrInvalidImageStudioSubmission)
}
