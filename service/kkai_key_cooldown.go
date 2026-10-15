package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

const (
	kkaiPolicyKeyCooldownKeyPrefix   = "kkai:policy:key-cooldown:v3:"
	kkaiPolicyKeyCooldownScopeDomain = "kkai-key-cooldown-scope-v3"
	kkaiPolicyKeyCooldownEventDomain = "kkai-key-cooldown-event-v1"
	kkaiPolicyKeyCooldownTimeout     = 150 * time.Millisecond
	kkaiPolicyKeyCooldownMaxStrike   = 1
	kkaiPolicyKeyCooldownMaxSeconds  = 60
	kkaiPolicyEmergencyCooldown      = time.Minute
)

var kkaiPolicyEmergencyCooldowns = struct {
	sync.Mutex
	blockedUntil map[string]time.Time
	lastSweep    time.Time
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

func KKAIPolicyClientSessionID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	for _, name := range []string{"session-id", "session_id", "conversation_id", "X-Session-Id", "X-OpenCode-Session", "X-Conversation-ID", "X-Claude-Code-Session-Id"} {
		value := strings.TrimSpace(c.GetHeader(name))
		if value == "" || len(value) > 255 || !utf8.ValidString(value) {
			continue
		}
		valid := true
		for _, char := range value {
			if char < 0x20 || char == 0x7f {
				valid = false
				break
			}
		}
		if valid {
			return value
		}
	}
	return ""
}

func KKAIPolicyKeyCooldownRedisKey(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	sessionID := KKAIPolicyClientSessionID(c)
	if userID <= 0 || tokenID <= 0 || sessionID == "" {
		return "", false
	}
	material := fmt.Sprintf("%s\x00user_id\x00%d\x00token_id\x00%d\x00session_id\x00%s", kkaiPolicyKeyCooldownScopeDomain, userID, tokenID, sessionID)
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
	key, ok := KKAIPolicyKeyCooldownRedisKey(c)
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

// RecordKKAIPolicyEmergencyKeyCooldown applies a one-minute process-local
// fallback for the same conversation when Redis cannot record the cooldown.
func RecordKKAIPolicyEmergencyKeyCooldown(c *gin.Context, now time.Time) KKAIPolicyKeyCooldownState {
	if c == nil {
		return KKAIPolicyKeyCooldownState{}
	}
	key, ok := KKAIPolicyKeyCooldownRedisKey(c)
	if !ok {
		return KKAIPolicyKeyCooldownState{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	blockedUntil := now.Add(kkaiPolicyEmergencyCooldown)

	kkaiPolicyEmergencyCooldowns.Lock()
	sweepExpiredKKAIPolicyEmergencyCooldowns(now)
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
	sweepExpiredKKAIPolicyEmergencyCooldowns(now)
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

func sweepExpiredKKAIPolicyEmergencyCooldowns(now time.Time) {
	if !kkaiPolicyEmergencyCooldowns.lastSweep.IsZero() &&
		now.Sub(kkaiPolicyEmergencyCooldowns.lastSweep) < time.Minute {
		return
	}
	for key, blockedUntil := range kkaiPolicyEmergencyCooldowns.blockedUntil {
		if !blockedUntil.After(now) {
			delete(kkaiPolicyEmergencyCooldowns.blockedUntil, key)
		}
	}
	kkaiPolicyEmergencyCooldowns.lastSweep = now
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
