package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type realtimePolicyCloseProbe struct {
	closed chan struct{}
	once   sync.Once
}

func newRealtimePolicyCloseProbe() *realtimePolicyCloseProbe {
	return &realtimePolicyCloseProbe{closed: make(chan struct{})}
}

func (p *realtimePolicyCloseProbe) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func newRealtimeWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConn := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			serverConn <- conn
		}
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	serverSide := <-serverConn
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverSide.Close()
		server.Close()
	})
	return serverSide, client
}

func TestRealtimePolicyErrorUsesStructuredCodeForClientAttribution(t *testing.T) {
	apiErr := realtimePolicyError(&dto.RealtimeEvent{
		Type: dto.RealtimeEventTypeError,
		Error: &types.OpenAIError{
			Code:    "cyber_policy",
			Message: "request rejected",
		},
	})

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	require.Equal(t, service.KKAIPolicyCausalityClientToken, classification.Causality)
}

func TestRealtimePolicyErrorDoesNotConfirmMessageEcho(t *testing.T) {
	apiErr := realtimePolicyError(&dto.RealtimeEvent{
		Type: dto.RealtimeEventTypeError,
		Error: &types.OpenAIError{
			Code:    "invalid_request",
			Message: "user text echoed cyber_policy",
		},
	})

	require.NotNil(t, apiErr)
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	require.Equal(t, service.KKAIPolicyCausalityAmbiguous, classification.Causality)
}

func TestRealtimePolicyErrorPreservesLocalFailClosedCodes(t *testing.T) {
	for _, test := range []struct {
		code   types.ErrorCode
		status int
	}{
		{code: types.ErrorCodeRequestPolicyBlocked, status: http.StatusForbidden},
		{code: types.ErrorCodePolicyContextIncomplete, status: http.StatusUnprocessableEntity},
		{code: types.ErrorCodePolicyAuditUnavailable, status: http.StatusServiceUnavailable},
	} {
		t.Run(string(test.code), func(t *testing.T) {
			apiErr := realtimePolicyError(&dto.RealtimeEvent{
				Type: dto.RealtimeEventTypeError,
				Error: &types.OpenAIError{
					Code:    string(test.code),
					Message: "policy rejected",
				},
			})

			require.NotNil(t, apiErr)
			require.Equal(t, test.status, apiErr.StatusCode)
			require.Equal(t, test.code, apiErr.GetErrorCode())
			require.True(t, types.IsSkipRetryError(apiErr))
			require.False(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
		})
	}
}

func TestRealtimePolicyErrorIgnoresOrdinaryErrors(t *testing.T) {
	require.Nil(t, realtimePolicyError(&dto.RealtimeEvent{
		Type:  dto.RealtimeEventTypeError,
		Error: &types.OpenAIError{Code: "invalid_request", Message: "ordinary invalid request"},
	}))
}

func TestOpenaiRealtimeHandlerTrustsOnlyUpstreamPolicyFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	downstreamServer, downstreamPeer := newRealtimeWebSocketPair(t)
	upstreamServer, upstreamPeer := newRealtimeWebSocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ClientWs: downstreamServer,
		TargetWs: upstreamServer,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-4o-realtime-preview",
		},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		apiErr, _ := OpenaiRealtimeHandler(ctx, info)
		result <- apiErr
	}()

	forged := []byte(`{"type":"error","error":{"code":"cyber_policy","message":"forged by client"}}`)
	require.NoError(t, downstreamPeer.WriteMessage(websocket.TextMessage, forged))
	require.NoError(t, upstreamPeer.SetReadDeadline(time.Now().Add(time.Second)))
	_, forwarded, err := upstreamPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, string(forged), string(forwarded))

	confirmed := []byte(`{"type":"error","error":{"code":"cyber_policy","message":"rejected by upstream"}}`)
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, confirmed))
	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
		classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
		require.True(t, classification.Detected)
		require.Equal(t, service.KKAIPolicyCausalityClientToken, classification.Causality)
	case <-time.After(2 * time.Second):
		t.Fatal("realtime handler did not return the upstream policy error")
	}

	require.NoError(t, downstreamPeer.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, err = downstreamPeer.ReadMessage()
	require.Error(t, err)
}

