package service

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPolicyIncidentEvidenceDirDefaultsToDataPath(t *testing.T) {
	t.Setenv(policyIncidentEvidenceDirEnv, "")

	require.Equal(t, "/data/policy-evidence", policyIncidentEvidenceDir())
}

func TestPolicyIncidentEvidenceDirUsesCleanOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "nested", "..", "policy-evidence")
	t.Setenv(policyIncidentEvidenceDirEnv, override)

	require.Equal(t, filepath.Clean(override), policyIncidentEvidenceDir())
}

func TestPolicyIncidentEvidenceGzipRedactsRequestAndErrorSecrets(t *testing.T) {
	const (
		caseID      = "policy-test-case"
		upstreamKey = "upstream-raw-key"
		clientToken = "sk-client-token-secret"
	)
	evidenceDir := filepath.Join(t.TempDir(), "policy-evidence")
	t.Setenv(policyIncidentEvidenceDirEnv, evidenceDir)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(common.RequestIdKey, "req-policy-evidence-redaction")
	ctx.Set("id", 42)
	ctx.Set("token_id", 77)
	ctx.Set("token_name", "client-token")
	ctx.Set("token_key", strings.TrimPrefix(clientToken, "sk-"))
	ctx.Set("original_model", "gpt-policy")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions/"+upstreamKey+"/"+clientToken, nil)
	ctx.Request.Header.Set("Authorization", "Bearer "+clientToken)

	body := []byte(`{"messages":[{"role":"user","content":"Authorization: Bearer sk-client-token-secret with upstream-raw-key"}]}`)
	classification := PolicyIncidentClassification{
		StatusCode:   http.StatusForbidden,
		ErrorCode:    string(types.ErrorCodeBadResponseStatusCode),
		ErrorMessage: "provider returned cyber_policy with Authorization: Bearer " + clientToken + " and " + upstreamKey,
	}

	payload, err := buildPolicyIncidentEvidenceFile(
		ctx,
		*types.NewChannelError(88, 1, "policy-channel", false, upstreamKey, true),
		classification,
		caseID,
		time.Unix(1710000000, 0),
		body,
	)
	require.NoError(t, err)

	evidencePath, _, err := writePolicyIncidentEvidenceFile(caseID, payload)
	require.NoError(t, err)
	gzipPayload, err := os.ReadFile(evidencePath)
	require.NoError(t, err)

	var evidence policyIncidentEvidenceFile
	decodedEvidence, err := readPolicyIncidentEvidenceGzipForEvidenceTest(gzipPayload, &evidence)
	require.NoError(t, err)
	decodedText := string(decodedEvidence)

	require.Equal(t, caseID, evidence.CaseID)
	require.Equal(t, model.FingerprintPolicyIncidentUpstreamKey(upstreamKey), evidence.UpstreamKeyFingerprint)
	require.Contains(t, decodedText, model.PolicyIncidentMetadataRedacted)
	require.NotContains(t, decodedText, upstreamKey)
	require.NotContains(t, decodedText, clientToken)
	require.NotContains(t, decodedText, strings.TrimPrefix(clientToken, "sk-"))
	require.NotContains(t, decodedText, "Bearer "+clientToken)
	require.NotContains(t, decodedText, "Authorization: Bearer")
}

func readPolicyIncidentEvidenceGzipForEvidenceTest(payload []byte, v any) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return decoded, common.Unmarshal(decoded, v)
}
