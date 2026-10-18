package relay

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipResponsesWSRequiresConfirmedRejection(t *testing.T) {
	for _, tc := range []struct {
		body      string
		ambiguous bool
		confirmed bool
	}{
		{`{"type":"error","error":{"type":"invalid_request_error"}}`, false, true},
		{`{"type":"error","status":429,"error":{"type":"rate_limit_error"}}`, false, true},
		{`{"type":"error","status":500,"error":{"type":"invalid_request_error"}}`, false, false},
		{`{"type":"error","error":{"type":"server_error"}}`, false, false},
		{`{"type":"error","error":{"type":"invalid_request_error"},"usage":{"total_tokens":4}}`, false, false},
		{`{"type":"error","error":{"type":"invalid_request_error"}}`, true, false},
		{`{"type":"error","message":"unknown failure"}`, false, false},
	} {
		var event responsesWSErrorEvent
		require.NoError(t, common.UnmarshalJsonStr(tc.body, &event))
		assert.Equal(t, tc.confirmed, confirmedResponsesWSRejection(event, []byte(tc.body), tc.ambiguous), tc.body)
	}
}

func TestMembershipResponsesWSDeadlineClosesUpstreamBeforeCleanup(t *testing.T) {
	target, peer, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	session := &responsesWSSession{target: target}
	// A deadline must close transport even when request/accounting cleanup is
	// unable to acquire its normal write lock.
	session.targetWriteMu.Lock()
	defer session.targetWriteMu.Unlock()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	stop := session.closeUpstreamOnDeadline(ctx)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := peer.ReadMessage()
	assert.Error(t, err)
	assert.False(t, stop())
}

func TestMembershipResponsesWSCompletedCallPreservesReusableSocket(t *testing.T) {
	target, peer, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	session := &responsesWSSession{target: target}
	ctx, cancel := context.WithCancel(context.Background())
	stop := session.closeUpstreamOnDeadline(ctx)
	require.True(t, stop())
	cancel()
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte("next-call")))
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	_, message, err := peer.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "next-call", string(message))
}
