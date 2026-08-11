package channel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cancelOnFirstBodyWrite struct {
	gin.ResponseWriter
	cancel context.CancelFunc
	once   sync.Once
}

type cancelDuringHeaderAdaptor struct {
	channel.Adaptor
	cancel context.CancelFunc
}

func (a *cancelDuringHeaderAdaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	a.cancel()
	return a.Adaptor.SetupRequestHeader(c, req, info)
}

func (w *cancelOnFirstBodyWrite) Write(data []byte) (int, error) {
	written, err := w.ResponseWriter.Write(data)
	w.once.Do(w.cancel)
	return written, err
}

func (w *cancelOnFirstBodyWrite) WriteString(data string) (int, error) {
	written, err := w.ResponseWriter.WriteString(data)
	w.once.Do(w.cancel)
	return written, err
}

func TestDoApiRequestCancelsUpstreamBeforeResponseStarts(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		<-releaseUpstream
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() {
		close(releaseUpstream)
		upstream.Close()
	})
	service.InitHttpClient()

	requestContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		RequestURLPath: "/v1/images/generations", RelayMode: relayconstant.RelayModeImagesGenerations,
		StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: upstream.URL, ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	result := make(chan error, 1)
	go func() {
		resp, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-image-1"}`))
		if resp != nil {
			_ = resp.Body.Close()
		}
		result <- err
	}()
	<-requestContext.Done()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("request continued waiting for an upstream response after client cancellation")
	}
}

func TestDoApiRequestDoesNotDispatchWhenCallerAlreadyCanceled(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		RequestURLPath: "/v1/images/generations",
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		StartTime:      time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: upstream.URL,
			ChannelType:    constant.ChannelTypeOpenAI,
		},
	}

	resp, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-image-1"}`))
	require.Nil(t, resp)
	require.Error(t, err)
	require.Zero(t, requestCount.Load())
}

func TestDoApiRequestDoesNotDispatchWhenCallerCancelsDuringRequestSetup(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	requestContext, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		RequestURLPath: "/v1/images/generations",
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		StartTime:      time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: upstream.URL,
			ChannelType:    constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &cancelDuringHeaderAdaptor{Adaptor: &openai.Adaptor{}, cancel: cancel}

	resp, err := channel.DoApiRequest(adaptor, c, info, strings.NewReader(`{"model":"gpt-image-1"}`))
	require.Nil(t, resp)
	require.Error(t, err)
	require.Zero(t, requestCount.Load())
}

func TestDoApiRequestGeminiStreamDetectsPolicyAfterClientCancellation(t *testing.T) {
	previousStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 2
	t.Cleanup(func() { constant.StreamingTimeout = previousStreamingTimeout })

	downstreamCanceled := make(chan struct{})
	upstreamResult := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, err := writer.Write([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"visible\"}]}}]}\n\n"))
		if err != nil {
			upstreamResult <- err
			return
		}
		writer.(http.Flusher).Flush()

		<-downstreamCanceled
		time.Sleep(25 * time.Millisecond)
		if err := request.Context().Err(); err != nil {
			upstreamResult <- err
			return
		}
		_, err = writer.Write([]byte("data: {\"error\":{\"code\":403,\"message\":\"request rejected\",\"status\":\"PERMISSION_DENIED\",\"details\":[{\"@type\":\"type.googleapis.com/google.rpc.ErrorInfo\",\"reason\":\"cyber_policy\",\"domain\":\"sub2api\"}]}}\n\n"))
		upstreamResult <- err
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	c.Writer = &cancelOnFirstBodyWrite{
		ResponseWriter: c.Writer,
		cancel: func() {
			cancel()
			close(downstreamCanceled)
		},
	}
	info := &relaycommon.RelayInfo{
		StartTime:       time.Now(),
		IsStream:        true,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    upstream.URL,
			ChannelType:       constant.ChannelTypeGemini,
			ApiKey:            "test-key",
			UpstreamModelName: "gemini-test",
		},
	}

	resp, err := channel.DoApiRequest(&gemini.Adaptor{}, c, info, strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	usage, apiErr := gemini.GeminiChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("cyber_policy"), apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Equal(t, service.KKAIPolicyCausalityClientToken, service.ClassifyKKAIUpstreamPolicyError(apiErr).Causality)
	require.NoError(t, <-upstreamResult)
}
