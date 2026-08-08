package image_studio_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	AccessModeOff               = "off"
	AccessModeAdmin             = "admin"
	AccessModeAll               = "all"
	MaxImagesPerGenerationLimit = 4
)

type Setting struct {
	AccessMode                      string `json:"access_mode"`
	WorkerEnabled                   bool   `json:"worker_enabled"`
	MaxReferenceBytes               int64  `json:"max_reference_bytes"`
	MaxReferenceTotalBytes          int64  `json:"max_reference_total_bytes"`
	MaxOutputBytes                  int64  `json:"max_output_bytes"`
	MaxResponseBytes                int64  `json:"max_response_bytes"`
	MaxPixels                       int64  `json:"max_pixels"`
	ThumbnailMaxPixels              int64  `json:"thumbnail_max_pixels"`
	MaxImagesPerGeneration          int    `json:"max_images_per_generation"`
	SignedURLSeconds                int    `json:"signed_url_seconds"`
	SubmissionTimeoutSecs           int    `json:"submission_timeout_seconds"`
	MaxConcurrentSubmissions        int    `json:"max_concurrent_submissions"`
	MaxConcurrentSubmissionsPerUser int    `json:"max_concurrent_submissions_per_user"`
}

var imageStudioSetting = Setting{
	AccessMode:                      AccessModeOff,
	WorkerEnabled:                   false,
	MaxReferenceBytes:               32 << 20,
	MaxReferenceTotalBytes:          64 << 20,
	MaxOutputBytes:                  32 << 20,
	MaxResponseBytes:                128 << 20,
	MaxPixels:                       64_000_000,
	ThumbnailMaxPixels:              20_000_000,
	MaxImagesPerGeneration:          MaxImagesPerGenerationLimit,
	SignedURLSeconds:                600,
	SubmissionTimeoutSecs:           300,
	MaxConcurrentSubmissions:        2,
	MaxConcurrentSubmissionsPerUser: 1,
}

func init() {
	config.GlobalConfig.Register("image_studio", &imageStudioSetting)
}

func Get() Setting {
	setting := imageStudioSetting
	switch setting.AccessMode {
	case AccessModeAdmin, AccessModeAll:
	default:
		setting.AccessMode = AccessModeOff
	}
	if setting.MaxReferenceBytes <= 0 || setting.MaxReferenceBytes > 128<<20 {
		setting.MaxReferenceBytes = 32 << 20
	}
	if setting.MaxReferenceTotalBytes < setting.MaxReferenceBytes || setting.MaxReferenceTotalBytes > 128<<20 {
		setting.MaxReferenceTotalBytes = 64 << 20
		if setting.MaxReferenceTotalBytes < setting.MaxReferenceBytes {
			setting.MaxReferenceTotalBytes = setting.MaxReferenceBytes
		}
	}
	if setting.MaxOutputBytes <= 0 || setting.MaxOutputBytes > 128<<20 {
		setting.MaxOutputBytes = 32 << 20
	}
	if setting.MaxResponseBytes < setting.MaxOutputBytes || setting.MaxResponseBytes > 512<<20 {
		setting.MaxResponseBytes = 128 << 20
	}
	if setting.MaxPixels <= 0 || setting.MaxPixels > 256_000_000 {
		setting.MaxPixels = 64_000_000
	}
	if setting.ThumbnailMaxPixels <= 0 || setting.ThumbnailMaxPixels > 32_000_000 {
		setting.ThumbnailMaxPixels = 20_000_000
	}
	if setting.MaxImagesPerGeneration < 1 || setting.MaxImagesPerGeneration > MaxImagesPerGenerationLimit {
		setting.MaxImagesPerGeneration = MaxImagesPerGenerationLimit
	}
	if setting.SignedURLSeconds < 60 || setting.SignedURLSeconds > 3600 {
		setting.SignedURLSeconds = 600
	}
	if setting.SubmissionTimeoutSecs < 30 || setting.SubmissionTimeoutSecs > 900 {
		setting.SubmissionTimeoutSecs = 300
	}
	if setting.MaxConcurrentSubmissions < 1 || setting.MaxConcurrentSubmissions > 16 {
		setting.MaxConcurrentSubmissions = 2
	}
	if setting.MaxConcurrentSubmissionsPerUser < 1 || setting.MaxConcurrentSubmissionsPerUser > 16 {
		setting.MaxConcurrentSubmissionsPerUser = 1
	}
	if setting.MaxConcurrentSubmissionsPerUser > setting.MaxConcurrentSubmissions {
		setting.MaxConcurrentSubmissionsPerUser = setting.MaxConcurrentSubmissions
	}
	return setting
}

func CanAccess(role int) bool {
	switch Get().AccessMode {
	case AccessModeAll:
		return role >= common.RoleCommonUser
	case AccessModeAdmin:
		return role >= common.RoleAdminUser
	default:
		return false
	}
}
