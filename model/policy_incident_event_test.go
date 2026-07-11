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

func TestInsertPolicyIncidentEventPersistsStrictSafeMetadata(t *testing.T) {
	truncatePolicyIncidentEvents(t)
	rawKey := "sk-policy-raw-secret"
	digest := strings.Repeat("a", 64)
	event := &PolicyIncidentEvent{
		RequestId:              "req-policy-1",
		UserId:                 10,
		TokenId:                20,
		ChannelId:              30,
		ChannelType:            2,
		UpstreamKeyFingerprint: rawKey,
		StatusCode:             403,
		ErrorCode:              "provider free-form code",
		ErrorMessage:           "prompt echoed patient Alice Example SSN 123-45-6789",
		EvidenceLevel:          "high",
		Causality:              "client_policy_request",
		ActionTaken:            "token_breaker_set",
		ActionResult:           "success",
	}
	require.NoError(t, event.SetMetadata(map[string]any{
		"case_id":                     "policy-1710000000123-0123456789abcdef",
		"request_body_sha256":         digest,
		"request_body_bytes":          int64(17),
		"client_token_action_allowed": true,
	}))

	require.NoError(t, InsertPolicyIncidentEvent(event))

	var reloaded PolicyIncidentEvent
	require.NoError(t, DB.First(&reloaded, event.Id).Error)
	assert.Equal(t, FingerprintPolicyIncidentUpstreamKey(rawKey), reloaded.UpstreamKeyFingerprint)
	assert.Equal(t, PolicyIncidentErrorDetected, reloaded.ErrorCode)
	assert.Equal(t, PolicyIncidentErrorDetected, reloaded.ErrorMessage)
	assert.NotContains(t, string(reloaded.Metadata), rawKey)
	assert.NotContains(t, reloaded.ErrorMessage, "Alice")
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(reloaded.Metadata, &metadata))
	assert.Equal(t, digest, metadata["request_body_sha256"])
	assert.EqualValues(t, 17, metadata["request_body_bytes"])
	assert.Equal(t, true, metadata["client_token_action_allowed"])
}

func TestPolicyIncidentMetadataRejectsUnknownAndFreeFormValues(t *testing.T) {
	tests := map[string]any{
		"unknown field":   map[string]any{"note": "patient Alice Example"},
		"prompt":          map[string]any{"prompt": "do not persist this"},
		"path":            map[string]any{"path": "/v1/chat/completions"},
		"plain string":    "header policy fired",
		"nested metadata": map[string]any{"case_id": map[string]any{"value": "policy-1"}},
	}
	for name, metadata := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizePolicyIncidentMetadata(metadata)
			require.ErrorIs(t, err, ErrInvalidPolicyIncidentMetadata)
		})
	}
}

func TestPolicyIncidentBeforeCreateRejectsUnknownMetadataBoundaryBypass(t *testing.T) {
	truncatePolicyIncidentEvents(t)
	event := &PolicyIncidentEvent{
		RequestId: "req-policy-boundary-bypass",
		Metadata:  JSONValue(`{"note":"patient Alice Example"}`),
	}
	require.ErrorIs(t, InsertPolicyIncidentEvent(event), ErrInvalidPolicyIncidentMetadata)
	var count int64
	require.NoError(t, DB.Model(&PolicyIncidentEvent{}).Where("request_id = ?", event.RequestId).Count(&count).Error)
	assert.Zero(t, count)
}

func TestPolicyIncidentMetadataRequiresTypedCompleteBodyDigest(t *testing.T) {
	tests := []map[string]any{
		{"request_body_sha256": strings.Repeat("a", 64)},
		{"request_body_bytes": int64(1)},
		{"request_body_sha256": "short", "request_body_bytes": int64(1)},
		{"request_body_sha256": strings.Repeat("a", 64), "request_body_bytes": -1},
		{"client_token_action_allowed": "true"},
	}
	for _, metadata := range tests {
		_, err := NormalizePolicyIncidentMetadata(metadata)
		require.ErrorIs(t, err, ErrInvalidPolicyIncidentMetadata)
	}
}

func TestPolicyIncidentEventAdminNotificationPayloadClonesMetadata(t *testing.T) {
	metadata := JSONValue(`{"case_id":"policy-1710000000123-0123456789abcdef"}`)
	event := &PolicyIncidentEvent{Id: 99, Metadata: metadata}
	payload := event.AdminNotificationPayload()
	payload.Metadata[0] = '['
	assert.Equal(t, `{"case_id":"policy-1710000000123-0123456789abcdef"}`, string(event.Metadata))
}

func TestPolicyIncidentEventAppendOnly(t *testing.T) {
	truncatePolicyIncidentEvents(t)
	event := &PolicyIncidentEvent{RequestId: "req-append-only"}
	require.NoError(t, InsertPolicyIncidentEvent(event))
	event.ErrorMessage = "mutated"
	require.ErrorIs(t, DB.Save(event).Error, ErrPolicyIncidentEventAppendOnly)
	require.ErrorIs(t, DB.Delete(event).Error, ErrPolicyIncidentEventAppendOnly)
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
	t.Cleanup(func() { DB.Exec("DELETE FROM policy_incident_events") })
}
