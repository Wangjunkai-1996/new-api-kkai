package sora

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specialVideoModel = "sd_2.0_special_1080p"

func newSoraJSONContext(t *testing.T, method string, path string, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func readSoraRequestBody(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(raw, &decoded))
	return decoded
}

func newSoraRelayInfo(upstreamModel string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: upstreamModel},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
}

func TestBuildRequestBodyProjectsVideoStudioRequest(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos/", `{
		"model":"video-studio-model",
		"prompt":"top-level prompt",
		"ratio":"16:9",
		"resolution":"1080p",
		"duration":8,
		"seconds":"8",
		"reference_image":"https://assets.example/reference.jpg",
		"input_reference":"https://assets.example/input.jpg",
		"reference_video":"https://assets.example/reference.mp4",
		"generate_audio":false,
		"group":"Seedance video",
		"mode":"image_to_video",
		"metadata":{"prompt":"metadata prompt","duration":99,"generate_audio":true},
		"images":["https://assets.example/reference.jpg"],
		"image":"https://assets.example/reference.jpg"
	}`)
	info := newSoraRelayInfo(specialVideoModel)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, info)
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"model":           specialVideoModel,
		"prompt":          "top-level prompt",
		"ratio":           "16:9",
		"resolution":      "1080p",
		"duration":        float64(8),
		"seconds":         "8",
		"reference_image": "https://assets.example/reference.jpg",
		"input_reference": "https://assets.example/input.jpg",
		"reference_video": "https://assets.example/reference.mp4",
		"generate_audio":  false,
	}, readSoraRequestBody(t, body))
}

func TestVideoStudioReferenceSecondsDriveBillingAndProjection(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"continue the movement",
		"seconds":5,
		"reference_video":"https://assets.example/reference.mp4",
		"group":"Seedance video",
		"mode":"image_to_video",
		"metadata":{
			"seconds":5,
			"reference_video":"https://assets.example/reference.mp4"
		}
	}`)
	info := newSoraRelayInfo("sd_2.0_special_1080p_with_video_ref")
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
	require.Equal(t, float64(5), adaptor.EstimateBilling(ctx, info)["seconds"])
	body, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"model":           "sd_2.0_special_1080p_with_video_ref",
		"prompt":          "continue the movement",
		"duration":        float64(5),
		"seconds":         float64(5),
		"reference_video": "https://assets.example/reference.mp4",
	}, readSoraRequestBody(t, body))
}

func TestSeedanceDurationFallbackAndValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    int
		wantErr bool
		model   string
	}{
		{name: "missing uses minimum", body: `{"model":"video-studio-model","prompt":"test"}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "null duration uses minimum", body: `{"model":"video-studio-model","prompt":"test","duration":null}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "empty duration uses minimum", body: `{"model":"video-studio-model","prompt":"test","duration":""}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "null seconds uses minimum", body: `{"model":"video-studio-model","prompt":"test","seconds":null}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "empty seconds uses minimum", body: `{"model":"video-studio-model","prompt":"test","seconds":""}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "zero duration uses seconds", body: `{"model":"video-studio-model","prompt":"test","duration":0,"seconds":8}`, want: 8, model: "sd_2.0_special_1080p"},
		{name: "short duration clamps", body: `{"model":"video-studio-model","prompt":"test","duration":2}`, want: 4, model: "sd_2.0_special_1080p"},
		{name: "mismatch rejected", body: `{"model":"video-studio-model","prompt":"test","duration":8,"seconds":9}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "v2.0 upper bound rejected", body: `{"model":"video-studio-model","prompt":"test","duration":16}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "negative rejected", body: `{"model":"video-studio-model","prompt":"test","duration":-1}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "fraction rejected", body: `{"model":"video-studio-model","prompt":"test","duration":5.5}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "invalid string rejected", body: `{"model":"video-studio-model","prompt":"test","duration":"abc"}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "leading zero string rejected", body: `{"model":"video-studio-model","prompt":"test","duration":"05"}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "signed string rejected", body: `{"model":"video-studio-model","prompt":"test","duration":"+5"}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "padded string rejected", body: `{"model":"video-studio-model","prompt":"test","duration":" 5 "}`, wantErr: true, model: "sd_2.0_special_1080p"},
		{name: "v2.5 upper bound rejected", body: `{"model":"video-studio-model","prompt":"test","seconds":31}`, wantErr: true, model: "seedance-2.5"},
		{name: "v2.5 accepts thirty", body: `{"model":"video-studio-model","prompt":"test","duration":30}`, want: 30, model: "seedance-2.5"},
		{name: "fast alias uses v2.0 bounds", body: `{"model":"video-studio-model","prompt":"test","duration":16}`, wantErr: true, model: "sd_2.0_fast_special_720p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", tt.body)
			info := newSoraRelayInfo(tt.model)
			adaptor := &TaskAdaptor{}
			err := adaptor.ValidateRequestAndSetAction(ctx, info)
			if tt.wantErr {
				require.NotNil(t, err)
				assert.Equal(t, http.StatusBadRequest, err.StatusCode)
				assert.Equal(t, "invalid_duration", err.Code)
				return
			}
			require.Nil(t, err)
			require.Equal(t, float64(tt.want), adaptor.EstimateBilling(ctx, info)["seconds"])
			body, bodyErr := adaptor.BuildRequestBody(ctx, info)
			require.NoError(t, bodyErr)
			assert.Equal(t, float64(tt.want), readSoraRequestBody(t, body)["duration"])
		})
	}
}

