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
