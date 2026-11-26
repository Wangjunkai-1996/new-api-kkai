package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoWebhookMigrationPristineAndAuthorization(t *testing.T) {
	dsn := "sqlite://" + filepath.Join(t.TempDir(), "schema.db")
	var output bytes.Buffer
	require.NoError(t, runVideoWebhookExpansion(t.Context(), dsn, "plan", "", &output))
	var status struct {
		State        string
		PlanChecksum string `json:"plan_checksum"`
	}
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.Equal(t, "pristine", status.State)
	assert.Len(t, status.PlanChecksum, 64)
	assert.Error(t, runVideoWebhookExpansion(t.Context(), dsn, "check", "", &output))
	assert.Error(t, runVideoWebhookExpansion(t.Context(), dsn, "apply", "", &output))
	assert.Error(t, runVideoWebhookExpansion(t.Context(), dsn, "plan", videoWebhookProofPath, &output))
}
