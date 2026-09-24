package kkaiattribution

import (
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	OriginsEnvironmentVariable    = "KKAI_ATTRIBUTION_ORIGINS"
	SecretEnvironmentVariable     = "KKAI_ATTRIBUTION_SECRET"
	ConfigFileEnvironmentVariable = "KKAI_ATTRIBUTION_CONFIG_FILE"
	defaultConfigFile             = "/data/kkai-attribution.json"
	maxConfigFileSize             = 16 * 1024
)

func NewSignerFromEnvironment() (*Signer, error) {
	originsRaw := strings.TrimSpace(os.Getenv(OriginsEnvironmentVariable))
	secret := os.Getenv(SecretEnvironmentVariable)
	if originsRaw == "" && secret == "" {
		return newSignerFromConfigFile()
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

func newSignerFromConfigFile() (*Signer, error) {
	path := strings.TrimSpace(os.Getenv(ConfigFileEnvironmentVariable))
	usingDefault := path == ""
	if usingDefault {
		path = defaultConfigFile
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && usingDefault {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrInvalidConfiguration
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0077 != 0 || openedInfo.Size() > maxConfigFileSize {
		return nil, ErrInvalidConfiguration
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigFileSize+1))
	if err != nil || len(data) > maxConfigFileSize {
		return nil, ErrInvalidConfiguration
	}
	var config struct {
		Origins []string `json:"origins"`
		Secret  string   `json:"secret"`
	}
	if common.Unmarshal(data, &config) != nil {
		return nil, ErrInvalidConfiguration
	}
	return NewSigner(config.Origins, config.Secret)
}
