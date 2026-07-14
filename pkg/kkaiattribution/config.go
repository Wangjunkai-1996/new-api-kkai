package kkaiattribution

import (
	"os"
	"strings"
)

const (
	OriginsEnvironmentVariable = "KKAI_ATTRIBUTION_ORIGINS"
	SecretEnvironmentVariable  = "KKAI_ATTRIBUTION_SECRET"
)

func NewSignerFromEnvironment() (*Signer, error) {
	originsRaw := strings.TrimSpace(os.Getenv(OriginsEnvironmentVariable))
	secret := os.Getenv(SecretEnvironmentVariable)
	if originsRaw == "" && secret == "" {
		return nil, nil
	}
	if originsRaw == "" || secret == "" {
		return nil, ErrInvalidConfiguration
	}
	origins := make([]string, 0)
	for _, origin := range strings.Split(originsRaw, ",") {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return NewSigner(origins, secret)
}