func TestEstimateBillingPreservesLegacySoraDuration(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/v1/videos", `{"model":"sora-2","prompt":"legacy","duration":7}`)
	info := newSoraRelayInfo("sora-2")
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
	assert.Equal(t, float64(7), adaptor.EstimateBilling(ctx, info)["seconds"])
	body, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	assert.Equal(t, float64(7), readSoraRequestBody(t, body)["duration"])
}

func TestSeedanceDurationValidationFollowsModelMapping(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/v1/videos", `{"model":"customer-video","prompt":"mapped","duration":16}`)
	ctx.Set("model_mapping", `{"customer-video":"sd_2.0_special_720p"}`)
	info := newSoraRelayInfo("customer-video")

	err := (&TaskAdaptor{}).ValidateRequestAndSetAction(ctx, info)
	require.NotNil(t, err)
	assert.Equal(t, "invalid_duration", err.Code)
}

func TestSeedanceSpecialRejectsMultipartBeforeGenericValidation(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", specialVideoModel))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	info := newSoraRelayInfo(specialVideoModel)

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(ctx, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_content_type", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "application/json")
}

func TestSeedanceSpecialRejectsRemixButLegacySoraStillAllowsIt(t *testing.T) {
	for _, test := range []struct {
		name        string
		model       string
		wantCode    string
		wantAllowed bool
	}{
		{name: "seedance special", model: specialVideoModel, wantCode: "unsupported_operation"},
		{name: "legacy sora", model: "sora-2", wantAllowed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/v1/videos/task/remix", `{"model":"client-model","prompt":"continue"}`)
			info := newSoraRelayInfo(test.model)
			info.Action = constant.TaskActionRemix

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(ctx, info)
			if test.wantAllowed {
				require.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Equal(t, test.wantCode, taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		})
	}
}

func TestVideoStudioMetadataParseFailureIsRejectedBeforeBilling(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"test prompt",
		"metadata":"{not-json}"
	}`)
	info := newSoraRelayInfo(specialVideoModel)

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(ctx, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_metadata", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

func TestDurationModelMatchingOnlyUsesPublicAliases(t *testing.T) {
	for _, model := range []string{
		"customer-sd_2.0_special-model",
		"customer-seedance-2.5-model",
		"sd_2.0_special",
		"sd_2.0_fast_special",
		"sd_2.5_special_v1",
	} {
		t.Run(model, func(t *testing.T) {
			_, _, supported := durationBoundsForModel(model)
			assert.False(t, supported)
		})
	}
}

func TestBuildRequestBodyProjectsEveryVideoStudioModel(t *testing.T) {
	models := []string{
		"sd_2.0_special_1080p",
		"seedance-2.5",
		"future-video-model",
	}
	for _, modelName := range models {
		t.Run(modelName, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
				"model":"video-studio-model",
				"prompt":"test prompt",
				"group":"Seedance video"
			}`)

			body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(modelName))
			require.NoError(t, err)

			expected := map[string]any{
				"model":  modelName,
				"prompt": "test prompt",
			}
			if modelName != "future-video-model" {
				expected["duration"] = float64(4)
			}
			assert.Equal(t, expected, readSoraRequestBody(t, body))
		})
	}
}

