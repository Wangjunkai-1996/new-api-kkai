package tokenkit

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

var (
	o200kBase  = sync.OnceValue(func() tokenizer.Codec { return codec.NewO200kBase() })
	cl100kBase = sync.OnceValue(func() tokenizer.Codec { return codec.NewCl100kBase() })
)

// Count uses the provider's estimator or an exact OpenAI BPE count. The local
// tokenizer fork uses a heap for long pieces; no arbitrary byte cuts alter
// billable counts. Completed regex pieces alone share a bounded history cache.
func Count(model, text string) int {
	if text == "" {
		return 0
	}
	model = strings.ToLower(model)
	if ModelFamily(model) != FamilyOpenAI {
		return Estimate(model, text)
	}
	encoder := o200kBase()
	if usesCl100k(model) {
		encoder = cl100kBase()
	}
	return countTokensExact(encoder, text)
}

const tokenChunkMinBytes = 4096
const tokenChunkCacheMaxBytes = 16 << 20
const tokenChunkCacheMaxEntries = 4096

type tokenChunkKey struct {
	codec string
	sum   [32]byte
}

var tokenChunkCache = struct {
	sync.Mutex
	counts map[tokenChunkKey]int
	order  []tokenChunkKey
}{counts: make(map[tokenChunkKey]int)}

var tokenChunkSalt = func() [32]byte {
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return [32]byte{}
	}
	return salt
}()

func countTokensExact(tokenEncoder tokenizer.Codec, text string) int {
	if text == "" {
		return 0
	}
	name := tokenEncoder.GetName()
	if len(text) < tokenChunkMinBytes*2 || name != "cl100k_base" && name != "o200k_base" {
		tkm, _ := tokenEncoder.Count(text)
		return tkm
	}

	// Both codecs end a regex piece before a newline followed by an ASCII letter.
	// Count complete pieces independently so cached history keeps exact BPE counts.
	total, start := 0, 0
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '\n' || i+1-start < tokenChunkMinBytes ||
			!(text[i+1] >= 'A' && text[i+1] <= 'Z' || text[i+1] >= 'a' && text[i+1] <= 'z') {
			continue
		}
		count, err := countTokenChunk(tokenEncoder, name, text[start:i+1])
		if err != nil {
			tkm, _ := tokenEncoder.Count(text)
			return tkm
		}
		total += count
		start = i + 1
	}
	if start == 0 {
		tkm, _ := tokenEncoder.Count(text)
		return tkm
	}
	count, err := countTokenChunk(tokenEncoder, name, text[start:])
	if err != nil {
		tkm, _ := tokenEncoder.Count(text)
		return tkm
	}
	return total + count
}

func countTokenChunk(tokenEncoder tokenizer.Codec, name, text string) (int, error) {
	cacheable := len(text) >= tokenChunkMinBytes && len(text) <= tokenChunkCacheMaxBytes && tokenChunkSalt != [32]byte{}
	var key tokenChunkKey
	if cacheable {
		key.codec = name
		digest := sha256.New()
		_, _ = digest.Write(tokenChunkSalt[:])
		_, _ = digest.Write([]byte(text))
		copy(key.sum[:], digest.Sum(nil))
		tokenChunkCache.Lock()
		count, ok := tokenChunkCache.counts[key]
		tokenChunkCache.Unlock()
		if ok {
			return count, nil
		}
	}
	count, err := tokenEncoder.Count(text)
	if err != nil || !cacheable {
		return count, err
	}

	tokenChunkCache.Lock()
	defer tokenChunkCache.Unlock()
	if existing, ok := tokenChunkCache.counts[key]; ok {
		return existing, nil
	}
	tokenChunkCache.counts[key] = count
	tokenChunkCache.order = append(tokenChunkCache.order, key)
	for len(tokenChunkCache.order) > tokenChunkCacheMaxEntries {
		oldest := tokenChunkCache.order[0]
		delete(tokenChunkCache.counts, oldest)
		tokenChunkCache.order[0] = tokenChunkKey{}
		tokenChunkCache.order = tokenChunkCache.order[1:]
	}
	return count, nil
}

// usesCl100k reports whether an OpenAI model predates o200k_base: GPT-4 (but
// not GPT-4o, GPT-4.1, or GPT-4.5) and GPT-3.5. Every later model, including
// GPT-5, GPT-6, and the o-series, uses o200k_base.
func usesCl100k(model string) bool {
	// ModelFamily already accepts provider namespaces; codec selection must
	// inspect the same underlying model, including namespaced fine-tunes.
	model = model[strings.LastIndex(model, "/")+1:]
	model = strings.TrimPrefix(model, "ft:")
	return model == "gpt-4" || strings.HasPrefix(model, "gpt-4-") ||
		strings.HasPrefix(model, "gpt-3.5") || strings.HasPrefix(model, "gpt-35")
}
