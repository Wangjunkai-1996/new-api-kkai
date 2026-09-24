package kkaiattribution

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSignerFromEnvironmentDisabledWhenUnset(t *testing.T) {
	t.Setenv(OriginsEnvironmentVariable, "")
	t.Setenv(SecretEnvironmentVariable, "")
	t.Setenv(ConfigFileEnvironmentVariable, "")
	if _, err := os.Lstat(defaultConfigFile); !os.IsNotExist(err) {
		t.Skip("default attribution configuration exists on this host")
	}

	signer, err := NewSignerFromEnvironment()
	require.NoError(t, err)
	require.Nil(t, signer)
}

func TestNewSignerFromEnvironmentRejectsPartialOrWeakConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		origins string
		secret  string
	}{
		{name: "missing secret", origins: "https://guard.internal.example:8443"},
		{name: "missing origins", secret: attributionTestSecret},
		{name: "weak secret", origins: "https://guard.internal.example:8443", secret: "short"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(OriginsEnvironmentVariable, test.origins)
			t.Setenv(SecretEnvironmentVariable, test.secret)
			_, err := NewSignerFromEnvironment()
			require.ErrorIs(t, err, ErrInvalidConfiguration)
		})
	}
}

func TestNewSignerFromEnvironmentAcceptsCommaSeparatedOrigins(t *testing.T) {
	t.Setenv(OriginsEnvironmentVariable, "https://guard-a.internal.example:8443, https://guard-b.internal.example:9443")
	t.Setenv(SecretEnvironmentVariable, attributionTestSecret)
	// Complete environment configuration must not depend on a configured file.
	t.Setenv(ConfigFileEnvironmentVariable, filepath.Join(t.TempDir(), "missing.json"))

	signer, err := NewSignerFromEnvironment()
	require.NoError(t, err)
	require.NotNil(t, signer)
}

func TestNewSignerFromEnvironmentConfigFileSignsAndRejectsPartialEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("configuration requires Unix file permissions")
	}
	t.Setenv(OriginsEnvironmentVariable, "")
	t.Setenv(SecretEnvironmentVariable, "")
	path := filepath.Join(t.TempDir(), "attribution.json")
	t.Setenv(ConfigFileEnvironmentVariable, path)
	require.NoError(t, os.WriteFile(path, []byte(`{"origins":["https://guard.internal.example:8443"],"secret":"`+attributionTestSecret+`"}`), 0600))

	signer, err := NewSignerFromEnvironment()
	require.NoError(t, err)
	require.NotNil(t, signer)
	req, err := http.NewRequest(http.MethodPost, "https://guard.internal.example:8443/v1/messages", nil)
	require.NoError(t, err)
	signed, err := signer.ApplyRequest(req, validAttributionClaims())
	require.NoError(t, err)
	assert.True(t, signed)
	verifier, err := NewVerifier([]string{"https://guard.internal.example:8443"}, attributionTestSecret, &memoryNonceStore{reserved: make(map[string]struct{})})
	require.NoError(t, err)
	claims, err := verifier.VerifyRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, validAttributionClaims(), claims)

	t.Setenv(OriginsEnvironmentVariable, "https://other.internal.example")
	signer, err = NewSignerFromEnvironment()
	require.ErrorIs(t, err, ErrInvalidConfiguration)
	assert.Nil(t, signer)
}

func TestNewSignerFromEnvironmentRejectsInvalidConfigFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("configuration requires Unix file permissions")
	}
	t.Setenv(OriginsEnvironmentVariable, "")
	t.Setenv(SecretEnvironmentVariable, "")
	validConfig := `{"origins":["https://guard.internal.example"],"secret":"` + attributionTestSecret + `"}`
	tests := []struct {
		name string
		body string
		mode os.FileMode
	}{
		{name: "malformed", body: `{"secret":"` + attributionTestSecret + `",`, mode: 0600},
		{name: "missing origins", body: `{"secret":"` + attributionTestSecret + `"}`, mode: 0600},
		{name: "missing secret", body: `{"origins":["https://guard.internal.example"]}`, mode: 0600},
		{name: "weak secret", body: `{"origins":["https://guard.internal.example"],"secret":"short"}`, mode: 0600},
		{name: "invalid origin", body: `{"origins":["https://guard.internal.example/path"],"secret":"` + attributionTestSecret + `"}`, mode: 0600},
		{name: "trailing value", body: `{"origins":["https://guard.internal.example"],"secret":"` + attributionTestSecret + `"} {}`, mode: 0600},
		{name: "group readable", body: validConfig, mode: 0640},
		{name: "other writable", body: validConfig, mode: 0602},
		{name: "oversized", body: validConfig + strings.Repeat(" ", maxConfigFileSize), mode: 0600},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attribution.json")
			t.Setenv(ConfigFileEnvironmentVariable, path)
			require.NoError(t, os.WriteFile(path, []byte(test.body), 0600))
			require.NoError(t, os.Chmod(path, test.mode))
			signer, err := NewSignerFromEnvironment()
			require.ErrorIs(t, err, ErrInvalidConfiguration)
			assert.Equal(t, ErrInvalidConfiguration.Error(), err.Error())
			assert.Nil(t, signer)
		})
	}
	for _, kind := range []string{"missing", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attribution.json")
			switch kind {
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "symlink":
				require.NoError(t, os.WriteFile(path+".target", []byte(validConfig), 0600))
				require.NoError(t, os.Symlink(path+".target", path))
			}
			t.Setenv(ConfigFileEnvironmentVariable, path)
			signer, err := NewSignerFromEnvironment()
			require.ErrorIs(t, err, ErrInvalidConfiguration)
			assert.Equal(t, ErrInvalidConfiguration.Error(), err.Error())
			assert.Nil(t, signer)
		})
	}
}
