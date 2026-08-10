package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	KKAIPolicyCaseContextKey       = "kkai_policy_case_id"
	KKAIPolicyCausalityContextKey  = "kkai_policy_causality"
	kkaiPolicyNoRetryContextKey    = "kkai_policy_no_retry"
	kkaiPolicyHandledContextKey    = "kkai_policy_handled"
	kkaiPolicyRuleVersion          = "kkai-upstream-policy-v2"
	kkaiPolicyClientAuthBearer     = "bearer_token"
	kkaiPolicyClientAuthPlayground = "playground_session"
)

type KKAIPolicyIncidentGuard struct {
	applier  RiskActionApplier
	cooldown KKAIPolicyKeyCooldownStore
	now      func() time.Time
}

func NewKKAIPolicyIncidentGuard(applier RiskActionApplier) *KKAIPolicyIncidentGuard {
	return NewKKAIPolicyIncidentGuardWithKeyCooldown(applier, KKAIPolicyDefaultKeyCooldownStore())
}

func NewKKAIPolicyIncidentGuardWithKeyCooldown(applier RiskActionApplier, cooldown KKAIPolicyKeyCooldownStore) *KKAIPolicyIncidentGuard {
	return &KKAIPolicyIncidentGuard{applier: applier, cooldown: cooldown, now: time.Now}
}

func (g *KKAIPolicyIncidentGuard) HandleAPIError(c *gin.Context, channel types.ChannelError, apiErr *types.NewAPIError) (bool, error) {
	return g.handle(c, channel, ClassifyKKAIUpstreamPolicyError(apiErr))
}

func (g *KKAIPolicyIncidentGuard) HandleTaskError(c *gin.Context, channel types.ChannelError, taskErr *dto.TaskError) (bool, error) {
	return g.handle(c, channel, ClassifyKKAITaskPolicyError(taskErr))
}

