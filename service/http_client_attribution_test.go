package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaiattribution"
	"github.com/stretchr/testify/require"
)

func TestCheckRedirectStripsAttributionHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/redirected", nil)
	for _, name := range kkaiattribution.HeaderNames() {
		req.Header.Set(name, "sensitive")
	}
	req.Header.Set(kkaiattribution.LegacyTokenNameHeader, "sensitive-token-name")

	_ = checkRedirect(req, nil)

	for _, name := range kkaiattribution.HeaderNames() {
		require.Empty(t, req.Header.Get(name))
	}
	require.Empty(t, req.Header.Get(kkaiattribution.LegacyTokenNameHeader))
}

func TestProtectedFetchRedirectStripsAttributionHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/redirected", nil)
	for _, name := range kkaiattribution.HeaderNames() {
		req.Header.Set(name, "sensitive")
	}
	req.Header.Set(kkaiattribution.LegacyTokenNameHeader, "sensitive-token-name")

	_ = checkProtectedFetchRedirect(req, nil)

	for _, name := range kkaiattribution.HeaderNames() {
		require.Empty(t, req.Header.Get(name))
	}
	require.Empty(t, req.Header.Get(kkaiattribution.LegacyTokenNameHeader))
}
