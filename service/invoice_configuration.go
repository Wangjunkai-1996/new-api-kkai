package service

import (
	"errors"
	"io"
	"os"
	"strings"
)

// invoiceSecret keeps invoice credentials in mounted files in production while
// preserving direct environment configuration for standalone installations.
func invoiceSecret(name string) (string, error) {
	path := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if path == "" {
		return os.Getenv(name), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("invoice secret file is unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 {
		return "", errors.New("invoice secret file is invalid")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("invoice secret file is invalid")
	}
	return value, nil
}
