package sora

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
		"seconds":         float64(5),
		"reference_video": "https://assets.example/reference.mp4",
	}, readSoraRequestBody(t, body))
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

			assert.Equal(t, map[string]any{
				"model":  modelName,
				"prompt": "test prompt",
			}, readSoraRequestBody(t, body))
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
		"model":  "seedance-2.5",
		"prompt": "test prompt",
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
				"model": "seedance-2.5", "prompt": "test prompt",
				"image": "https://assets.example/input.jpg",
			},
		},
		{
			name: "images",
			body: `{"model":"video-studio-model","prompt":"test prompt","images":["https://assets.example/input.jpg"]}`,
			expected: map[string]any{
				"model": "seedance-2.5", "prompt": "test prompt",
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

			assert.Equal(t, map[string]any{
				"model":         test.upstreamModel,
				"prompt":        "test prompt",
				"group":         "preserved",
				"future_option": "preserved",
			}, readSoraRequestBody(t, body))
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
