package video_studio_setting

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadR2ConfigPrefersCredentialFiles(t *testing.T) {
	accessKeyPath := filepath.Join(t.TempDir(), "access-key")
	secretKeyPath := filepath.Join(t.TempDir(), "secret-key")
	require.NoError(t, os.WriteFile(accessKeyPath, []byte("  file-access-key\n"), 0o600))
	require.NoError(t, os.WriteFile(secretKeyPath, []byte("file-secret-key\r\n"), 0o600))

	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", " https://example.r2.cloudflarestorage.com/ ")
	t.Setenv("VIDEO_STUDIO_R2_REGION", " auto ")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", " video-assets ")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "environment-access-key")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "environment-secret-key")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", accessKeyPath)
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", secretKeyPath)

	config, err := LoadR2Config()
	require.NoError(t, err)
	assert.Equal(t, "https://example.r2.cloudflarestorage.com", config.Endpoint)
	assert.Equal(t, "auto", config.Region)
	assert.Equal(t, "video-assets", config.Bucket)
	assert.Equal(t, "file-access-key", config.AccessKeyID)
	assert.Equal(t, "file-secret-key", config.SecretAccessKey)
}

func TestLoadR2ConfigRetainsEnvironmentCredentialCompatibility(t *testing.T) {
	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("VIDEO_STUDIO_R2_REGION", "")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "video-assets")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "environment-access-key")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "environment-secret-key")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", "")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", "")

	config, err := LoadR2Config()
	require.NoError(t, err)
	assert.Equal(t, "auto", config.Region)
	assert.Equal(t, "environment-access-key", config.AccessKeyID)
	assert.Equal(t, "environment-secret-key", config.SecretAccessKey)
}

func TestLoadR2ConfigRejectsMissingCredentials(t *testing.T) {
	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "video-assets")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", "")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", "")

	_, err := LoadR2Config()
	require.ErrorIs(t, err, ErrR2NotConfigured)
}

func TestLoadR2ConfigRejectsUnreadableCredentialFileWithoutSecretContent(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing-secret")
	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "video-assets")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "must-not-fallback")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "must-not-appear")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", missingPath)
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", "")

	_, err := LoadR2Config()
	require.ErrorIs(t, err, ErrR2CredentialFileUnreadable)
	assert.NotContains(t, err.Error(), "must-not-fallback")
	assert.NotContains(t, err.Error(), "must-not-appear")
}
