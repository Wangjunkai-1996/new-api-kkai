package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

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
			assert.True(t, strings.HasPrefix(recorder.Body.String(), "{"))
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
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &got))
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
	writes    chan string
	deadlines chan time.Time
}

type imageBridgeFailingWriter struct {
	gin.ResponseWriter
	err error
}

func (w *imageBridgeFailingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *imageBridgeFailingWriter) WriteString(string) (int, error) {
	return 0, w.err
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

func (w *imageBridgeSignalWriter) SetWriteDeadline(deadline time.Time) error {
	if w.deadlines != nil {
		w.deadlines <- deadline
	}
	return nil
}

func TestOpenaiImageJSONBridgeKeepalivesDoNotCommitSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	signals := make(chan string, 8)
	deadlines := make(chan time.Time, 4)
	c.Writer = &imageBridgeSignalWriter{ResponseWriter: c.Writer, writes: signals, deadlines: deadlines}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	ticks := make(chan time.Time)
	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	done := make(chan result, 1)
	go func() {
		usage, bridgeErr := openaiImageJSONBridge(c, info, resp, ticks)
		done <- result{usage: usage, err: bridgeErr}
	}()

	ticks <- time.Now()
	<-deadlines
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Body.String())
	select {
	case write := <-signals:
		t.Fatalf("keepalive committed an HTTP response: %q", write)
	default:
	}
	ticks <- time.Now()
	<-deadlines
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Body.String())
	_, err := io.WriteString(writer, "data: {\"type\":\"image_generation.completed\",\"created_at\":1710000002,\"b64_json\":\"final\"}\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bridgeResult := <-done
	require.Nil(t, bridgeResult.err)
	require.NotNil(t, bridgeResult.usage)
	assert.JSONEq(t, `{"created":1710000002,"data":[{"b64_json":"final"}]}`, recorder.Body.String())
}

func TestOpenaiImageJSONBridgeSettlesCompletedImageBeforeTerminalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"type":"image_generation.partial_image","b64_json":"partial-secret"}`,
		``,
		`data: {"type":"image_generation.completed","created_at":1710000004,"b64_json":"completed-before-error"}`,
		``,
		`data: {"type":"error","error":{"type":"server_error","code":"server_error","message":"image failed after partial output"}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", false)
	info.UpstreamIsStream = true
	info.Request = &dto.ImageRequest{}

	usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

	require.Nil(t, bridgeErr)
	require.NotNil(t, usage)
	assert.True(t, c.Writer.Written())
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "event:")
	assert.NotContains(t, recorder.Body.String(), "data:")
	assert.NotContains(t, recorder.Body.String(), "partial-secret")
	assert.JSONEq(t, `{"created":1710000004,"data":[{"b64_json":"completed-before-error"}]}`, recorder.Body.String())
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
}

