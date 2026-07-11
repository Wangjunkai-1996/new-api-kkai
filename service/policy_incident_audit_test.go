package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type streamingOnlyPolicyIncidentBody struct{ *bytes.Reader }

func (s *streamingOnlyPolicyIncidentBody) Close() error { return nil }
func (s *streamingOnlyPolicyIncidentBody) Bytes() ([]byte, error) {
	panic("audit digest must not materialize the request body")
}
func (s *streamingOnlyPolicyIncidentBody) Size() int64  { return 1 }
func (s *streamingOnlyPolicyIncidentBody) IsDisk() bool { return true }

func TestPolicyIncidentAuditHashesCompleteBodyWithoutMaterializingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	body := []byte(strings.Repeat("policy-body-", 10000))
	storage := &streamingOnlyPolicyIncidentBody{Reader: bytes.NewReader(body)}
	ctx.Set(common.KeyBodyStorage, storage)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	digest, size, err := readPolicyIncidentRequestBodyDigest(ctx)
	require.NoError(t, err)
	sum := sha256.Sum256(body)
	assert.Equal(t, hex.EncodeToString(sum[:]), digest)
	assert.EqualValues(t, len(body), size)
	reloaded, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, body, reloaded)
}

func TestPolicyIncidentAuditMetadataContainsOnlyWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/private/patient/Alice", strings.NewReader("secret prompt"))
	ctx.Request.Header.Set("Authorization", "Bearer sk-secret")
	ctx.Set("token_name", "private-token-name")

	metadata := preparePolicyIncidentAuditMetadata(ctx, true).Map()
	assert.ElementsMatch(t, []string{"case_id", "request_body_sha256", "request_body_bytes", "client_token_action_allowed"}, mapKeys(metadata))
	encoded, err := common.Marshal(metadata)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "Alice")
	assert.NotContains(t, string(encoded), "secret prompt")
	assert.NotContains(t, string(encoded), "sk-secret")
	assert.NotContains(t, string(encoded), "private-token-name")
}

func TestPolicyIncidentAuditDoesNotUseEvidenceDirectory(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy-policy-evidence")
	require.NoError(t, os.MkdirAll(legacyDir, 0700))
	legacyFile := filepath.Join(legacyDir, "policy-legacy.json.gz")
	require.NoError(t, os.WriteFile(legacyFile, []byte("legacy"), 0600))
	t.Setenv("NEW_API_POLICY_EVIDENCE_DIR", legacyDir)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("request"))
	_ = preparePolicyIncidentAuditMetadata(ctx, false)

	require.FileExists(t, legacyFile)
	entries, err := os.ReadDir(legacyDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

type failingPolicyIncidentBody struct{}

func (f *failingPolicyIncidentBody) Read([]byte) (int, error)       { return 0, assert.AnError }
func (f *failingPolicyIncidentBody) Seek(int64, int) (int64, error) { return 0, nil }
func (f *failingPolicyIncidentBody) Close() error                   { return nil }
func (f *failingPolicyIncidentBody) Bytes() ([]byte, error)         { return nil, assert.AnError }
func (f *failingPolicyIncidentBody) Size() int64                    { return 0 }
func (f *failingPolicyIncidentBody) IsDisk() bool                   { return false }
