package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/kkaiattribution"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type attributionNonceStore struct {
	seen map[string]struct{}
}

func (s *attributionNonceStore) Reserve(_ context.Context, nonce string, _ time.Time) (bool, error) {
	if _, exists := s.seen[nonce]; exists {
		return false, nil
	}
	s.seen[nonce] = struct{}{}
	return true, nil
}

func TestApplyInternalAttributionHeadersSignsContextWithoutTokenName(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	signer, err := kkaiattribution.NewSigner([]string{"https://guard.internal.example:8443"}, secret)
	require.NoError(t, err)
	previous := internalAttributionSigner
	internalAttributionSigner = signer
	t.Cleanup(func() { internalAttributionSigner = previous })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(commonpkg.RequestIdKey, "req-context")
	ctx.Set("token_name", "must-not-leave-process")
	commonpkg.SetContextKey(ctx, constant.ContextKeyUserId, 42)
	commonpkg.SetContextKey(ctx, constant.ContextKeyTokenId, 77)
	commonpkg.SetContextKey(ctx, constant.ContextKeyChannelId, 13)
	commonpkg.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 2)
	commonpkg.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-4.1")
	info := &relaycommon.RelayInfo{RequestId: "fallback", OriginModelName: "fallback-model"}
	req := httptest.NewRequest(http.MethodPost, "https://guard.internal.example:8443/v1/policy", nil)
	req.Header.Set(kkaiattribution.UserIDHeader, "forged")
	req.Header.Set(kkaiattribution.LegacyTokenNameHeader, "forged-name")

	err = applyInternalAttributionHeaders(req, ctx, info)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get(kkaiattribution.LegacyTokenNameHeader))
	require.Equal(t, "42", req.Header.Get(kkaiattribution.UserIDHeader))
	require.NotEmpty(t, req.Header.Get(kkaiattribution.SignatureHeader))

	verifier, err := kkaiattribution.NewVerifier(
		[]string{"https://guard.internal.example:8443"},
		secret,
		&attributionNonceStore{seen: make(map[string]struct{})},
	)
	require.NoError(t, err)
	claims, err := verifier.VerifyRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, kkaiattribution.Claims{
		RequestID:     "req-context",
		UserID:        42,
		TokenID:       77,
		ChannelID:     13,
		MultiKeyIndex: 2,
		Model:         "gpt-4.1",
		Source:        "new-api",
	}, claims)
}

func TestApplyInternalAttributionHeadersStripsUnlistedOrigin(t *testing.T) {
	signer, err := kkaiattribution.NewSigner(
		[]string{"https://guard.internal.example:8443"},
		"0123456789abcdef0123456789abcdef",
	)
	require.NoError(t, err)
	previous := internalAttributionSigner
	internalAttributionSigner = signer
	t.Cleanup(func() { internalAttributionSigner = previous })

	req := httptest.NewRequest(http.MethodPost, "https://api.external.example/v1/chat", nil)
	for _, name := range kkaiattribution.HeaderNames() {
		req.Header.Set(name, "forged")
	}
	req.Header.Set(kkaiattribution.LegacyTokenNameHeader, "forged-name")

	err = applyInternalAttributionHeaders(req, nil, nil)
	require.NoError(t, err)
	for _, name := range kkaiattribution.HeaderNames() {
		require.Empty(t, req.Header.Get(name))
	}
	require.Empty(t, req.Header.Get(kkaiattribution.LegacyTokenNameHeader))
}
