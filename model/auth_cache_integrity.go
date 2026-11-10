package model

import (
	"context"
	"crypto/hmac"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var (
	errAuthCacheIntegrity       = errors.New("authentication cache integrity check failed")
	errAuthCacheSnapshotChanged = errors.New("authentication cache metadata changed during refresh")
	errAuthCacheUnsafeCounters  = errors.New("authentication cache identity or counters cannot be safely preserved")
)

// Only database snapshots or server-created session states may be signed.
// The domain and full Redis key prevent reuse across cache kinds or identities.
func authCacheMAC(key string, metadata any) (string, error) {
	payload, err := common.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return common.GenerateHMACWithKey([]byte("auth-cache-v1:"+common.CryptoSecret), key+"\x00"+string(payload)), nil
}

func validAuthCacheMAC(expected, actual string, expiresAt int64) bool {
	return expiresAt > time.Now().UnixMilli() && hmac.Equal([]byte(expected), []byte(actual))
}

// Compare every observed metadata field, including missing/extra fields. Old
// writers can change metadata without changing AuthVersion or CacheMAC. Quota
// deltas are deliberately excluded and remain owned by the atomic billing paths.
const authCacheCASLua = `
local function authCacheMatches(observedJSON, counters)
  local observed = cjson.decode(observedJSON)
  local exists = redis.call('EXISTS', KEYS[1]) == 1
  if exists ~= (next(observed) ~= nil) then
    return false
  end
  local current = redis.call('HGETALL', KEYS[1])
  for i = 1, #current, 2 do
    if not counters[current[i]] and observed[current[i]] ~= current[i+1] then
      return false
    end
  end
  for field, value in pairs(observed) do
    if not counters[field] and redis.call('HGET', KEYS[1], field) ~= value then
      return false
    end
  end
  return true
end
`

// Discard only our own publication when its post-write database check fails.
// Never delete live counters, a newer signature, or revocation fences.
func discardAuthCacheSignature(key, mac string) error {
	const script = `
if redis.call('HGET', KEYS[1], 'CacheMAC') == ARGV[1] then
  redis.call('HDEL', KEYS[1], 'CacheMAC', 'CacheExpiresAt')
end
return 1`
	return common.RDB.Eval(context.Background(), script, []string{key}, mac).Err()
}
