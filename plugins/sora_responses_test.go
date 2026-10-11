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
			name:  "video studio 2.0 payload uses metadata values and keeps false audio",
			model: "sd_2.0_special_1080p",
			request: map[string]any{
				"model": "studio-alias", "prompt": "camera move", "mode": "text_to_video", "group": "video-studio",
				"duration": 5, "image": "https://example.invalid/frame.jpg", "images": []any{"https://example.invalid/frame.jpg"},
				"metadata": map[string]any{"duration": 5, "ratio": "16:9", "generate_audio": false, "image": "https://example.invalid/frame.jpg"},
			},
			want: map[string]any{
				"model": "sd_2.0_special_1080p", "prompt": "camera move", "duration": float64(5),
				"ratio": "16:9", "reference_image": "https://example.invalid/frame.jpg", "generate_audio": false,
			},
			forbidden: []string{"mode", "group", "metadata", "image", "images"},
		},
		{
			name:  "2.5 video reference keeps canonical fields",
			model: "sd_2.5_special_720p_with_video_ref",
			request: map[string]any{
				"prompt": "reference motion", "metadata": map[string]any{
					"seconds": "8", "ratio": "9:16", "resolution": "720p",
					"reference_video": "https://example.invalid/reference.mp4", "generate_audio": true,
				},
			},
			want: map[string]any{
				"model": "sd_2.5_special_720p_with_video_ref", "prompt": "reference motion", "duration": float64(8),
				"ratio": "9:16", "resolution": "720p", "reference_video": "https://example.invalid/reference.mp4", "generate_audio": true,
			},
			forbidden: []string{"metadata"},
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
		{name: "too long for 2.0", request: map[string]any{"prompt": "x", "duration": 16}},
		{name: "multiple reference images", request: map[string]any{"prompt": "x", "images": []any{"a", "b"}}},
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
