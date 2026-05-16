package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyIncidentEventAutoMigrateCreatesTable(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PolicyIncidentEvent{}))
	assert.True(t, DB.Migrator().HasTable(&PolicyIncidentEvent{}))
	assert.True(t, DB.Migrator().HasColumn(&PolicyIncidentEvent{}, "request_id"))
	assert.True(t, DB.Migrator().HasColumn(&PolicyIncidentEvent{}, "metadata"))
	assert.True(t, DB.Migrator().HasColumn(&PolicyIncidentEvent{}, "created_at"))
}

func TestInsertPolicyIncidentEventPersistsSafeAppendOnlyRecord(t *testing.T) {
	truncatePolicyIncidentEvents(t)
	rawKey := "sk-policy-raw-secret"

	event := &PolicyIncidentEvent{
		RequestId:              "req-policy-1",
		UserId:                 10,
		TokenId:                20,
		TokenName:              "prod-token",
		ModelName:              "gpt-test",
		ChannelId:              30,
		ChannelType:            2,
		UpstreamKeyFingerprint: rawKey,
		StatusCode:             403,
		ErrorCode:              "policy_denied",
		ErrorMessage:           "blocked by cyber policy",
		EvidenceLevel:          "high",
		Causality:              "upstream_policy",
		ActionTaken:            "channel_disabled",
		ActionResult:           "success",
	}
	require.NoError(t, event.SetMetadata(map[string]any{
		"rule_id":      "cyber-p0",
		"prompt":       "do not persist this",
		"upstream_key": rawKey,
		"nested": map[string]any{
			"messages": []any{"do not persist this either"},
		},
	}))

	require.NoError(t, InsertPolicyIncidentEvent(event))

	var reloaded PolicyIncidentEvent
	require.NoError(t, DB.First(&reloaded, event.Id).Error)
	assert.NotZero(t, reloaded.Id)
	assert.NotZero(t, reloaded.CreatedAt)
	assert.Equal(t, "req-policy-1", reloaded.RequestId)
	assert.Equal(t, FingerprintPolicyIncidentUpstreamKey(rawKey), reloaded.UpstreamKeyFingerprint)
	assert.NotContains(t, string(reloaded.Metadata), rawKey)
	assert.NotContains(t, string(reloaded.Metadata), "do not persist this")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(reloaded.Metadata, &metadata))
	assert.Equal(t, "cyber-p0", metadata["rule_id"])
	assert.Equal(t, PolicyIncidentMetadataRedacted, metadata["prompt"])
	assert.Equal(t, FingerprintPolicyIncidentUpstreamKey(rawKey), metadata["upstream_key"])
	assert.Equal(t, PolicyIncidentMetadataRedacted, metadata["nested"].(map[string]any)["messages"])
}

func TestPolicyIncidentEventMetadataAcceptsJSONString(t *testing.T) {
	truncatePolicyIncidentEvents(t)

	event := &PolicyIncidentEvent{
		RequestId: "req-policy-json",
		Metadata:  JSONValue(`{"evidence":"header-match","input":"remove me"}`),
	}

	require.NoError(t, InsertPolicyIncidentEvent(event))

	var reloaded PolicyIncidentEvent
	require.NoError(t, DB.First(&reloaded, event.Id).Error)
	assert.JSONEq(t, `{"evidence":"header-match","input":"[redacted]"}`, string(reloaded.Metadata))
}

func TestPolicyIncidentEventRedactsRawKeyFromErrorMessage(t *testing.T) {
	truncatePolicyIncidentEvents(t)
	rawKey := "sk-policy-message-secret"

	event := &PolicyIncidentEvent{
		RequestId:              "req-policy-message",
		UpstreamKeyFingerprint: rawKey,
		ErrorMessage:           "provider disabled upstream key " + rawKey,
	}
	require.NoError(t, InsertPolicyIncidentEvent(event))

	var reloaded PolicyIncidentEvent
	require.NoError(t, DB.First(&reloaded, event.Id).Error)
	assert.Equal(t, FingerprintPolicyIncidentUpstreamKey(rawKey), reloaded.UpstreamKeyFingerprint)
	assert.NotContains(t, reloaded.ErrorMessage, rawKey)
	assert.Contains(t, reloaded.ErrorMessage, PolicyIncidentMetadataRedacted)
}