func (g *KKAIPolicyIncidentGuard) handle(c *gin.Context, channel types.ChannelError, classification KKAIPolicyClassification) (bool, error) {
	if !classification.Detected {
		return false, nil
	}
	markKKAIPolicyContext(c, classification.Causality)
	if c != nil && c.GetBool(kkaiPolicyHandledContextKey) {
		return true, nil
	}
	if g == nil || g.now == nil {
		return true, ErrRiskActionInvalidInput
	}

	now := g.now()
	eventID := kkaiPolicyEventID(c, channel.ChannelId, now)
	if c != nil {
		c.Set(KKAIPolicyCaseContextKey, eventID)
	}
	userID := kkaiPolicyContextInt(c, "id")
	tokenID := kkaiPolicyContextInt(c, "token_id")
	tokenFingerprint := RiskFingerprint(kkaiPolicyContextString(c, "token_key"))
	upstreamKeyFingerprint := RiskFingerprint(channel.UsingKey)
	clientAuthMode := ""
	if tokenID > 0 && tokenFingerprint != "" {
		clientAuthMode = kkaiPolicyClientAuthBearer
	} else if tokenID == 0 && userID > 0 && c != nil {
		requestPath := ""
		if c.Request != nil && c.Request.URL != nil {
			requestPath = c.Request.URL.Path
		}
		if common.GetContextKeyBool(c, constant.ContextKeyIsPlayground) ||
			requestPath == "/pg" || strings.HasPrefix(requestPath, "/pg/") {
			clientAuthMode = kkaiPolicyClientAuthPlayground
		}
	}
	clientActionAllowed := classification.Causality == KKAIPolicyCausalityClientToken &&
		clientAuthMode != "" && channel.ChannelId > 0 && upstreamKeyFingerprint != ""
	metadata := map[string]any{
		"case_id":                        eventID,
		"causality":                      classification.Causality,
		"client_token_action_allowed":    clientActionAllowed,
		"client_policy_marker_confirmed": classification.Causality == KKAIPolicyCausalityClientToken,
		"evidence_level":                 "confirmed",
		"original_status_code":           classification.StatusCode,
		"rule_id":                        kkaiPolicyRuleVersion,
	}
	if clientAuthMode != "" {
		metadata["client_auth_mode"] = clientAuthMode
	}
	if body, ok := kkaiPolicyRequestBody(c); ok {
		digest := sha256.Sum256(body)
		metadata["request_body_bytes"] = int64(len(body))
		metadata["request_body_sha256"] = hex.EncodeToString(digest[:])
	}

	event := RiskStreamEvent{
		EventID:                eventID,
		Source:                 RiskSourceUpstreamPolicy,
		OccurredAt:             now.Unix(),
		RequestID:              kkaiPolicyContextString(c, common.RequestIdKey),
		UserID:                 userID,
		TokenID:                tokenID,
		ChannelID:              channel.ChannelId,
		ModelName:              kkaiPolicyContextString(c, "original_model"),
		RuleVersion:            kkaiPolicyRuleVersion,
		EvidenceSHA256:         RiskFingerprint(fmt.Sprintf("%d\n%s\n%s", classification.StatusCode, classification.ErrorCode, classification.Evidence)),
		TokenFingerprint:       tokenFingerprint,
		UpstreamKeyFingerprint: upstreamKeyFingerprint,
		Recommendation:         RiskDecisionReject,
		Metadata:               metadata,
	}
	if classification.Causality == KKAIPolicyCausalityClientToken && clientAuthMode == kkaiPolicyClientAuthBearer {
		if _, cooldownErr := RecordKKAIPolicyKeyCooldown(c, g.cooldown, eventID); cooldownErr != nil {
			RecordKKAIPolicyEmergencyKeyCooldown(c, now)
			logger.LogWarn(kkaiPolicyContext(c), "KKAI key cooldown record failed: "+cooldownErr.Error())
		}
	}
	if clientActionAllowed {
		event.Recommendation = RiskDecisionDisable
	}
	decision, actions, err := DecideKKAIRiskStreamEvent(event)
	if err != nil {
		return true, err
	}
	if g.applier == nil {
		return true, ErrRiskActionInvalidInput
	}
	result, err := g.applier.Apply(kkaiPolicyContext(c), RiskActionInput{
		EventID:                event.EventID,
		Source:                 event.Source,
		OccurredAt:             event.OccurredAt,
		RequestID:              event.RequestID,
		UserID:                 event.UserID,
		TokenID:                event.TokenID,
		ChannelID:              event.ChannelID,
		ModelName:              event.ModelName,
		RuleVersion:            event.RuleVersion,
		EvidenceSHA256:         event.EvidenceSHA256,
		TokenFingerprint:       event.TokenFingerprint,
		UpstreamKeyFingerprint: event.UpstreamKeyFingerprint,
		Decision:               decision,
		Metadata:               event.Metadata,
		Actions:                actions,
	})
	if err != nil {
		return true, err
	}
	if result == nil {
		return true, ErrRiskStreamInvalidResult
	}
	if c != nil {
		c.Set(kkaiPolicyHandledContextKey, true)
	}
	return true, nil
}

func markKKAIPolicyContext(c *gin.Context, causality string) {
	if c == nil {
		return
	}
	c.Set(kkaiPolicyNoRetryContextKey, true)
	c.Set(KKAIPolicyCausalityContextKey, causality)
}

func ShouldSkipRetryAfterKKAIPolicy(c *gin.Context) bool {
	return c != nil && c.GetBool(kkaiPolicyNoRetryContextKey)
}

func kkaiPolicyEventID(c *gin.Context, channelID int, now time.Time) string {
	requestID := kkaiPolicyContextString(c, common.RequestIdKey)
	if requestID != "" {
		candidate := fmt.Sprintf("upstream:%s:%d", requestID, channelID)
		if riskEventIDPattern.MatchString(candidate) {
			return candidate
		}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s:%d:%d:%d:%d",
		requestID,
		kkaiPolicyContextInt(c, "id"),
		kkaiPolicyContextInt(c, "token_id"),
		channelID,
		now.UnixNano(),
	)))
	return "upstream:" + hex.EncodeToString(sum[:16])
}

func kkaiPolicyRequestBody(c *gin.Context) ([]byte, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, false
	}
	_, _ = storage.Seek(0, io.SeekStart)
	c.Request.Body = io.NopCloser(storage)
	return body, true
}

func kkaiPolicyContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}

func kkaiPolicyContextString(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	return c.GetString(key)
}

func kkaiPolicyContextInt(c *gin.Context, key string) int {
	if c == nil {
		return 0
	}
	return c.GetInt(key)
}