func TestBuildRequestBodyPreservesConfiguredVideoStudioFields(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"test prompt",
		"content":[{"type":"text","text":"configured input"}],
		"size":"1920x1080",
		"future_option":"preserved"
	}`)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo("seedance-2.5"))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"model":    "seedance-2.5",
		"prompt":   "test prompt",
		"duration": float64(4),
		"content": []any{map[string]any{
			"type": "text",
			"text": "configured input",
		}},
		"size":          "1920x1080",
		"future_option": "preserved",
	}, readSoraRequestBody(t, body))
}

func TestBuildRequestBodyRejectsNullVideoStudioRequest(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `null`)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo("seedance-2.5"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request body must be an object")
	assert.Nil(t, body)
}

func TestBuildRequestBodyPreservesReferenceFieldsWithoutProjectionMetadata(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected map[string]any
	}{
		{
			name: "image",
			body: `{"model":"video-studio-model","prompt":"test prompt","image":"https://assets.example/input.jpg"}`,
			expected: map[string]any{
				"model": "seedance-2.5", "prompt": "test prompt", "duration": float64(4),
				"image": "https://assets.example/input.jpg",
			},
		},
		{
			name: "images",
			body: `{"model":"video-studio-model","prompt":"test prompt","images":["https://assets.example/input.jpg"]}`,
			expected: map[string]any{
				"model": "seedance-2.5", "prompt": "test prompt", "duration": float64(4),
				"images": []any{"https://assets.example/input.jpg"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", test.body)

			body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo("seedance-2.5"))

			require.NoError(t, err)
			assert.Equal(t, test.expected, readSoraRequestBody(t, body))
		})
	}
}

func TestBuildRequestBodyDropsReferenceAliasesWithCanonicalReference(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"test prompt",
		"reference_image":"https://assets.example/canonical.jpg",
		"image":"https://assets.example/alias.jpg",
		"images":["https://assets.example/alias-list.jpg"],
		"metadata":{"reference_image":"https://assets.example/canonical.jpg"}
	}`)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(specialVideoModel))
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"model":           specialVideoModel,
		"prompt":          "test prompt",
		"duration":        float64(4),
		"reference_image": "https://assets.example/canonical.jpg",
	}, readSoraRequestBody(t, body))
}

func TestBuildRequestBodyPreservesConfiguredCustomReferenceKeys(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected map[string]any
	}{
		{
			name: "image request key",
			body: `{
				"model":"video-studio-model",
				"prompt":"test prompt",
				"image":"https://assets.example/reference.jpg",
				"images":["https://assets.example/reference.jpg"],
				"metadata":{"image":"https://assets.example/reference.jpg"}
			}`,
			expected: map[string]any{
				"model": "future-video-model", "prompt": "test prompt",
				"image": "https://assets.example/reference.jpg",
			},
		},
		{
			name: "first and last frame request keys",
			body: `{
				"model":"video-studio-model",
				"prompt":"test prompt",
				"first_frame":"https://assets.example/first.jpg",
				"last_frame":"https://assets.example/last.jpg",
				"images":["https://assets.example/first.jpg","https://assets.example/last.jpg"],
				"metadata":{
					"first_frame":"https://assets.example/first.jpg",
					"last_frame":"https://assets.example/last.jpg"
				}
			}`,
			expected: map[string]any{
				"model": "future-video-model", "prompt": "test prompt",
				"first_frame": "https://assets.example/first.jpg",
				"last_frame":  "https://assets.example/last.jpg",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", test.body)

			body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo("future-video-model"))

			require.NoError(t, err)
			assert.Equal(t, test.expected, readSoraRequestBody(t, body))
		})
	}
}