func TestOpenaiImageJSONBridgeSettlesCompletedImageWithMalformedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"type":"image_generation.completed","created_at":1710000006,"b64_json":"final","usage":{"total_tokens":"invalid"}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", false)
	info.UpstreamIsStream = true
	info.Request = &dto.ImageRequest{}

	usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

	require.Nil(t, bridgeErr)
	require.NotNil(t, usage)
	assert.Zero(t, usage.TotalTokens)
	assert.JSONEq(t, `{"created":1710000006,"data":[{"b64_json":"final"}]}`, recorder.Body.String())
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
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
			assert.False(t, c.Writer.Written())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiImageJSONBridgeDiagnosticsAreBoundedAndRedacted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	promptSecret := "prompt-super-secret"
	b64Secret := "base64-super-secret"
	urlSecret := "https://img.example/private.png?token=url-super-secret"
	providerSecret := "arbitrary-provider-payload-secret"
	typeSecret := "event-type-secret"
	diagnosticItems := make([]map[string]any, 0, 18)
	for index := 0; index < 18; index++ {
		diagnosticItems = append(diagnosticItems, map[string]any{
			"b64_json": strings.Repeat("i", index+1),
			"url":      fmt.Sprintf("https://img.example/diagnostic/%d?token=item-secret", index),
		})
	}
	partialEvent, err := common.Marshal(map[string]any{
		"type":        "image_generation.partial_image",
		"prompt":      promptSecret,
		"b64_json":    b64Secret,
		"url":         urlSecret,
		"provider":    map[string]any{"payload": providerSecret},
		"image_index": 0,
		"data":        diagnosticItems,
	})
	require.NoError(t, err)
	invalidTypeEvent, err := common.Marshal(map[string]any{
		"type":          "invalid/type/" + typeSecret,
		"provider_blob": providerSecret,
	})
	require.NoError(t, err)
	var body strings.Builder
	fmt.Fprintf(&body, "data: %s\n\ndata: %s\n\n", partialEvent, invalidTypeEvent)
	for index := 0; index < 16; index++ {
		event, marshalErr := common.Marshal(map[string]any{
			"type":                           fmt.Sprintf("image_generation.diagnostic_%02d", index),
			"b64_json":                       strings.Repeat("b", index+1),
			"url":                            fmt.Sprintf("https://img.example/diagnostic-event/%d", index),
			fmt.Sprintf("field_%02d", index): providerSecret,
		})
		require.NoError(t, marshalErr)
		fmt.Fprintf(&body, "data: %s\n\n", event)
	}
	body.WriteString("data: [DONE]\n\n")
	c, _, resp, info := newImageTestContext(t, body.String(), "text/event-stream", false)
	c.Set(common.RequestIdKey, "req-image-json-bridge")
	info.Request = &dto.ImageRequest{}

	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

	require.Nil(t, usage)
	require.NotNil(t, bridgeErr)
	logs := logBuffer.String()
	assert.Contains(t, logs, "req-image-json-bridge")
	assert.Contains(t, logs, "reason=done client_gone=false events=18 completed_events=0 completed_images=0 data_items=18")
	assert.Contains(t, logs, "<invalid>:1")
	assert.Contains(t, logs, "image_generation.partial_image:1")
	assert.Contains(t, logs, "fields=[b64_json:33,data:1,field_00:1,field_01:1,field_02:1,field_03:1,image_index:1,prompt:1,provider:1,provider_blob:1,type:18,url:33]")
	assert.Contains(t, logs, fmt.Sprintf("b64_json_lengths=[%d 1 2 3 4 5 6 7]", len(b64Secret)))
	assert.Contains(t, logs, "dropped_event_types=6 dropped_fields=12 dropped_b64_json_lengths=25 dropped_url_lengths=25 dropped_data_items=2")
	assert.NotContains(t, logs, "image_generation.diagnostic_10")
	assert.NotContains(t, logs, "field_04")
	assert.NotContains(t, logs, promptSecret)
	assert.NotContains(t, logs, b64Secret)
	assert.NotContains(t, logs, urlSecret)
	assert.NotContains(t, logs, providerSecret)
	assert.NotContains(t, logs, typeSecret)
	assert.NotContains(t, logs, "https://img.example/diagnostic/")
	assert.NotContains(t, logs, "https://img.example/diagnostic-event/")
}

func TestOpenaiImageJSONBridgeSettlesCompletedUpstreamAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
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

	_, err := io.WriteString(writer, "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n")
	require.NoError(t, err)
	cancel()
	_, err = io.WriteString(writer, "data: {\"type\":\"image_generation.completed\",\"created_at\":1710000004,\"b64_json\":\"final\",\"usage\":{\"total_tokens\":9}}\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bridgeResult := <-done
	require.NotNil(t, bridgeResult.usage)
	require.Nil(t, bridgeResult.err)
	assert.Equal(t, 9, bridgeResult.usage.TotalTokens)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.True(t, c.Writer.Written())
	assert.JSONEq(t, `{"created":1710000004,"data":[{"b64_json":"final"}],"usage":{"total_tokens":9}}`, recorder.Body.String())
}

func TestOpenaiImageJSONBridgeKeepsFailureRefundableAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
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

	_, err := io.WriteString(writer, "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n")
	require.NoError(t, err)
	cancel()
	_, err = io.WriteString(writer, "data: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bridgeResult := <-done
	require.Nil(t, bridgeResult.usage)
	require.NotNil(t, bridgeResult.err)
	assert.True(t, types.IsSkipRetryError(bridgeResult.err))
	assert.Equal(t, types.ErrorCodeEmptyResponse, bridgeResult.err.GetErrorCode())
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiImageJSONBridgeSettlesCompletedUpstreamWhenDownstreamWriteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	downstreamErr := errors.New("downstream connection closed")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Writer = &imageBridgeFailingWriter{ResponseWriter: c.Writer, err: downstreamErr}
	body := "data: {\"type\":\"image_generation.completed\",\"created_at\":1710000005,\"b64_json\":\"final\",\"usage\":{\"total_tokens\":9}}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ChannelMeta: &relaycommon.ChannelMeta{}}

	usage, bridgeErr := openaiImageJSONBridge(c, info, resp, nil)

	require.Nil(t, bridgeErr)
	require.NotNil(t, usage)
	assert.Equal(t, 9, usage.TotalTokens)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, downstreamErr)
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