func TestPolicyIncidentEventMetadataAcceptsPlainString(t *testing.T) {
	metadata, err := NormalizePolicyIncidentMetadata("header policy fired")
	require.NoError(t, err)
	assert.JSONEq(t, `"header policy fired"`, string(metadata))
}

func TestPolicyIncidentEventAdminNotificationPayload(t *testing.T) {
	metadata := JSONValue(`{"rule_id":"cyber-p0"}`)
	event := &PolicyIncidentEvent{
		Id:                     99,
		RequestId:              "req-notify",
		UserId:                 1,
		TokenId:                2,
		TokenName:              "token",
		ModelName:              "model",
		ChannelId:              3,
		ChannelType:            4,
		UpstreamKeyFingerprint: FingerprintPolicyIncidentUpstreamKey("secret"),
		StatusCode:             403,
		ErrorCode:              "policy_denied",
		ErrorMessage:           "blocked",
		EvidenceLevel:          "high",
		Causality:              "provider_policy",
		ActionTaken:            "disabled",
		ActionResult:           "success",
		Metadata:               metadata,
		CreatedAt:              123456,
	}

	payload := event.AdminNotificationPayload()

	assert.Equal(t, event.Id, payload.IncidentId)
	assert.Equal(t, event.RequestId, payload.RequestId)
	assert.Equal(t, event.UserId, payload.UserId)
	assert.Equal(t, event.TokenId, payload.TokenId)
	assert.Equal(t, event.TokenName, payload.TokenName)
	assert.Equal(t, event.ModelName, payload.ModelName)
	assert.Equal(t, event.ChannelId, payload.ChannelId)
	assert.Equal(t, event.ChannelType, payload.ChannelType)
	assert.Equal(t, event.UpstreamKeyFingerprint, payload.UpstreamKeyFingerprint)
	assert.Equal(t, event.StatusCode, payload.StatusCode)
	assert.Equal(t, event.ErrorCode, payload.ErrorCode)
	assert.Equal(t, event.ErrorMessage, payload.ErrorMessage)
	assert.Equal(t, event.EvidenceLevel, payload.EvidenceLevel)
	assert.Equal(t, event.Causality, payload.Causality)
	assert.Equal(t, event.ActionTaken, payload.ActionTaken)
	assert.Equal(t, event.ActionResult, payload.ActionResult)
	assert.Equal(t, event.CreatedAt, payload.CreatedAt)
	assert.JSONEq(t, string(metadata), string(payload.Metadata))

	payload.Metadata[0] = '{'
	assert.Equal(t, `{"rule_id":"cyber-p0"}`, string(event.Metadata))
}

func TestPolicyIncidentEventAppendOnly(t *testing.T) {
	truncatePolicyIncidentEvents(t)

	event := &PolicyIncidentEvent{RequestId: "req-append-only"}
	require.NoError(t, InsertPolicyIncidentEvent(event))

	event.ErrorMessage = "mutated"
	err := DB.Save(event).Error
	require.ErrorIs(t, err, ErrPolicyIncidentEventAppendOnly)

	err = DB.Delete(event).Error
	require.ErrorIs(t, err, ErrPolicyIncidentEventAppendOnly)
}

func TestNormalizePolicyIncidentKeyFingerprint(t *testing.T) {
	rawKey := "sk-policy-secret"
	fingerprint := FingerprintPolicyIncidentUpstreamKey(rawKey)
	assert.True(t, strings.HasPrefix(fingerprint, "sha256:"))
	assert.Equal(t, fingerprint, NormalizePolicyIncidentKeyFingerprint(rawKey))
	assert.Equal(t, fingerprint, NormalizePolicyIncidentKeyFingerprint(strings.TrimPrefix(fingerprint, "sha256:")))
	assert.Equal(t, fingerprint, NormalizePolicyIncidentKeyFingerprint(strings.ToUpper(fingerprint)))
	assert.NotContains(t, fingerprint, rawKey)
}

func truncatePolicyIncidentEvents(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&PolicyIncidentEvent{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM policy_incident_events")
	})
}
