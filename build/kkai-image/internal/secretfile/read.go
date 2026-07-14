package secretfile

import (
	"errors"
	"io"
	"os"
	"strings"
)

const maxSecretBytes = 8 * 1024

func Read(path string) (string, error) {
	if path == "" {
		return "", errors.New("secret file path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxSecretBytes {
		return "", errors.New("secret file is too large")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("secret file contains invalid data")
	}
	return value, nil
}
