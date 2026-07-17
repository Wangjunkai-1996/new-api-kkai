package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionStringsEscapeCredentials(t *testing.T) {
	t.Setenv("NEWAPI_DATABASE_USER", "newapi")
	t.Setenv("NEWAPI_DATABASE_HOST", "kkai-postgres")
	t.Setenv("NEWAPI_DATABASE_NAME", "newapi_stage")
	t.Setenv("NEWAPI_REDIS_USER", "newapi")
	t.Setenv("NEWAPI_REDIS_HOST", "newapi-redis")
	t.Setenv("NEWAPI_REDIS_DATABASE", "1")

	databaseURL, err := url.Parse(databaseDSN("p@ss:/word"))
	if err != nil {
		t.Fatal(err)
	}
	password, ok := databaseURL.User.Password()
	if !ok || password != "p@ss:/word" {
		t.Fatalf("database password was not encoded safely")
	}
	if databaseURL.Query().Get("sslmode") != "disable" {
		t.Fatalf("database sslmode is missing")
	}

	redisURL, err := url.Parse(redisDSN("redis@secret"))
	if err != nil {
		t.Fatal(err)
	}
	password, ok = redisURL.User.Password()
	if !ok || password != "redis@secret" {
		t.Fatalf("redis password was not encoded safely")
	}
}

func TestReadSecretRemovesOnlyFileTerminator(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, []byte(" preserved value \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_FILE", secretPath)

	if got := readSecret("TEST_SECRET_FILE"); got != " preserved value " {
		t.Fatalf("unexpected secret value %q", got)
	}
}

func TestConfigureRebateEventDeliveryAllowsDisabledRuntime(t *testing.T) {
	t.Setenv("REBATE_EVENT_INGEST_URL", "")
	t.Setenv("NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE", "")
	t.Setenv("REBATE_EVENT_INGEST_SECRET", "stale-secret-must-be-cleared")

	if err := configureRebateEventDelivery(); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv("REBATE_EVENT_INGEST_SECRET"); value != "" {
		t.Fatalf("disabled delivery retained runtime secret %q", value)
	}
}

func TestConfigureRebateEventDeliveryLoadsEnabledSecret(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "rebate-secret")
	if err := os.WriteFile(secretPath, []byte("rebate-secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REBATE_EVENT_INGEST_URL", "https://invitations.internal/api/internal/rebate-source-events")
	t.Setenv("NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE", secretPath)

	if err := configureRebateEventDelivery(); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv("REBATE_EVENT_INGEST_SECRET"); value != "rebate-secret-value" {
		t.Fatalf("unexpected runtime secret %q", value)
	}
}

func TestConfigureRebateEventDeliveryRejectsHalfConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		endpoint   string
		secretPath string
	}{
		{name: "endpoint only", endpoint: "https://invitations.internal/api/internal/rebate-source-events"},
		{name: "secret file only", secretPath: "/run/secrets/rebate_event_ingest_secret"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("REBATE_EVENT_INGEST_URL", testCase.endpoint)
			t.Setenv("NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE", testCase.secretPath)
			if err := configureRebateEventDelivery(); err == nil {
				t.Fatal("expected half-configured delivery to fail")
			}
		})
	}
}
