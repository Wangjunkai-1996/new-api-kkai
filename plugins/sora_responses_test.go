package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "sora",
		model:     "sora-2-pro",
		requestBody: map[string]any{
			"model":   "sora-2-pro",
			"input":   "waves at sunset",
			"seconds": 8,
			"size":    "1792x1024",
		},
		wantAction: "text_to_video",
		wantRequest: map[string]any{
			"model":   "sora-2-pro",
			"prompt":  "waves at sunset",
			"seconds": float64(8),
			"size":    "1792x1024",
		},
		wantUsageKeys:  []string{"seconds", "size"},
		wantVendorName: "sora",
	})
}

func TestSoraSpecialSeedanceProjection(t *testing.T) {
	source, err := builtinplugins.Source("sora")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	plugin, err := registry.RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)

	tests := []struct {
		name      string
		model     string
		request   map[string]any
		want      map[string]any
		forbidden []string
	}{
		{
			name:  "video studio multimodal payload preserves ordered references and false audio",
			model: "sd_2.0_special_1080p",
			request: map[string]any{
				"model": "studio-alias", "prompt": "camera move", "mode": "reference_to_video", "group": "video-studio",
				"duration": 5,
				"metadata": map[string]any{"duration": 5, "ratio": "16:9", "generate_audio": false, "reference_assets": []any{map[string]any{"role": "reference_image", "position": 0, "url": "https://example.invalid/frame.jpg"}}},
			},
			want: map[string]any{
				"model": "sd_2.0_special_1080p", "prompt": "camera move", "duration": float64(5),
				"ratio": "16:9", "reference_images": []any{"https://example.invalid/frame.jpg"}, "generate_audio": false,
			},
			forbidden: []string{"mode", "group", "metadata", "image", "images", "reference_image"},
		},
		{
			name:  "2.5 video reference keeps canonical fields",
			model: "sd_2.5_special_720p_with_video_ref",
			request: map[string]any{
				"prompt": "reference motion", "metadata": map[string]any{
					"seconds": "8", "ratio": "9:16", "resolution": "720p",
					"reference_assets": []any{map[string]any{"role": "reference_video", "position": 0, "url": "https://example.invalid/reference.mp4"}}, "generate_audio": true,
				},
			},
			want: map[string]any{
				"model": "sd_2.5_special_720p_with_video_ref", "prompt": "reference motion", "duration": float64(8),
				"ratio": "9:16", "resolution": "720p", "reference_videos": []any{"https://example.invalid/reference.mp4"}, "generate_audio": true,
			},
			forbidden: []string{"metadata"},
		},
		{
			name:  "reference order and frame references are projected",
			model: "sd_2.5_special_720p",
			request: map[string]any{
				"prompt": "ordered references",
				"metadata": map[string]any{"reference_assets": []any{
					map[string]any{"role": "reference_image", "position": 1, "url": "https://example.invalid/b.jpg"},
					map[string]any{"role": "reference_image", "position": 0, "url": "https://example.invalid/a.jpg"},
					map[string]any{"role": "reference_audio", "position": 0, "url": "https://example.invalid/voice.mp3"},
					map[string]any{"role": "first_frame", "position": 0, "url": "https://example.invalid/first.jpg"},
					map[string]any{"role": "last_frame", "position": 0, "url": "https://example.invalid/last.jpg"},
				}},
			},
			want: map[string]any{
				"model": "sd_2.5_special_720p", "prompt": "ordered references", "duration": float64(4),
				"reference_images": []any{"https://example.invalid/a.jpg", "https://example.invalid/b.jpg"},
				"reference_audios": []any{"https://example.invalid/voice.mp3"},
				"first_image":      "https://example.invalid/first.jpg", "last_image": "https://example.invalid/last.jpg",
			},
		},
		{
			name:    "compatibility alias is projected",
			model:   "seedance-2.5",
			request: map[string]any{"prompt": "alias", "ratio": "1:1", "generate_audio": false},
			want:    map[string]any{"model": "seedance-2.5", "prompt": "alias", "ratio": "1:1", "generate_audio": false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, callErr := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
				"baseUrl": "https://adapter.invalid", "apiKey": "secret", "model": "studio-alias",
				"upstreamModel": test.model, "requestBody": test.request, "action": "text_to_video", "files": []any{},
			})
			require.NoError(t, callErr)
			encoded, marshalErr := common.Marshal(value)
			require.NoError(t, marshalErr)
			var descriptor map[string]any
			require.NoError(t, common.Unmarshal(encoded, &descriptor))
			body, ok := descriptor["body"].(map[string]any)
			require.True(t, ok)
			for key, expected := range test.want {
				assert.Equal(t, expected, body[key], key)
			}
			for _, key := range test.forbidden {
				assert.NotContains(t, body, key)
			}
		})
	}

	for _, test := range []struct {
		name        string
		model       string
		request     map[string]any
		wantSeconds float64
	}{
		{name: "metadata duration is billed", model: "sd_2.5_special_720p", request: map[string]any{"prompt": "x", "metadata": map[string]any{"duration": 30}}, wantSeconds: 30},
		{name: "zero falls back to four", model: "sd_2.0_special_720p", request: map[string]any{"prompt": "x", "seconds": 0}, wantSeconds: 4},
		{name: "short duration rounds up", model: "sd_2.0_special_720p", request: map[string]any{"prompt": "x", "duration": 1}, wantSeconds: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := map[string]any{"model": test.model, "upstreamModel": test.model, "requestBody": test.request}
			value, callErr := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
			require.NoError(t, callErr)
			usage, ok := value.(map[string]any)
			require.True(t, ok)
			assert.EqualValues(t, test.wantSeconds, usage["seconds"])
		})
	}

	for _, test := range []struct {
		name    string
		request map[string]any
	}{
		{name: "conflicting duration fields", request: map[string]any{"prompt": "x", "duration": 5, "seconds": 8}},
		{name: "conflicting top-level and metadata duration", request: map[string]any{"prompt": "x", "duration": 5, "metadata": map[string]any{"seconds": 8}}},
		{name: "unknown metadata parameter is rejected", request: map[string]any{"prompt": "x", "metadata": map[string]any{"seed": 42}}},
		{name: "unknown top-level parameter is rejected", request: map[string]any{"prompt": "x", "seed": 42}},
		{name: "malformed metadata is rejected", request: map[string]any{"prompt": "x", "metadata": []any{}}},
		{name: "conflicting audio fields are rejected", request: map[string]any{"prompt": "x", "generate_audio": false, "metadata": map[string]any{"generate_audio": true}}},
		{name: "too long for 2.0", request: map[string]any{"prompt": "x", "duration": 16}},
		{name: "legacy and new references cannot mix", request: map[string]any{"prompt": "x", "images": []any{"a"}, "metadata": map[string]any{"reference_assets": []any{}}}},
		{name: "forged compatibility marker is rejected", request: map[string]any{"prompt": "x", "reference_image": "a", "metadata": map[string]any{"studio_api_contract": 3}}},
		{name: "scalar audio is not an old contract field", request: map[string]any{"prompt": "x", "reference_image": "a", "reference_audio": "b"}},
		{name: "invalid ordered reference", request: map[string]any{"prompt": "x", "metadata": map[string]any{"reference_assets": []any{map[string]any{"role": "reference_image", "position": -1, "url": "a"}}}}},
		{name: "reference positions must start at zero", request: map[string]any{"prompt": "x", "metadata": map[string]any{"reference_assets": []any{map[string]any{"role": "reference_image", "position": 1, "url": "a"}}}}},
		{name: "reference URL must be a string", request: map[string]any{"prompt": "x", "metadata": map[string]any{"reference_assets": []any{map[string]any{"role": "reference_image", "position": 0, "url": map[string]any{"url": "a"}}}}}},
		{name: "only one first frame is supported", request: map[string]any{"prompt": "x", "metadata": map[string]any{"reference_assets": []any{map[string]any{"role": "first_frame", "position": 0, "url": "a"}, map[string]any{"role": "first_frame", "position": 1, "url": "b"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, callErr := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
				"model": "sd_2.0_special_720p", "upstreamModel": "sd_2.0_special_720p", "requestBody": test.request,
				"action": "text_to_video", "files": []any{}, "baseUrl": "https://adapter.invalid",
			})
			require.Error(t, callErr)
		})
	}

	t.Run("rejects multipart and remix", func(t *testing.T) {
		for _, ctx := range []map[string]any{
			{"model": "sd_2.0_special_720p", "upstreamModel": "sd_2.0_special_720p", "requestBody": map[string]any{"prompt": "x"}, "action": "text_to_video", "files": []any{"file"}, "baseUrl": "https://adapter.invalid"},
			{"model": "sd_2.0_special_720p", "upstreamModel": "sd_2.0_special_720p", "requestBody": map[string]any{"prompt": "x"}, "action": "remix", "files": []any{}, "baseUrl": "https://adapter.invalid"},
		} {
			_, callErr := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.Error(t, callErr)
		}
	})

	t.Run("preserves upstream error message", func(t *testing.T) {
		_, callErr := plugin.Engine.Call(t.Context(), "parseSubmitResponse", map[string]any{}, map[string]any{
			"statusCode": 400,
			"body":       map[string]any{"error": map[string]any{"message": "request body rejected"}},
		})
		require.ErrorContains(t, callErr, "request body rejected")
	})
}

