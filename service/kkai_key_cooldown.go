package service

import (
	"context"
	"fmt"
	"strings"
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
)

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
	ctx, cancel := kkaiPolicyKeyCooldownContext(c, true)
	defer cancel()
	return store.Record(ctx, key, eventDigest)
}

func KKAIPolicyDefaultKeyCooldownStore() KKAIPolicyKeyCooldownStore {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	return NewRedisKKAIPolicyKeyCooldownStore(common.RDB)
}

func kkaiPolicyKeyCooldownContext(c *gin.Context, detached bool) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if c != nil && c.Request != nil {
		parent = c.Request.Context()
		if detached {
			parent = context.WithoutCancel(parent)
		}
	}
	return context.WithTimeout(parent, kkaiPolicyKeyCooldownTimeout)
}
