package common

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type copyFixture struct {
	Input json.RawMessage
	Tools json.RawMessage
	Flags map[string][]bool
	Extra map[string]json.RawMessage
	Items []json.RawMessage
}

func TestDeepCopyRawMessageIsolation(t *testing.T) {
	src := copyFixture{
		Input: json.RawMessage(`{"zero":0,"false":false}`),
		Tools: make(json.RawMessage, 0, 8),
		Flags: map[string][]bool{"enabled": {false, true}},
		Extra: map[string]json.RawMessage{"raw": json.RawMessage(`{"large":9007199254740993}`)},
	}
	got, err := DeepCopy(&src)
	require.NoError(t, err)
	assert.Equal(t, src, *got)
	assert.NotNil(t, got.Tools)
	got.Input[0] = '['
	got.Flags["enabled"][0] = true
	got.Extra["raw"][0] = '['
	assert.Equal(t, byte('{'), src.Input[0])
	assert.False(t, src.Flags["enabled"][0])
	assert.Equal(t, byte('{'), src.Extra["raw"][0])
}

func TestDeepCopyRawMessageContainerSemantics(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"nil", nil}, {"empty", json.RawMessage{}}, {"null", json.RawMessage(`null`)}, {"false", json.RawMessage(`false`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := copyFixture{Input: tc.raw, Extra: map[string]json.RawMessage{"value": tc.raw}, Items: []json.RawMessage{tc.raw}}
			got, err := DeepCopy(&src)
			require.NoError(t, err)
			assert.Equal(t, src, *got)
			encoded, err := Marshal(got)
			if tc.name == "empty" {
				assert.Error(t, err, "empty raw JSON must not become a valid null value")
				return
			}
			require.NoError(t, err)
			want, err := Marshal(src)
			require.NoError(t, err)
			assert.Equal(t, want, encoded)
		})
	}
}

func TestDeepCopyResponsesRequestPreservesJSONAndRetryIsolation(t *testing.T) {
	zero := uint(0)
	off := false
	temperature := float64(0)
	src := dto.OpenAIResponsesRequest{
		Model:           "gpt-6.1-sol",
		Input:           json.RawMessage(`[{"role":"user","content":"hello"}]`),
		Tools:           json.RawMessage(`[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]`),
		Metadata:        json.RawMessage(`{"large":9007199254740993,"zero":0,"off":false}`),
		Store:           json.RawMessage(`false`),
		Conversation:    json.RawMessage(`null`),
		MaxOutputTokens: &zero,
		Stream:          &off,
		Temperature:     &temperature,
		Reasoning:       &dto.Reasoning{Effort: "medium", Context: json.RawMessage(`{"budget":0}`)},
	}
	originalJSON, err := Marshal(&src)
	require.NoError(t, err)
	first, err := DeepCopy(&src)
	require.NoError(t, err)
	second, err := DeepCopy(&src)
	require.NoError(t, err)
	firstJSON, err := Marshal(first)
	require.NoError(t, err)
	assert.Equal(t, originalJSON, firstJSON)
	assert.Equal(t, src, *first)
	assert.Nil(t, first.Include)
	require.NotNil(t, first.MaxOutputTokens)
	require.NotNil(t, first.Stream)
	require.NotNil(t, first.Temperature)
	require.NotNil(t, first.Reasoning)
	first.Input[0] = '{'
	first.Tools[0] = '{'
	first.Metadata[0] = '['
	first.Store[0] = 't'
	first.Reasoning.Context[0] = '['
	first.Reasoning.Effort = "high"
	*first.MaxOutputTokens = 100
	*first.Stream = true
	*first.Temperature = 1
	assert.Equal(t, src, *second)
	afterJSON, err := Marshal(&src)
	require.NoError(t, err)
	assert.Equal(t, originalJSON, afterJSON)
}

func BenchmarkDeepCopyResponsesRequest(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"1KiB", 1 << 10}, {"768KiB", 768 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			src := dto.OpenAIResponsesRequest{
				Model: "gpt-6.1-sol",
				Input: json.RawMessage(`[{"role":"user","content":"` + strings.Repeat("x", size.bytes) + `"}]`),
			}
			b.Run("legacy", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(src.Input)))
				for i := 0; i < b.N; i++ {
					var dst dto.OpenAIResponsesRequest
					if err := copier.CopyWithOption(&dst, &src, copier.Option{DeepCopy: true, IgnoreEmpty: true}); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("optimized", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(src.Input)))
				for i := 0; i < b.N; i++ {
					if _, err := DeepCopy(&src); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
