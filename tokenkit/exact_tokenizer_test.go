package tokenkit

import (
	"crypto/sha256"
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
				assert.Equal(t, want, countTokensExact(tc.codec, text))
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
						assert.Equal(t, want, countTokensExact(tc.codec, text), "%q / %q / %q", before, after, suffix)
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
			results <- countTokensExact(encoder, texts[item]) == wants[item]
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
			_ = countTokensExact(encoder, histories[i%len(histories)])
		}
	})
}

// These token counts and token-ID hashes were produced with the unmodified
// tiktoken-go/tokenizer v0.8.1 module. They guard the bundled heap BPE and exact
// counting path against changes at the old 1024-byte approximation boundary.
func TestCountMatchesUpstreamBPE(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   int
		hashes [2]string
	}{
		{"latin-1023", strings.Repeat("a", 1023), 129, [2]string{"ebbff98dc05e44d92b54343523d6ed8f6f6354f700ffe4a851419fbf8a5f85a3", "38e9835a40c52d3821eeb81451a8496a92656b0081ddb9d71a640f4f2fdfa5ac"}},
		{"punctuation-1023", strings.Repeat("+/=_", 256)[:1023], 512, [2]string{"618a65299e2d1935431987493b1045897690b99369afa645540d62138494fad4", "55adaa15af05558178b570fb62ec1afb2342621fae9eb9d438c177cb8d4a68a0"}},
		{"latin-1024", strings.Repeat("a", 1024), 128, [2]string{"54dd33071941277678e7a8cec4b1473d0487ec12a65d260fc77f1f30ba9c9ce6", "d53a48218428056fccea0add6d87675396dcb3374cda7162f95e6cd43ce8c5f7"}},
		{"punctuation-1024", strings.Repeat("+/=_", 256)[:1024], 512, [2]string{"7877936711097200db7a857ed2b57475401b22eabe1113cf8f986d3e379faa7a", "8304a76d6aed8a765090bf5372db5415d7a2495aa897d8ba95d89258f81cd6ef"}},
		{"latin-1025", strings.Repeat("a", 1025), 129, [2]string{"ffd975b64490a9a2886791f71d095dd5b214626a3dd07f2f7f9c076eff5b2e57", "851df138fc7de1b072d17c9dca80639eebd2235cfc2752fb0debce3bb5118019"}},
		{"punctuation-1025", strings.Repeat("+/=_", 257)[:1025], 513, [2]string{"442925a5fe5a7d0ac75bf1b2a32c1b8c3e00950bc1a32641f2c05b0edba6ab87", "0fa1d726e43e7d8dcf57a04ba2d67bcaa420980577ebe95a7e2227378f9bdcdf"}},
		{"latin-8193", strings.Repeat("a", 8193), 1025, [2]string{"1bc7799cd5c077b881ec22319e7fc6b8a538f37c9a753ad0389bab89eee3329d", "eca8d4f1fadf266b357bd9dcedd25239bee68fe90c450e7e6746fbb30f79b4fb"}},
		{"punctuation-8193", strings.Repeat("+/=_", 2049)[:8193], 4097, [2]string{"a6ee0f91f04cf7525c921224185eca5af584a0288b510ea479c87805e985d11b", "a3a615e25a899408eb801e762435db567bbb9564dccfbad2dbfb2e77d1a68a27"}},
		{"unicode-boundary", strings.Repeat("订单Á/\r\n", 300) + "END", 1201, [2]string{"84ceed6f86ce9050bbca389679cf3d9bf96308a09b96de90a8a4a0b9e52e7144", "fdec2b63982e9c7396a49d1e5bf9c4fedd23259184c271c4444571d80ee18421"}},
		{"growing-history", strings.Repeat("User: stable history.\n", 450) + "Assistant: final answer\n  User: next", 2259, [2]string{"33b55312afafc4a15ec717d66791723fa31c1adc5fc25619275656e37cce5228", "6571dbacdb30c5ebc3d1875950749fd9b88fc33b7b7cafcedd7fb46f28e6d001"}},
	}
	for index, enc := range []*codec.Codec{codec.NewCl100kBase(), codec.NewO200kBase()} {
		model := "gpt-4"
		if index == 1 {
			model = "gpt-4o"
		}
		for _, tc := range cases {
			t.Run(enc.GetName()+"/"+tc.name, func(t *testing.T) {
				ids, _, err := enc.Encode(tc.text)
				require.NoError(t, err)
				digest := sha256.New()
				for _, id := range ids {
					fmt.Fprintf(digest, "%d,", id)
				}
				assert.Equal(t, tc.hashes[index], fmt.Sprintf("%x", digest.Sum(nil)))
				assert.Equal(t, tc.want, Count(model, tc.text))
			})
		}
	}
}
