package service

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validImageModelSpec() ImageModelSpec {
	minimum := 1
	maximum := 4
	return ImageModelSpec{
		Version: 1,
		Parameters: []ImageParameterSpec{
			{
				Key: "size", Label: "Size", Control: ImageControlSelect, RequestKey: "size", Required: true,
				Options: []ImageParameterOption{{Label: "Square", Value: "1024x1024"}},
			},
			{Key: "count", Label: "Count", Control: ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
			{Key: "watermark", Label: "Watermark", Control: ImageControlBoolean, RequestKey: "watermark"},
		},
	}
}

func TestValidateImageModelSpecRejectsUnsafeOrUnknownFields(t *testing.T) {
	spec := validImageModelSpec()
	require.NoError(t, ValidateImageModelSpec(spec, map[string]any{"size": "1024x1024", "count": 1}))

	unknown := spec
	unknown.Parameters = append(unknown.Parameters, ImageParameterSpec{
		Key: "raw", Label: "Raw", Control: ImageControlSelect, RequestKey: "extra_fields",
		Options: []ImageParameterOption{{Label: "Unsafe", Value: "unsafe"}},
	})
	assert.ErrorIs(t, ValidateImageModelSpec(unknown, nil), ErrInvalidImageModelSpec)

	unsafeCount := validImageModelSpec()
	*unsafeCount.Parameters[1].Max = dto.MaxImageN + 1
	assert.ErrorIs(t, ValidateImageModelSpec(unsafeCount, nil), ErrInvalidImageModelSpec)

	oversizedOption := validImageModelSpec()
	oversizedOption.Parameters[0].Options[0].Label = string(make([]byte, 129))
	assert.ErrorIs(t, ValidateImageModelSpec(oversizedOption, nil), ErrInvalidImageModelSpec)

	tooManyReferences := validImageModelSpec()
	tooManyReferences.MaxReferenceImages = MaxImageStudioReferenceImages + 1
	assert.ErrorIs(t, ValidateImageModelSpec(tooManyReferences, nil), ErrInvalidImageModelSpec)
}

func TestValidateImageModelSpecAllowsDefaultAndBoundedReferenceCounts(t *testing.T) {
	defaults := map[string]any{"size": "1024x1024"}
	defaultSingle := validImageModelSpec()
	require.NoError(t, ValidateImageModelSpec(defaultSingle, defaults))

	multiple := validImageModelSpec()
	multiple.MaxReferenceImages = MaxImageStudioReferenceImages
	require.NoError(t, ValidateImageModelSpec(multiple, defaults))
}

func TestImageStudioOutputLimitAcceptsLegacyBoundsButRejectsUnsafeMinimumDefaultsAndValues(t *testing.T) {
	legacyWide := validImageModelSpec()
	*legacyWide.Parameters[1].Max = dto.MaxImageN
	require.NoError(t, ValidateImageModelSpec(legacyWide, map[string]any{
		"size": "1024x1024", "count": MaxImageStudioOutputs,
	}))
	assert.ErrorIs(t, ValidateImageModelSpec(legacyWide, map[string]any{
		"size": "1024x1024", "count": MaxImageStudioOutputs + 1,
	}), ErrInvalidImageModelSpec)

	unsafeMinimum := validImageModelSpec()
	*unsafeMinimum.Parameters[1].Min = 2
	*unsafeMinimum.Parameters[1].Max = dto.MaxImageN
	assert.ErrorIs(t, ValidateImageModelSpec(unsafeMinimum, map[string]any{
		"size": "1024x1024",
	}), ErrInvalidImageModelSpec)

	_, err := ValidateImageParameters(legacyWide, map[string]any{
		"size": "1024x1024", "count": MaxImageStudioOutputs + 1,
	}, true)
	assert.ErrorIs(t, err, ErrInvalidImageParameters)
	_, err = BuildImageRelayRequest("image-model", "draw a cat", legacyWide, map[string]any{
		"size": "1024x1024", "count": MaxImageStudioOutputs + 1,
	})
	assert.ErrorIs(t, err, ErrInvalidImageParameters)
}

func TestValidateImageParametersRejectsUnknownFractionalAndProductOverflow(t *testing.T) {
	spec := validImageModelSpec()

	_, err := ValidateImageParameters(spec, map[string]any{"size": "1024x1024", "extra": true}, true)
	assert.ErrorIs(t, err, ErrInvalidImageParameters)
	for _, count := range []any{1, 2, MaxImageStudioOutputs} {
		_, err = ValidateImageParameters(spec, map[string]any{"size": "1024x1024", "count": count}, true)
		require.NoError(t, err)
	}
	for _, count := range []any{0, 1.5, MaxImageStudioOutputs + 1, math.MaxFloat64} {
		_, err = ValidateImageParameters(spec, map[string]any{"size": "1024x1024", "count": count}, true)
		assert.ErrorIs(t, err, ErrInvalidImageParameters)
	}
}

func TestBuildImageRelayRequestUsesOnlyValidatedFieldsAndDisablesStreaming(t *testing.T) {
	spec := validImageModelSpec()
	parameters, err := ValidateImageParameters(spec, map[string]any{
		"size": "1024x1024", "count": 2, "watermark": false,
	}, true)
	require.NoError(t, err)

	request, err := BuildImageRelayRequest("image-model", "draw a cat", spec, parameters)
	require.NoError(t, err)
	require.NotNil(t, request.N)
	assert.EqualValues(t, 2, *request.N)
	assert.Equal(t, "1024x1024", request.Size)
	require.NotNil(t, request.Watermark)
	assert.False(t, *request.Watermark)
	require.NotNil(t, request.Stream)
	assert.False(t, *request.Stream)
	assert.Empty(t, request.Extra)
}

func TestBuildImageRelayRequestRejectsMissingPrompt(t *testing.T) {
	_, err := BuildImageRelayRequest("image-model", "", ImageModelSpec{Version: 1}, map[string]any{})
	assert.True(t, errors.Is(err, ErrInvalidImageParameters))
}
