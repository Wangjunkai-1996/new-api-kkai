package video_studio_setting

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	AccessModeOff   = "off"
	AccessModeAdmin = "admin"
	AccessModeAll   = "all"
)

var (
	ErrR2NotConfigured            = errors.New("video studio R2 storage is not configured")
	ErrR2CredentialFileUnreadable = errors.New("video studio R2 credential file is unreadable")
)

type Setting struct {
	AccessMode            string `json:"access_mode"`
	ArchiveEnqueueEnabled bool   `json:"archive_enqueue_enabled"`
	WorkerEnabled         bool   `json:"worker_enabled"`
	BackfillEnabled       bool   `json:"backfill_enabled"`
	MaxReferenceBytes     int64  `json:"max_reference_bytes"`
	MaxArchivedVideoBytes int64  `json:"max_archived_video_bytes"`
	SignedURLSeconds      int    `json:"signed_url_seconds"`
	ReferenceOrphanHours  int    `json:"reference_orphan_hours"`
}

type UploadLimits struct {
	ReferenceMaxBytes int64 `json:"reference_max_bytes"`
	SampleMaxBytes    int64 `json:"sample_max_bytes"`
	ArchiveMaxBytes   int64 `json:"archive_max_bytes"`
}

func (setting Setting) UploadLimits() UploadLimits {
	return UploadLimits{
		ReferenceMaxBytes: setting.MaxReferenceBytes,
		SampleMaxBytes:    setting.MaxArchivedVideoBytes,
		ArchiveMaxBytes:   setting.MaxArchivedVideoBytes,
	}
}

type R2Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

var videoStudioSetting = Setting{
	AccessMode:            AccessModeOff,
	ArchiveEnqueueEnabled: false,
	WorkerEnabled:         false,
	BackfillEnabled:       false,
	MaxReferenceBytes:     20 << 20,
	MaxArchivedVideoBytes: 1 << 30,
	SignedURLSeconds:      600,
	ReferenceOrphanHours:  24,
}

func init() {
	config.GlobalConfig.Register("video_studio", &videoStudioSetting)
}

func Get() Setting {
	setting := videoStudioSetting
	switch setting.AccessMode {
	case AccessModeAdmin, AccessModeAll:
	default:
		setting.AccessMode = AccessModeOff
	}
	if setting.MaxReferenceBytes <= 0 || setting.MaxReferenceBytes > 100<<20 {
		setting.MaxReferenceBytes = 20 << 20
	}
	if setting.MaxArchivedVideoBytes <= 0 || setting.MaxArchivedVideoBytes > 4<<30 {
		setting.MaxArchivedVideoBytes = 1 << 30
	}
	if setting.SignedURLSeconds < 60 || setting.SignedURLSeconds > 3600 {
		setting.SignedURLSeconds = 600
	}
	if setting.ReferenceOrphanHours < 1 || setting.ReferenceOrphanHours > 720 {
		setting.ReferenceOrphanHours = 24
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

func LoadR2Config() (R2Config, error) {
	accessKeyID, err := loadR2Credential("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE")
	if err != nil {
		return R2Config{}, err
	}
	secretAccessKey, err := loadR2Credential("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE")
	if err != nil {
		return R2Config{}, err
	}
	result := R2Config{
		Endpoint:        strings.TrimRight(strings.TrimSpace(os.Getenv("VIDEO_STUDIO_R2_ENDPOINT")), "/"),
		Region:          strings.TrimSpace(os.Getenv("VIDEO_STUDIO_R2_REGION")),
		Bucket:          strings.TrimSpace(os.Getenv("VIDEO_STUDIO_R2_BUCKET")),
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
	}
	if result.Region == "" {
		result.Region = "auto"
	}
	if result.Endpoint == "" || result.Bucket == "" || result.AccessKeyID == "" || result.SecretAccessKey == "" {
		return R2Config{}, ErrR2NotConfigured
	}
	return result, nil
}

func loadR2Credential(valueEnvironment, fileEnvironment string) (string, error) {
	path := strings.TrimSpace(os.Getenv(fileEnvironment))
	if path == "" {
		return strings.TrimSpace(os.Getenv(valueEnvironment)), nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", fileEnvironment, ErrR2CredentialFileUnreadable)
	}
	return strings.TrimSpace(string(content)), nil
}
