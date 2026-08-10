package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

const (
	kkaiPolicyKeyCooldownKeyPrefix   = "kkai:policy:key-cooldown:v1:"
	kkaiPolicyKeyCooldownScopeDomain = "kkai-key-cooldown-scope-v1"
	kkaiPolicyKeyCooldownEventDomain = "kkai-key-cooldown-event-v1"
	kkaiPolicyKeyCooldownTimeout     = 150 * time.Millisecond
	kkaiPolicyKeyCooldownMaxStrike   = 7
	kkaiPolicyKeyCooldownMaxSeconds  = 3600
	kkaiPolicyEmergencyCooldown      = 24 * time.Hour
)

var kkaiPolicyEmergencyCooldowns = struct {
	sync.Mutex
	blockedUntil map[string]time.Time
}{blockedUntil: make(map[string]time.Time)}

type KKAIPolicyKeyCooldownState struct {
	Blocked      bool
	RetryAfter   int
	Strike       int
	BlockedUntil int64
}

type KKAIPolicyKeyCooldownStore interface {
	Check(context.Context, string) (KKAIPolicyKeyCooldownState, error)
	Record(context.Context, string, string) (KKAIPolicyKeyCooldownState, error)
}

func KKAIPolicyKeyCooldownRedisKey(tokenID int) (string, bool) {
	if tokenID <= 0 {
		return "", false
	}
	material := fmt.Sprintf("%s\x00token_id\x00%d", kkaiPolicyKeyCooldownScopeDomain, tokenID)
	return kkaiPolicyKeyCooldownKeyPrefix + common.GenerateHMAC(material), true
}

func KKAIPolicyKeyCooldownEventDigest(eventID string) (string, bool) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", false
	}
	return common.GenerateHMAC(kkaiPolicyKeyCooldownEventDomain + "\x00event_id\x00" + eventID), true
}

func RecordKKAIPolicyKeyCooldown(c *gin.Context, store KKAIPolicyKeyCooldownStore, eventID string) (KKAIPolicyKeyCooldownState, error) {
	parent := context.Background()
	if c != nil && c.Request != nil {
		parent = context.WithoutCancel(c.Request.Context())
	}
	return recordKKAIPolicyKeyCooldown(parent, c, store, eventID)
}

func recordKKAIPolicyKeyCooldown(parent context.Context, c *gin.Context, store KKAIPolicyKeyCooldownStore, eventID string) (KKAIPolicyKeyCooldownState, error) {
	if c == nil {
		return KKAIPolicyKeyCooldownState{}, nil
	}
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	key, ok := KKAIPolicyKeyCooldownRedisKey(tokenID)
	if !ok {
		return KKAIPolicyKeyCooldownState{}, nil
	}
	if store == nil {
		return KKAIPolicyKeyCooldownState{}, ErrKKAIPolicyKeyCooldownUnavailable
	}
	eventDigest, ok := KKAIPolicyKeyCooldownEventDigest(eventID)
	if !ok {
		return KKAIPolicyKeyCooldownState{}, ErrKKAIPolicyKeyCooldownInvalidEvent
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, kkaiPolicyKeyCooldownTimeout)
	defer cancel()
	return store.Record(ctx, key, eventDigest)
}

// RecordKKAIPolicyEmergencyKeyCooldown covers a failed Redis write until the
// durable token mutation succeeds or the incident can be persisted later.
func RecordKKAIPolicyEmergencyKeyCooldown(c *gin.Context, now time.Time) KKAIPolicyKeyCooldownState {
	if c == nil {
		return KKAIPolicyKeyCooldownState{}
	}
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	key, ok := KKAIPolicyKeyCooldownRedisKey(tokenID)
	if !ok {
		return KKAIPolicyKeyCooldownState{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	blockedUntil := now.Add(kkaiPolicyEmergencyCooldown)

	kkaiPolicyEmergencyCooldowns.Lock()
	if existing := kkaiPolicyEmergencyCooldowns.blockedUntil[key]; existing.After(blockedUntil) {
		blockedUntil = existing
	} else {
		kkaiPolicyEmergencyCooldowns.blockedUntil[key] = blockedUntil
	}
	kkaiPolicyEmergencyCooldowns.Unlock()

	return kkaiPolicyEmergencyCooldownState(blockedUntil, now)
}

func CheckKKAIPolicyEmergencyKeyCooldown(key string, now time.Time) KKAIPolicyKeyCooldownState {
	if key == "" {
		return KKAIPolicyKeyCooldownState{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	kkaiPolicyEmergencyCooldowns.Lock()
	blockedUntil, ok := kkaiPolicyEmergencyCooldowns.blockedUntil[key]
	if ok && !blockedUntil.After(now) {
		delete(kkaiPolicyEmergencyCooldowns.blockedUntil, key)
		ok = false
	}
	kkaiPolicyEmergencyCooldowns.Unlock()
	if !ok {
		return KKAIPolicyKeyCooldownState{}
	}
	return kkaiPolicyEmergencyCooldownState(blockedUntil, now)
}

func kkaiPolicyEmergencyCooldownState(blockedUntil time.Time, now time.Time) KKAIPolicyKeyCooldownState {
	retryAfter := int((blockedUntil.Sub(now) + time.Second - 1) / time.Second)
	if retryAfter < 1 {
		retryAfter = 1
	}
	return KKAIPolicyKeyCooldownState{
		Blocked:      true,
		RetryAfter:   retryAfter,
		BlockedUntil: blockedUntil.UnixMilli(),
	}
}

func KKAIPolicyDefaultKeyCooldownStore() KKAIPolicyKeyCooldownStore {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	return NewRedisKKAIPolicyKeyCooldownStore(common.RDB)
}
