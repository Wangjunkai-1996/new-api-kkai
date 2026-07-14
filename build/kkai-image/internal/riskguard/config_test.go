package riskguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configureTestSecrets(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	redisPath := filepath.Join(directory, "redis")
	signingPath := filepath.Join(directory, "signing")
	if err := os.WriteFile(redisPath, []byte(strings.Repeat("r", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signingPath, []byte(strings.Repeat("s", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RISK_GUARD_REDIS_PASSWORD_FILE", redisPath)
	t.Setenv("RISK_GUARD_SIGNING_SECRET_FILE", signingPath)
}

func TestLoadConfigUsesManagedInternalEndpoints(t *testing.T) {
	configureTestSecrets(t)
	t.Setenv("RISK_GUARD_REDIS_USER", "newapi_stage_risk")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Upstream.String() != "http://newapi-active:3000" {
		t.Fatalf("unexpected upstream %q", config.Upstream)
	}
	if config.Redis.Addr != "newapi-redis:6379" || config.Redis.Username != "newapi_stage_risk" {
		t.Fatalf("unexpected Redis identity %#v", config.Redis)
	}
}

func TestLoadConfigRejectsExternalOrCredentialBearingEndpoints(t *testing.T) {
	configureTestSecrets(t)
	for _, upstream := range []string{
		"https://newapi-active:3000",
		"http://user@newapi-active:3000",
		"http://example.com:3000",
		"http://newapi-active:8080",
	} {
		t.Run(upstream, func(t *testing.T) {
			t.Setenv("RISK_GUARD_UPSTREAM", upstream)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("accepted unsafe upstream %q", upstream)
			}
		})
	}
}
