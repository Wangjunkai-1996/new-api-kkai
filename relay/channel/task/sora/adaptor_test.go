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

func TestBuildRequestBodyProjectsVideoStudioSpecialRequest(t *testing.T) {
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

func TestBuildRequestBodyProjectsEverySpecialVideoModel(t *testing.T) {
	models := []string{
		"sd_2.0_fast_special_720p",
		"sd_2.0_special_720p",
		"sd_2.0_special_1080p",
		"sd_2.0_special_2k",
		"sd_2.0_special_4k",
		"sd_2.0_fast_special_720p_with_video_ref",
		"sd_2.0_special_720p_with_video_ref",
		"sd_2.0_special_1080p_with_video_ref",
		"sd_2.0_special_2k_with_video_ref",
		"sd_2.0_special_4k_with_video_ref",
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

func TestBuildRequestBodyRejectsUnknownVideoStudioSpecialField(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"test prompt",
		"future_option":"must not be silently discarded"
	}`)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(specialVideoModel))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "future_option")
	assert.Nil(t, body)
}

func TestBuildRequestBodyRejectsUnsupportedVideoStudioSpecialFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
		body  string
	}{
		{
			name:  "content",
			field: "content",
			body:  `{"model":"video-studio-model","prompt":"test prompt","content":"real input"}`,
		},
		{
			name:  "size",
			field: "size",
			body:  `{"model":"video-studio-model","prompt":"test prompt","size":"1920x1080"}`,
		},
		{
			name:  "image without canonical reference",
			field: "image",
			body:  `{"model":"video-studio-model","prompt":"test prompt","image":"https://assets.example/input.jpg"}`,
		},
		{
			name:  "images without canonical reference",
			field: "images",
			body:  `{"model":"video-studio-model","prompt":"test prompt","images":["https://assets.example/input.jpg"]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", test.body)

			body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(specialVideoModel))

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.field)
			assert.Nil(t, body)
		})
	}
}

func TestBuildRequestBodyDropsReferenceAliasesWithCanonicalReference(t *testing.T) {
	ctx := newSoraJSONContext(t, http.MethodPost, "/pg/videos", `{
		"model":"video-studio-model",
		"prompt":"test prompt",
		"reference_image":"https://assets.example/canonical.jpg",
		"image":"https://assets.example/alias.jpg",
		"images":["https://assets.example/alias-list.jpg"]
	}`)

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, newSoraRelayInfo(specialVideoModel))
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"model":           specialVideoModel,
		"prompt":          "test prompt",
		"reference_image": "https://assets.example/canonical.jpg",
	}, readSoraRequestBody(t, body))
}

func TestBuildRequestBodyLeavesNonSpecialRequestsUnchanged(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		path          string
		upstreamModel string
	}{
		{name: "ordinary Sora model", method: http.MethodPost, path: "/pg/videos", upstreamModel: "sora-2"},
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

func TestVideoStudioSpecialRequestPassesStrictMockAdapter(t *testing.T) {
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
				"model": true, "prompt": true, "duration": true, "ratio": true, "generate_audio": true,
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
		"duration":5,
		"ratio":"16:9",
		"generate_audio":false,
		"group":"Seedance video",
		"mode":"text_to_video",
		"metadata":{"duration":5,"ratio":"16:9","generate_audio":false}
	}`)
	info := newSoraRelayInfo(specialVideoModel)
	info.ChannelBaseUrl = server.URL
	info.ApiKey = "strict-adapter-key"
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

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
		"model":          specialVideoModel,
		"prompt":         "strict adapter request",
		"duration":       float64(5),
		"ratio":          "16:9",
		"generate_audio": false,
	}, request.payload)
}
