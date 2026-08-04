package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenaiImageJSONBridgeProducesStandardResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		path           string
		responseFormat string
		terminal       string
		wantField      string
		wantValue      string
		wantImages     int
	}{
		{
			name:       "generation base64 with done",
			path:       "/v1/images/generations",
			terminal:   "data: [DONE]\n\n",
			wantField:  "b64_json",
			wantValue:  "final-one",
			wantImages: 2,
		},
		{
			name:           "edit URL with EOF",
			path:           "/v1/images/edits",
			responseFormat: "url",
			wantField:      "url",
			wantValue:      "https://img.example/final-one.png",
			wantImages:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Join([]string{
				`event: image_generation.partial_image`,
				`data: {"type":"image_generation.partial_image","b64_json":"partial-secret"}`,
				``,
				`data: {"type":"image_generation.completed","created_at":1710000001,"b64_json":"final-one","url":"https://img.example/final-one.png","revised_prompt":"refined","background":"opaque","output_format":"png","quality":"high","size":"2048x2048","model":"gpt-image-2","metadata":{"provider":"sub2"},"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`,
				``,
				`data: {"type":"image_edit.completed","created_at":1710000001,"b64_json":"final-two","url":"https://img.example/final-two.png","usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`,
				``,
				tt.terminal,
			}, "\n")

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
			info := &relaycommon.RelayInfo{
				IsStream:         false,
				UpstreamIsStream: true,
				Request: &dto.ImageRequest{
					ResponseFormat: tt.responseFormat,
				},
				ChannelMeta: &relaycommon.ChannelMeta{},
			}
			info.PriceData.UsePrice = true
			info.PriceData.AddOtherRatio("n", 3)

			usage, bridgeErr := OpenaiImageJSONBridgeHandler(c, info, resp)

			require.Nil(t, bridgeErr)
			require.NotNil(t, usage)
			assert.Equal(t, 3, usage.PromptTokens)
			assert.Equal(t, 4, usage.CompletionTokens)
			assert.Equal(t, 7, usage.TotalTokens)
			assert.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
			assert.False(t, info.IsStream)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache, no-transform", recorder.Header().Get("Cache-Control"))
			assert.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
			assert.Empty(t, recorder.Header().Get("Content-Length"))
			assert.Empty(t, recorder.Header().Get("Transfer-Encoding"))
			assert.True(t, recorder.Flushed)
			assert.True(t, strings.HasPrefix(recorder.Body.String(), " "))
			assert.NotContains(t, recorder.Body.String(), "event:")
			assert.NotContains(t, recorder.Body.String(), "data:")
			assert.NotContains(t, recorder.Body.String(), "partial-secret")

			var got struct {
				Created      int64            `json:"created"`
				Data         []map[string]any `json:"data"`
				Usage        dto.Usage        `json:"usage"`
				Background   string           `json:"background"`
				OutputFormat string           `json:"output_format"`
				Quality      string           `json:"quality"`
				Size         string           `json:"size"`
				Model        string           `json:"model"`
				Metadata     map[string]any   `json:"metadata"`
			}
			require.NoError(t, common.Unmarshal([]byte(strings.TrimSpace(recorder.Body.String())), &got))
			assert.Equal(t, int64(1710000001), got.Created)
			require.Len(t, got.Data, tt.wantImages)
			assert.Equal(t, tt.wantValue, got.Data[0][tt.wantField])
			assert.Equal(t, "refined", got.Data[0]["revised_prompt"])
			if tt.responseFormat == "url" {
				assert.NotContains(t, got.Data[0], "b64_json")
			} else {
				assert.NotContains(t, got.Data[0], "url")
			}
			assert.Equal(t, 7, got.Usage.TotalTokens)
			assert.Equal(t, "opaque", got.Background)
			assert.Equal(t, "png", got.OutputFormat)
			assert.Equal(t, "high", got.Quality)
			assert.Equal(t, "2048x2048", got.Size)
			assert.Equal(t, "gpt-image-2", got.Model)
			assert.Equal(t, "sub2", got.Metadata["provider"])
		})
	}
}

type imageBridgeSignalWriter struct {
	gin.ResponseWriter
	writes chan string
}

func (w *imageBridgeSignalWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	select {
	case w.writes <- string(append([]byte(nil), p...)):
	default:
	}
	return n, err
}

func (w *imageBridgeSignalWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	select {
	case w.writes <- s:
	default:
	}
	return n, err
}

