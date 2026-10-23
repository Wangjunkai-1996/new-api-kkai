package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceSecretFilesOverrideEnvironmentAndFailClosed(t *testing.T) {
	name := "KKAI_INVOICE_KEYS"
	t.Setenv(name, "standalone-value")
	t.Setenv(name+"_FILE", "")
	value, err := invoiceSecret(name)
	require.NoError(t, err)
	assert.Equal(t, "standalone-value", value)
	path := filepath.Join(t.TempDir(), "invoice-secret")
	t.Setenv(name+"_FILE", path)
	value, err = invoiceSecret(name)
	require.Error(t, err, "an unreadable configured file must never fall back to a stale environment secret")
	assert.Empty(t, value)
	assert.NotContains(t, err.Error(), path)
	for _, content := range []string{"mounted-value\n", "", "line1\nline2", strings.Repeat("s", 8193)} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		value, err = invoiceSecret(name)
		if content == "mounted-value\n" {
			require.NoError(t, err)
			assert.Equal(t, "mounted-value", value)
		} else {
			require.Error(t, err)
			assert.Empty(t, value)
		}
	}
}
