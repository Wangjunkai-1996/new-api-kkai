package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImageRelayResponseFileAcceptsURLAndBase64Results(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"created": 1,
		"data": [
			{"url": "https://cdn.example.test/result.png", "revised_prompt": " revised "},
			{"b64_json": "aGVsbG8="}
		]
	}`), 0o600))

	results, err := ParseImageRelayResponseFile(path, 1<<20, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "https://cdn.example.test/result.png", results[0].URL)
	assert.Equal(t, "revised", results[0].RevisedPrompt)
	assert.Equal(t, "aGVsbG8=", results[1].Base64)
}

func TestParseImageRelayResponseFileRejectsAmbiguousOrExtraResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"data": [{"url": "https://example.test/a", "b64_json": "aA=="}]
	}`), 0o600))

	_, err := ParseImageRelayResponseFile(path, 1<<20, 1)
	require.ErrorIs(t, err, ErrInvalidImageRelayResponse)

	require.NoError(t, os.WriteFile(path, []byte(`{
		"data": [{"url": "https://example.test/a"}, {"url": "https://example.test/b"}]
	}`), 0o600))
	_, err = ParseImageRelayResponseFile(path, 1<<20, 1)
	require.ErrorIs(t, err, ErrInvalidImageRelayResponse)
}
