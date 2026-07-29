package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/Wangjunkai-1996/new-api-kkai/build/kkai-image/internal/secretfile"
)

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		fatalf("required environment variable %s is empty", name)
	}
	return value
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func readSecret(pathVariable string) string {
	path := requiredEnv(pathVariable)
	value, err := secretfile.Read(path)
	if err != nil {
		fatalf("read %s: %v", pathVariable, err)
	}
	return value
}

func setEnvironment(name, value string) {
	if err := os.Setenv(name, value); err != nil {
		fatalf("set %s: %v", name, err)
	}
}

func configureRebateEventDelivery() error {
	endpointConfigured := strings.TrimSpace(os.Getenv("REBATE_EVENT_INGEST_URL")) != ""
	secretPath := strings.TrimSpace(os.Getenv("NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE"))
	secretConfigured := secretPath != ""
	if endpointConfigured != secretConfigured {
		return fmt.Errorf("REBATE_EVENT_INGEST_URL and NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE must be configured together")
	}
	if !endpointConfigured {
		return os.Unsetenv("REBATE_EVENT_INGEST_SECRET")
	}
	secret, err := secretfile.Read(secretPath)
	if err != nil {
		return fmt.Errorf("read NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE: %w", err)
	}
	return os.Setenv("REBATE_EVENT_INGEST_SECRET", secret)
}

func databaseDSN(password string) string {
	dsn := &url.URL{
		Scheme: "postgresql",
		User: url.UserPassword(
			requiredEnv("NEWAPI_DATABASE_USER"),
			password,
		),
		Host: net.JoinHostPort(
			requiredEnv("NEWAPI_DATABASE_HOST"),
			envOrDefault("NEWAPI_DATABASE_PORT", "5432"),
		),
		Path: "/" + requiredEnv("NEWAPI_DATABASE_NAME"),
	}
	query := dsn.Query()
	query.Set("sslmode", envOrDefault("NEWAPI_DATABASE_SSLMODE", "disable"))
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func redisDSN(password string) string {
	dsn := &url.URL{
		Scheme: "redis",
		User: url.UserPassword(
			requiredEnv("NEWAPI_REDIS_USER"),
			password,
		),
		Host: net.JoinHostPort(
			requiredEnv("NEWAPI_REDIS_HOST"),
			envOrDefault("NEWAPI_REDIS_PORT", "6379"),
		),
		Path: "/" + requiredEnv("NEWAPI_REDIS_DATABASE"),
	}
	return dsn.String()
}

func fatalf(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "new-api-entrypoint: "+format+"\n", values...)
	os.Exit(1)
}

func commandArguments(arguments []string) []string {
	if len(arguments) > 0 && arguments[0] == "kkai-video-archive-once" {
		return append([]string{"/kkai-video-archive-once"}, arguments[1:]...)
	}
	return append([]string{"/new-api"}, arguments...)
}

func main() {
	setEnvironment("SQL_DSN", databaseDSN(readSecret("NEWAPI_DATABASE_PASSWORD_FILE")))
	setEnvironment("REDIS_CONN_STRING", redisDSN(readSecret("NEWAPI_REDIS_PASSWORD_FILE")))
	setEnvironment("SESSION_SECRET", readSecret("NEWAPI_SESSION_SECRET_FILE"))
	setEnvironment("CRYPTO_SECRET", readSecret("NEWAPI_CRYPTO_SECRET_FILE"))
	setEnvironment("INVITATIONS_INTERNAL_SECRET", readSecret("NEWAPI_INVITATIONS_INTERNAL_SECRET_FILE"))
	if err := configureRebateEventDelivery(); err != nil {
		fatalf("configure rebate event delivery: %v", err)
	}
	setEnvironment("KKAI_RISK_STREAM_SECRET", readSecret("NEWAPI_RISK_STREAM_SECRET_FILE"))

	arguments := commandArguments(os.Args[1:])
	if err := syscall.Exec(arguments[0], arguments, os.Environ()); err != nil {
		fatalf("exec %s: %v", arguments[0], err)
	}
}
