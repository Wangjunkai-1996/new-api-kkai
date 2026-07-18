package riskguard

import (
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Wangjunkai-1996/new-api-kkai/build/kkai-image/internal/secretfile"

	"github.com/redis/go-redis/v9"
)

var redisUserPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Config struct {
	ListenAddress  string
	Upstream       *url.URL
	Redis          *redis.Options
	SigningSecret  string
	MaxBodyBytes   int64
	PublishTimeout time.Duration
}

func LoadConfig() (Config, error) {
	upstream, err := url.Parse(envOr("RISK_GUARD_UPSTREAM", "http://newapi-active:3000"))
	if err != nil || !isManagedUpstream(upstream) {
		return Config{}, errors.New("RISK_GUARD_UPSTREAM must be the managed New API origin")
	}
	redisAddress := envOr("RISK_GUARD_REDIS_ADDRESS", "newapi-redis:6379")
	redisUser := envOr("RISK_GUARD_REDIS_USER", "newapi_risk")
	if redisAddress != "newapi-redis:6379" || !redisUserPattern.MatchString(redisUser) {
		return Config{}, errors.New("risk Redis endpoint or user is invalid")
	}
	redisPassword, err := secretfile.Read(os.Getenv("RISK_GUARD_REDIS_PASSWORD_FILE"))
	if err != nil {
		return Config{}, err
	}
	signingSecret, err := secretfile.Read(os.Getenv("RISK_GUARD_SIGNING_SECRET_FILE"))
	if err != nil || len(signingSecret) < 32 {
		return Config{}, errors.New("risk stream signing secret must contain at least 32 bytes")
	}
	return Config{
		ListenAddress: envOr("RISK_GUARD_LISTEN", ":18081"),
		Upstream:      upstream,
		Redis: &redis.Options{
			Addr:     redisAddress,
			Username: redisUser,
			Password: redisPassword,
			DB:       0,
		},
		SigningSecret:  signingSecret,
		MaxBodyBytes:   2 * 1024 * 1024,
		PublishTimeout: 3 * time.Second,
	}, nil
}

func isManagedUpstream(upstream *url.URL) bool {
	return upstream != nil &&
		upstream.Scheme == "http" &&
		upstream.Hostname() == "newapi-active" &&
		upstream.Port() == "3000" &&
		upstream.User == nil &&
		upstream.Path == "" &&
		upstream.RawQuery == "" &&
		upstream.Fragment == ""
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
