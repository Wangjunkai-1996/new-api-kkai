package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexModelsDiscoveryPreservesProtocolAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/backend-api/codex/models", r.URL.Path)
		assert.Equal(t, "0.100.0", r.URL.Query().Get("client_version"))
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "test-account", r.Header.Get("ChatGPT-Account-Id"))
		assert.Equal(t, "codex-cli/0.100.0", r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(`{"models":[{"slug":" gpt-5-codex "},{"slug":""},{"slug":"gpt-5-codex"},{"slug":"gpt-5"}]}`))
	}))
	defer server.Close()
	status, models, err := FetchCodexModels(context.Background(), server.Client(), server.URL+"/", &CodexOAuthKey{
		AccessToken: " test-token ", AccountID: "test-account",
	}, "0.100.0")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, []string{"gpt-5-codex", "gpt-5"}, models)
}

func TestCodexModelsDiscoveryReportsUpstreamStatus(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
		}))
		models, err := fetchCodexChannelModels(context.Background(), &model.Channel{
			Type: constant.ChannelTypeCodex, Key: `{"access_token":"token","account_id":"account"}`,
		}, server.URL, server.Client(), "0.100.0")
		require.Error(t, err)
		assert.Nil(t, models)
		if status == http.StatusUnauthorized {
			assert.Contains(t, err.Error(), "save the channel")
		} else {
			assert.Contains(t, err.Error(), "upstream status")
		}
		server.Close()
	}
}

func TestCodexClientVersionCacheRefreshAndFailureFallback(t *testing.T) {
	requests := 0
	body := `{"name":"0.100.0","draft":false,"prerelease":false}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	cache := codexClientVersionCache{}
	now := time.Unix(1000, 0)
	version, err := cache.get(context.Background(), server.Client(), server.URL, now)
	require.NoError(t, err)
	assert.Equal(t, "0.100.0", version)
	version, err = cache.get(context.Background(), server.Client(), server.URL, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "0.100.0", version)
	assert.Equal(t, 1, requests)
	body = `{"name":"0.101.0-beta","prerelease":true}`
	version, err = cache.get(context.Background(), server.Client(), server.URL, now.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "0.100.0", version, "unstable releases never replace a cached stable version")
	assert.Equal(t, 2, requests)
	body = `{"name":"0.101.0","prerelease":false}`
	version, err = cache.get(context.Background(), server.Client(), server.URL, now.Add(4*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "0.101.0", version)
	assert.Equal(t, 3, requests)
}

func TestCodexClientVersionRejectsInvalidReleaseWithoutCache(t *testing.T) {
	for _, body := range []string{`{"name":""}`, `{"name":"dev","draft":true}`, `{"name":"preview","prerelease":true}`, `invalid`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		cache := codexClientVersionCache{}
		_, err := cache.get(context.Background(), server.Client(), server.URL, time.Now())
		require.Error(t, err)
		server.Close()
	}
}