func TestBuildRequestBodyLeavesNonVideoStudioRequestsUnchanged(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		path          string
		upstreamModel string
	}{
		{name: "direct special request", method: http.MethodPost, path: "/v1/videos", upstreamModel: specialVideoModel},
		{name: "non-submit playground method", method: http.MethodGet, path: "/pg/videos", upstreamModel: specialVideoModel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, test.method, test.path, `{
				"model":"client-model",
				"prompt":"test prompt",
				"group":"preserved",
				"future_option":"preserved"
			}`)

			body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(test.upstreamModel))
			require.NoError(t, err)

			expected := map[string]any{
				"model":         test.upstreamModel,
				"prompt":        "test prompt",
				"group":         "preserved",
				"future_option": "preserved",
			}
			if test.upstreamModel == specialVideoModel {
				expected["duration"] = float64(4)
			}
			assert.Equal(t, expected, readSoraRequestBody(t, body))
		})
	}
}

func TestSeedance25VideoStudioRequestPassesStrictMockAdapter(t *testing.T) {
	service.InitHttpClient()

	type observedRequest struct {
		method        string
		path          string
		contentType   string
		authorization string
		payload       map[string]any
		invalidField  string
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		result := observedRequest{
			method:        request.Method,
			path:          request.URL.Path,
			contentType:   request.Header.Get("Content-Type"),
			authorization: request.Header.Get("Authorization"),
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			result.invalidField = "body"
		} else if err := common.Unmarshal(raw, &result.payload); err != nil {
			result.invalidField = "json"
		} else {
			allowed := map[string]bool{
				"model": true, "prompt": true, "duration": true, "ratio": true, "resolution": true, "reference_image": true,
			}
			for field := range result.payload {
				if !allowed[field] {
					result.invalidField = field
					break
				}
			}
		}
		observed <- result
		if result.invalidField != "" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"strict-adapter-task"}`))
	}))
	t.Cleanup(server.Close)

	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"strict adapter request",
		"duration":30,
		"ratio":"16:9",
		"resolution":"720p",
		"reference_image":"https://assets.example/reference.jpg",
		"group":"Seedance video",
		"mode":"image_to_video",
		"metadata":{"duration":30,"ratio":"16:9","resolution":"720p"}
	}`)
	info := newSoraRelayInfo("seedance-2.5")
	info.ChannelBaseUrl = server.URL
	info.ApiKey = "strict-adapter-key"
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
	assert.Equal(t, map[string]float64{
		"seconds": 30,
		"size":    1,
	}, adaptor.EstimateBilling(ctx, info))
	body, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	response, err := adaptor.DoRequest(ctx, info, body)
	require.NoError(t, err)
	require.NotNil(t, response)
	t.Cleanup(func() { _ = response.Body.Close() })
	assert.Equal(t, http.StatusOK, response.StatusCode)

	request := <-observed
	assert.Empty(t, request.invalidField)
	assert.Equal(t, http.MethodPost, request.method)
	assert.Equal(t, "/v1/videos", request.path)
	assert.Equal(t, "application/json", request.contentType)
	assert.Equal(t, "Bearer strict-adapter-key", request.authorization)
	assert.Equal(t, map[string]any{
		"model":           "seedance-2.5",
		"prompt":          "strict adapter request",
		"duration":        float64(30),
		"ratio":           "16:9",
		"resolution":      "720p",
		"reference_image": "https://assets.example/reference.jpg",
	}, request.payload)
}

func TestSoraBuildRequestBodyReturnsReplayablePassThroughBody(t *testing.T) {
	payload := []byte("opaque-sora-request-body")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/octet-stream")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	replayable, ok := body.(common.ReplayableBody)
	require.True(t, ok)

	sent, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, sent)
	assert.EqualValues(t, len(payload), replayable.Size())

	replayBody, err := replayable.NewReader()
	require.NoError(t, err)
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)
}
