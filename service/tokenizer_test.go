package service

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer/codec"
)

func TestTokenCountMatchesCodecForGrowingHistory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		codec *codec.Codec
	}{
		{name: "cl100k", codec: codec.NewCl100kBase()},
		{name: "o200k", codec: codec.NewO200kBase()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := strings.Repeat("Assistant: analyze the tool result and continue.\nUser: 查找订单 123 /api/v1.\n", 350)
			for _, text := range []string{
				prefix,
				prefix + "Assistant: final answer",
				prefix + "Assistant: final answer\r\nUser: next request",
				prefix + "Assistant: final answer\n/relative/path",
				prefix + "Assistant: final answer\n\nUser: next request",
				prefix + "Assistant: final answer\n  User: next request",
				prefix + "Assistant: final answer\nA\u0301 combining mark",
				strings.Repeat("Assistant: trailing whitespace before newline.  \n", 350) + "User: next request",
				strings.Replace(prefix, "订单", "请求", 1) + "Assistant: edited history",
				strings.Replace(prefix, "订单", "请求", 1) + "Assistant: same-length edit",
				prefix + "Assistant: trailing spaces \n  ",
			} {
				want, err := tc.codec.Count(text)
				require.NoError(t, err)
				assert.Equal(t, want, getTokenNum(tc.codec, text))
			}
		})
	}
}

func TestTokenCountChunkBoundaryMatchesCodec(t *testing.T) {
	for _, tc := range []struct {
		name  string
		codec *codec.Codec
	}{
		{name: "cl100k", codec: codec.NewCl100kBase()},
		{name: "o200k", codec: codec.NewO200kBase()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, before := range []string{"word", "9", "!", "/", " ", "\r", "\n", "\u0301", "中"} {
				for _, after := range []string{"A", "z"} {
					for _, suffix := range []string{"word", "123", "!", "/", "\r\n", "  ", "\u0301", "中"} {
						text := strings.Repeat("User: stable history.\n", 210) + before + "\n" + after + suffix
						want, err := tc.codec.Count(text)
						require.NoError(t, err)
						assert.Equal(t, want, getTokenNum(tc.codec, text), "%q / %q / %q", before, after, suffix)
					}
				}
			}
		})
	}
}

func TestTokenCountConcurrentCacheAccess(t *testing.T) {
	encoder := codec.NewO200kBase()
	base := strings.Repeat("User: inspect order 123.\nAssistant: verify the result.\n", 100)
	texts := []string{base + "User: first", base + "User: second", strings.Replace(base, "order 123", "order 456", 1)}
	wants := make([]int, len(texts))
	for i, text := range texts {
		count, err := encoder.Count(text)
		require.NoError(t, err)
		wants[i] = count
	}
	var workers sync.WaitGroup
	results := make(chan bool, 24)
	for i := 0; i < 24; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			item := index % len(texts)
			results <- getTokenNum(encoder, texts[item]) == wants[item]
		}(i)
	}
	workers.Wait()
	close(results)
	for equal := range results {
		assert.True(t, equal)
	}
}

func BenchmarkTokenCountGrowingResponsesHistory(b *testing.B) {
	encoder := codec.NewO200kBase()
	var text strings.Builder
	var histories []string
	for i := 0; i < 3650; i++ {
		fmt.Fprintf(&text, "Assistant: analyze tool output %d, verify request_id=req_%d and amount=%d.\nUser: continue with order %d and explain the result.\n", i, i*37, i*17, i*13)
		if i >= 3500 && i%50 == 49 {
			histories = append(histories, text.String())
		}
	}
	finalTokens, err := encoder.Count(histories[len(histories)-1])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(finalTokens), "final_tokens")
	b.Run("codec", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = encoder.Count(histories[i%len(histories)])
		}
	})
	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = getTokenNum(encoder, histories[i%len(histories)])
		}
	})
}
