package image_studio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestGetNormalizesUnsafeImageStudioLimits(t *testing.T) {
	previous := imageStudioSetting
	t.Cleanup(func() { imageStudioSetting = previous })
	imageStudioSetting = Setting{
		AccessMode:                      "unexpected",
		MaxOutputBytes:                  -1,
		MaxResponseBytes:                1,
		MaxPixels:                       0,
		ThumbnailMaxPixels:              64_000_000,
		MaxImagesPerGeneration:          100,
		SignedURLSeconds:                1,
		SubmissionTimeoutSecs:           1,
		MaxConcurrentSubmissions:        100,
		MaxConcurrentSubmissionsPerUser: 100,
	}

	setting := Get()
	assert.Equal(t, AccessModeOff, setting.AccessMode)
	assert.EqualValues(t, 32<<20, setting.MaxOutputBytes)
	assert.EqualValues(t, 128<<20, setting.MaxResponseBytes)
	assert.EqualValues(t, 64_000_000, setting.MaxPixels)
	assert.EqualValues(t, 20_000_000, setting.ThumbnailMaxPixels)
	assert.Equal(t, 4, setting.MaxImagesPerGeneration)
	assert.Equal(t, 600, setting.SignedURLSeconds)
	assert.Equal(t, 300, setting.SubmissionTimeoutSecs)
	assert.Equal(t, 2, setting.MaxConcurrentSubmissions)
	assert.Equal(t, 1, setting.MaxConcurrentSubmissionsPerUser)
	assert.False(t, CanAccess(common.RoleRootUser))
}

func TestGetCapsPerUserImageStudioCapacityAtGlobalLimit(t *testing.T) {
	previous := imageStudioSetting
	t.Cleanup(func() { imageStudioSetting = previous })
	imageStudioSetting.MaxConcurrentSubmissions = 2
	imageStudioSetting.MaxConcurrentSubmissionsPerUser = 4

	setting := Get()
	assert.Equal(t, 2, setting.MaxConcurrentSubmissions)
	assert.Equal(t, 2, setting.MaxConcurrentSubmissionsPerUser)
}

func TestCanAccessHonorsConfiguredMode(t *testing.T) {
	previous := imageStudioSetting
	t.Cleanup(func() { imageStudioSetting = previous })

	imageStudioSetting.AccessMode = AccessModeAdmin
	assert.False(t, CanAccess(common.RoleCommonUser))
	assert.True(t, CanAccess(common.RoleAdminUser))

	imageStudioSetting.AccessMode = AccessModeAll
	assert.True(t, CanAccess(common.RoleCommonUser))
}
