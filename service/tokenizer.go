package service

import (
	"crypto/rand"
	"crypto/sha256"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

// tokenEncoderMap won't grow after initialization
var defaultTokenEncoder tokenizer.Codec

// tokenEncoderMap is used to store token encoders for different models
var tokenEncoderMap = make(map[string]tokenizer.Codec)

// tokenEncoderMutex protects tokenEncoderMap for concurrent access
var tokenEncoderMutex sync.RWMutex

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
		common.SysError("token chunk cache disabled: " + err.Error())
	}
	return salt
}()

func InitTokenEncoders() {
	common.SysLog("initializing token encoders")
	defaultTokenEncoder = codec.NewCl100kBase()
	common.SysLog("token encoders initialized")
}

func getTokenEncoder(model string) tokenizer.Codec {
	// First, try to get the encoder from cache with read lock
	tokenEncoderMutex.RLock()
	if encoder, exists := tokenEncoderMap[model]; exists {
		tokenEncoderMutex.RUnlock()
		return encoder
	}
	tokenEncoderMutex.RUnlock()

	// If not in cache, create new encoder with write lock
	tokenEncoderMutex.Lock()
	defer tokenEncoderMutex.Unlock()

	// Double-check if another goroutine already created the encoder
	if encoder, exists := tokenEncoderMap[model]; exists {
		return encoder
	}

	// Create new encoder
	modelCodec, err := tokenizer.ForModel(tokenizer.Model(model))
	if err != nil {
		// Cache the default encoder for this model to avoid repeated failures
		tokenEncoderMap[model] = defaultTokenEncoder
		return defaultTokenEncoder
	}

	// Cache the new encoder
	tokenEncoderMap[model] = modelCodec
	return modelCodec
}

func getTokenNum(tokenEncoder tokenizer.Codec, text string) int {
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
