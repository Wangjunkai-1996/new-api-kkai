package secretfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAcceptsOneFileTerminator(t *testing.T) {
	path := writeSecret(t, " preserved value \r\n")
	value, err := Read(path)
	if err != nil || value != " preserved value " {
		t.Fatalf("Read() = %q, %v", value, err)
	}
}

func TestReadRejectsInternalNewlinesAndOversizedFiles(t *testing.T) {
	for name, value := range map[string]string{
		"internal-newline": "first\nsecond",
		"oversized":        strings.Repeat("x", maxSecretBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Read(writeSecret(t, value)); err == nil {
				t.Fatal("Read() accepted invalid secret data")
			}
		})
	}
}

func writeSecret(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