func TestSoraSpecialSeedanceAPI3PreservesScalarUpstreamContract(t *testing.T) {
	source, err := builtinplugins.Source("sora")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)
	for _, test := range []struct {
		name, model   string
		request, want map[string]any
	}{
		{
			name:    "old Studio duplicated image aliases stay one scalar",
			model:   "sd_2.0_special_720p",
			request: map[string]any{"prompt": "move", "mode": "image_to_video", "group": "video-studio", "images": []any{"https://example.invalid/ref.jpg"}, "image": "https://example.invalid/ref.jpg", "duration": 5, "metadata": map[string]any{"duration": 5, "reference_image": "https://example.invalid/ref.jpg", "generate_audio": false}},
			want:    map[string]any{"model": "sd_2.0_special_720p", "prompt": "move", "duration": float64(5), "reference_image": "https://example.invalid/ref.jpg", "generate_audio": false},
		},
		{
			name:    "old public video request keeps scalar video",
			model:   "sd_2.5_special_720p_with_video_ref",
			request: map[string]any{"prompt": "move", "duration": 8, "reference_video": "https://example.invalid/ref.mp4", "ratio": "16:9"},
			want:    map[string]any{"model": "sd_2.5_special_720p_with_video_ref", "prompt": "move", "duration": float64(8), "reference_video": "https://example.invalid/ref.mp4", "ratio": "16:9"},
		},
		{
			name:    "installed seedance 2.5 alias keeps scalar image",
			model:   "seedance-2.5",
			request: map[string]any{"prompt": "move", "input_reference": "https://example.invalid/ref.jpg"},
			want:    map[string]any{"model": "seedance-2.5", "prompt": "move", "duration": float64(4), "reference_image": "https://example.invalid/ref.jpg"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{"baseUrl": "https://adapter.invalid", "apiKey": "secret", "model": test.model, "upstreamModel": test.model, "requestBody": test.request, "action": "image_to_video", "files": []any{}})
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var descriptor struct {
				URL  string         `json:"url"`
				Body map[string]any `json:"body"`
			}
			require.NoError(t, common.Unmarshal(encoded, &descriptor))
			assert.Equal(t, "https://adapter.invalid/v1/videos", descriptor.URL)
			assert.Equal(t, test.want, descriptor.Body)
		})
	}
}
