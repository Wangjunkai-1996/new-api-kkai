package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	KKAIPolicyConversationIDHeader = "X-KKAI-Conversation-ID"
	KKAIPolicyScopeContextKey      = "kkai_policy_conversation_scope"
	KKAIPolicyCooldownContextKey   = "kkai_policy_conversation_cooldown"
	KKAIPolicyCooldownReasonCyber = "cyber"
	KKAIPolicyCooldownReasonWords = "keyword"

	kkaiPolicyConversationKeyPrefix = "kkai:policy:conversation:v1:"
	kkaiPolicyConversationIDMaxBytes = 256
	kkaiPolicyKeywordThreshold       = 3
	kkaiPolicyKeywordWindow          = 10 * time.Minute
)

type KKAIPolicyConversationScope struct {
	Key         string
	Fingerprint string
	Source      string
	Stable      bool
}

func (s KKAIPolicyConversationScope) PublicScope() string {
	if s.Stable {
		return "conversation"
	}
	return "request"
}

type KKAIPolicyCooldownState struct {
	Blocked          bool
	RetryAfter      int
	Strike           int
	BlockedUntil    int64
	KeywordHits     int
	Reason           string
	Scope           string
	StoreAvailable  bool
}

type KKAIPolicyCooldownStore interface {
	Check(context.Context, string) (KKAIPolicyCooldownState, error)
	RecordCyber(context.Context, string, string, bool) (KKAIPolicyCooldownState, error)
	RecordKeyword(context.Context, string, string, bool) (KKAIPolicyCooldownState, error)
}