func TestOpenaiImageJSONBridgeFlushesKeepalivesFromSingleWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	signals := make(chan string, 8)
	c.Writer = &imageBridgeSignalWriter{ResponseWriter: c.Writer, writes: signals}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	ticks := make(chan time.Time, 2)
	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	done := make(chan result, 1)
	go func() {
		usage, bridgeErr := openaiImageJSONBridge(c, info, resp, ticks)
		done <- result{usage: usage, err: bridgeErr}
	}()

	require.Equal(t, " ", <-signals, "the bridge must flush valid JSON whitespace immediately")
	ticks <- time.Now()
	require.Equal(t, "\n", <-signals)
	ticks <- time.Now()
	require.Equal(t, "\n", <-signals)
	_, err := io.WriteString(writer, "data: {\"type\":\"image_generation.completed\",\"created_at\":1710000002,\"b64_json\":\"final\"}\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bridgeResult := <-done
	require.Nil(t, bridgeResult.err)
	require.NotNil(t, bridgeResult.usage)
	assert.JSONEq(t, `{"created":1710000002,"data":[{"b64_json":"final"}]}`, strings.TrimSpace(recorder.Body.String()))
}

func TestOpenaiImageJSONBridgeReturnsCommittedJSONErrorWithoutRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"type":"image_generation.partial_image","b64_json":"partial-secret"}`,
		``,
		`data: {"type":"image_generation.completed","created_at":1710000004,"b64_json":"must-not-be-committed"}`,
		``,
		`data: {"type":"error","error":{"type":"server_error","code":"server_error","message":"image failed after partial output"}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", false)
	info.UpstreamIsStream = true
	info.Request = &dto.ImageRequest{}

	usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

	require.Nil(t, usage)
	require.NotNil(t, bridgeErr)
	assert.True(t, types.IsSkipRetryError(bridgeErr))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, " ", recorder.Body.String())
	c.JSON(bridgeErr.StatusCode, gin.H{"error": bridgeErr.ToOpenAIError()})
	assert.Equal(t, http.StatusOK, recorder.Code, "the flushed status cannot be rewritten")
	assert.NotContains(t, recorder.Body.String(), "event:")
	assert.NotContains(t, recorder.Body.String(), "data:")
	assert.NotContains(t, recorder.Body.String(), "partial-secret")
	assert.NotContains(t, recorder.Body.String(), "must-not-be-committed")
	var errorBody struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal([]byte(strings.TrimSpace(recorder.Body.String())), &errorBody))
	assert.Equal(t, "image failed after partial output", errorBody.Error.Message)
	assert.Equal(t, "server_error", errorBody.Error.Type)
}

type imageBridgeReadError struct {
	sent bool
}

func (r *imageBridgeReadError) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("upstream read failed")
	}
	r.sent = true
	return copy(p, []byte("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n")), nil
}

func (r *imageBridgeReadError) Close() error { return nil }

func TestOpenaiImageJSONBridgeRejectsIncompleteStreams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body io.ReadCloser
	}{
		{name: "done without completed image", body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))},
		{name: "EOF without completed image", body: io.NopCloser(strings.NewReader("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n"))},
		{name: "scanner failure", body: &imageBridgeReadError{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: tt.body}
			info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}

			usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

			require.Nil(t, usage)
			require.NotNil(t, bridgeErr)
			assert.True(t, types.IsSkipRetryError(bridgeErr))
			assert.Equal(t, " ", recorder.Body.String())
		})
	}
}

func TestOpenaiImageJSONBridgeStopsUpstreamWhenClientDisconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	signals := make(chan string, 2)
	c.Writer = &imageBridgeSignalWriter{ResponseWriter: c.Writer, writes: signals}
	body := &blockingBody{
		chunk:  []byte("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n"),
		closed: make(chan struct{}),
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	done := make(chan result, 1)
	go func() {
		usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)
		done <- result{usage: usage, err: bridgeErr}
	}()

	require.Equal(t, " ", <-signals)
	cancel()
	bridgeResult := <-done
	require.Nil(t, bridgeResult.usage)
	require.NotNil(t, bridgeResult.err)
	assert.True(t, types.IsSkipRetryError(bridgeResult.err))
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	select {
	case <-body.closed:
	default:
		t.Fatal("upstream response body was not closed")
	}
}

func TestOpenaiImageJSONBridgeDoesNotCommitNon2xxResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"message":"upstream unavailable","type":"upstream_error","code":"unavailable"}}`
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", false)
	resp.StatusCode = http.StatusBadGateway
	info.UpstreamIsStream = true

	usage, bridgeErr := OpenaiImageJSONBridgeHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, bridgeErr)
	assert.Equal(t, http.StatusBadGateway, bridgeErr.StatusCode)
	assert.Empty(t, recorder.Body.String())
	assert.False(t, recorder.Flushed)
}

func TestOpenaiImageDoResponseSeparatesClientAndUpstreamStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := "data: {\"type\":\"image_generation.completed\",\"created_at\":1710000003,\"b64_json\":\"final\"}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", false)
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.UpstreamIsStream = true
	info.Request = &dto.ImageRequest{}

	usage, bridgeErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, bridgeErr)
	require.NotNil(t, usage)
	assert.False(t, info.IsStream)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"created":1710000003,"data":[{"b64_json":"final"}]}`, strings.TrimSpace(recorder.Body.String()))
}