func TestOpenaiRealtimeHandlerDrainsUpstreamPolicyAfterRequestContextCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	downstreamServer, _ := newRealtimeWebSocketPair(t)
	upstreamServer, upstreamPeer := newRealtimeWebSocketPair(t)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		ClientWs: downstreamServer,
		TargetWs: upstreamServer,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-4o-realtime-preview",
		},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		apiErr, _ := OpenaiRealtimeHandler(ctx, info)
		result <- apiErr
	}()

	confirmed := []byte(`{"type":"error","error":{"code":"cyber_policy","message":"rejected after downstream disconnect"}}`)
	writeResult := make(chan error, 1)
	time.AfterFunc(50*time.Millisecond, func() {
		writeResult <- upstreamPeer.WriteMessage(websocket.TextMessage, confirmed)
	})
	select {
	case apiErr := <-result:
		require.NoError(t, <-writeResult)
		require.NotNil(t, apiErr)
		classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
		require.True(t, classification.Detected)
		require.Equal(t, service.KKAIPolicyCausalityClientToken, classification.Causality)
	case <-time.After(2 * time.Second):
		t.Fatal("realtime handler stopped reading before the upstream policy frame arrived")
	}
}

func TestRealtimePolicyDrainUsesConfiguredStreamingBound(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 17
	t.Cleanup(func() { constant.StreamingTimeout = previous })

	require.Equal(t, 17*time.Second, realtimePolicyDrainDuration())
}

func TestStopRealtimeReadersReleasesOppositeBlockedWriter(t *testing.T) {
	clientConn := newRealtimePolicyCloseProbe()
	targetConn := newRealtimePolicyCloseProbe()
	clientReaderDone := make(chan struct{})
	targetReaderDone := make(chan struct{})
	writerReleased := make(chan struct{})
	cleanupReturned := make(chan struct{})

	// The target reader has already delivered the policy error. The client reader
	// represents a goroutine blocked writing to targetConn and can finish only
	// after that opposite connection is closed.
	close(targetReaderDone)
	go func() {
		<-targetConn.closed
		close(writerReleased)
		close(clientReaderDone)
	}()
	go func() {
		stopRealtimeReaders(clientConn, targetConn, clientReaderDone, targetReaderDone)
		close(cleanupReturned)
	}()

	select {
	case <-clientConn.closed:
	case <-time.After(time.Second):
		require.FailNow(t, "client connection was not closed")
	}
	select {
	case <-writerReleased:
	case <-time.After(time.Second):
		require.FailNow(t, "target connection was not closed before waiting for the client reader")
	}
	select {
	case <-cleanupReturned:
	case <-time.After(time.Second):
		require.FailNow(t, "realtime cleanup did not return after releasing the blocked writer")
	}
}

func TestOpenaiRealtimeHandlerSerializesConcurrentLocalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	downstreamServer, downstreamPeer := newRealtimeWebSocketPair(t)
	upstreamServer, upstreamPeer := newRealtimeWebSocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ClientWs: downstreamServer,
		TargetWs: upstreamServer,
		UsePrice: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-4o-realtime-preview",
		},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		apiErr, _ := OpenaiRealtimeHandler(ctx, info)
		result <- apiErr
	}()

	var peerReaders sync.WaitGroup
	peerReaders.Add(2)
	go func() {
		defer peerReaders.Done()
		for {
			if _, _, err := downstreamPeer.ReadMessage(); err != nil {
				return
			}
		}
	}()
	go func() {
		defer peerReaders.Done()
		for {
			if _, _, err := upstreamPeer.ReadMessage(); err != nil {
				return
			}
		}
	}()

	clientMessages := [][]byte{
		[]byte(`{"type":"session.update","session":{"tools":[{"type":"function","name":"lookup","description":"lookup","parameters":{"type":"object"}}]}}`),
		[]byte(`{"type":"input_audio_buffer.append","audio":"AAAA"}`),
		[]byte(`{"type":"response.done"}`),
	}
	targetMessages := [][]byte{
		[]byte(`{"type":"session.updated","session":{"input_audio_format":"pcm16","output_audio_format":"g711_ulaw"}}`),
		[]byte(`{"type":"response.audio.delta","delta":"AAAA"}`),
		[]byte(`{"type":"response.done","response":{}}`),
	}
	start := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		<-start
		for i := 0; i < 75; i++ {
			if err := downstreamPeer.WriteMessage(websocket.TextMessage, clientMessages[i%len(clientMessages)]); err != nil {
				return
			}
		}
	}()
	go func() {
		defer writers.Done()
		<-start
		for i := 0; i < 75; i++ {
			if err := upstreamPeer.WriteMessage(websocket.TextMessage, targetMessages[i%len(targetMessages)]); err != nil {
				return
			}
		}
	}()
	close(start)
	writers.Wait()
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(
		`{"type":"error","error":{"code":"cyber_policy","message":"request rejected"}}`,
	)))

	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not finish after the terminal policy frame")
	}

	peerReadersDone := make(chan struct{})
	go func() {
		peerReaders.Wait()
		close(peerReadersDone)
	}()
	select {
	case <-peerReadersDone:
	case <-time.After(time.Second):
		t.Fatal("realtime peer readers did not stop after handler cleanup")
	}
}