func IsKKAIPolicyConversationPath(path string) bool {
	switch path {
	case "/v1/chat/completions", "/v1/completions", "/v1/messages", "/v1/responses", "/v1/responses/compact", "/pg/chat/completions":
		return true
	}
	return strings.HasPrefix(path, "/v1beta/models/") &&
		(strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"))
}

func ResolveKKAIPolicyConversationScope(c *gin.Context) (KKAIPolicyConversationScope, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil || !IsKKAIPolicyConversationPath(c.Request.URL.Path) {
		return KKAIPolicyConversationScope{}, nil
	}

	principalKind := "token"
	principalID := c.GetInt("token_id")
	if principalID <= 0 {
		principalKind = "user"
		principalID = c.GetInt("id")
	}
	if principalID <= 0 {
		return KKAIPolicyConversationScope{}, nil
	}

	conversationID := strings.TrimSpace(c.GetHeader(KKAIPolicyConversationIDHeader))
	if isValidKKAIPolicyConversationID(conversationID) {
		return newKKAIPolicyScope(principalKind, principalID, c.Request.URL.Path, "header", conversationID, true), nil
	}

	digest, err := kkaiPolicyRequestBodyDigest(c)
	if err != nil {
		return KKAIPolicyConversationScope{}, err
	}
	if digest == "" {
		return KKAIPolicyConversationScope{}, nil
	}
	return newKKAIPolicyScope(principalKind, principalID, c.Request.URL.Path, "request_body", digest, false), nil
}

func EnsureKKAIPolicyConversationScope(c *gin.Context) (KKAIPolicyConversationScope, bool, error) {
	if c == nil {
		return KKAIPolicyConversationScope{}, false, nil
	}
	if scope, ok := KKAIPolicyConversationScopeFromContext(c); ok {
		return scope, true, nil
	}
	scope, err := ResolveKKAIPolicyConversationScope(c)
	if err != nil {
		return KKAIPolicyConversationScope{}, false, err
	}
	if scope.Key == "" {
		return scope, false, nil
	}
	c.Set(KKAIPolicyScopeContextKey, scope)
	return scope, true, nil
}

func SetKKAIPolicyCooldownState(c *gin.Context, state KKAIPolicyCooldownState) {
	if c != nil {
		c.Set(KKAIPolicyCooldownContextKey, state)
	}
}

func KKAIPolicyCooldownStateFromContext(c *gin.Context) (KKAIPolicyCooldownState, bool) {
	if c == nil {
		return KKAIPolicyCooldownState{}, false
	}
	value, ok := c.Get(KKAIPolicyCooldownContextKey)
	if !ok {
		return KKAIPolicyCooldownState{}, false
	}
	state, ok := value.(KKAIPolicyCooldownState)
	return state, ok
}

func KKAIPolicyConversationScopeFromContext(c *gin.Context) (KKAIPolicyConversationScope, bool) {
	if c == nil {
		return KKAIPolicyConversationScope{}, false
	}
	value, ok := c.Get(KKAIPolicyScopeContextKey)
	if !ok {
		return KKAIPolicyConversationScope{}, false
	}
	scope, ok := value.(KKAIPolicyConversationScope)
	return scope, ok && scope.Key != ""
}

func RecordKKAIPolicyKeyword(c *gin.Context, store KKAIPolicyCooldownStore) (KKAIPolicyCooldownState, error) {
	scope, ok, err := EnsureKKAIPolicyConversationScope(c)
	if err != nil || !ok || store == nil {
		return KKAIPolicyCooldownState{Scope: scope.PublicScope()}, err
	}
	state, err := store.RecordKeyword(kkaPolicyRequestContext(c), scope.Key, kkaiPolicyLocalEventID(c, "keyword"), scope.Stable)
	state.Reason = KKAIPolicyCooldownReasonWords
	state.Scope = scope.PublicScope()
	state.StoreAvailable = err == nil
	if err == nil {
		SetKKAIPolicyCooldownState(c, state)
	}
	return state, err
}

func RecordKKAIPolicyCyber(c *gin.Context, store KKAIPolicyCooldownStore, eventID string) (KKAIPolicyCooldownState, error) {
	scope, ok, err := EnsureKKAIPolicyConversationScope(c)
	if err != nil || !ok || store == nil {
		return KKAIPolicyCooldownState{Scope: scope.PublicScope()}, err
	}
	state, err := store.RecordCyber(kkaPolicyRequestContext(c), scope.Key, eventID, scope.Stable)
	state.Reason = KKAIPolicyCooldownReasonCyber
	state.Scope = scope.PublicScope()
	state.StoreAvailable = err == nil
	if err == nil {
		SetKKAIPolicyCooldownState(c, state)
	}
	return state, err
}

func KKAIPolicyMessageForKeyword() string {
	return "你输入的内容包含暂不支持的信息，本次请求未发送。请调整相关内容后重新尝试。此次拦截只影响当前请求，不会影响你的账号或其他对话。"
}

func KKAIPolicyMessageForKeywordCooldown(retryAfter int, stable bool) string {
	if !stable || retryAfter <= 0 {
		return KKAIPolicyMessageForKeyword()
	}
	return fmt.Sprintf("这段对话多次包含暂不支持的内容，已暂时暂停 %d 秒。请调整相关内容后再试。你的账号和其他对话不受影响。", retryAfter)
}

func KKAIPolicyMessageForCyber(retryAfter int, stable bool) string {
	if !stable {
		return "这次请求触发了安全风险提示，暂未发送。请调整相关内容后重新尝试。"
	}
	if retryAfter <= 0 {
		return "这段对话触发了安全风险提示。请调整相关内容后重新尝试，也可以新建对话继续使用。"
	}
	return fmt.Sprintf("这段对话触发了安全风险提示，已暂时暂停 %d 秒。你的账号和其他对话不受影响。请调整相关内容后再试；如果再次触发，等待时间会延长。", retryAfter)
}

func KKAIPolicyMessageForCooldown(retryAfter int, stable bool) string {
	if retryAfter < 1 {
		retryAfter = 1
	}
	if stable {
		return fmt.Sprintf("这段对话仍在暂停中，请等待 %d 秒后再试。你也可以新建对话继续使用。", retryAfter)
	}
	return fmt.Sprintf("这次请求仍在冷却中，请等待 %d 秒后再试。", retryAfter)
}

func kkaPolicyRequestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	parent := context.WithoutCancel(c.Request.Context())
	ctx, _ := context.WithTimeout(parent, 150*time.Millisecond)
	return ctx
}

func newKKAIPolicyScope(principalKind string, principalID int, endpoint string, source string, identity string, stable bool) KKAIPolicyConversationScope {
	material := fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", principalKind, principalID, endpoint, source, identity)
	fingerprint := common.GenerateHMAC("kkai-conversation-policy-v1:" + material)
	return KKAIPolicyConversationScope{
		Key:         kkaiPolicyConversationKeyPrefix + fingerprint,
		Fingerprint: fingerprint,
		Source:      source,
		Stable:      stable,
	}
}

func isValidKKAIPolicyConversationID(value string) bool {
	if value == "" || len(value) > kkaiPolicyConversationIDMaxBytes {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func kkaiPolicyRequestBodyDigest(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return "", nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, storage); err != nil {
		return "", err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(storage)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func kkaiPolicyLocalEventID(c *gin.Context, prefix string) string {
	requestID := ""
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
	}
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + ":" + requestID
}

func KKAIPolicyDefaultCooldownStore() KKAIPolicyCooldownStore {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	return NewRedisKKAIPolicyCooldownStore(common.RDB)
}

func KKAIPolicyErrorCode(state KKAIPolicyCooldownState) types.ErrorCode {
	if state.Blocked {
		return types.ErrorCodeConversationPolicyViolation
	}
	return types.ErrorCodePromptBlocked
}
