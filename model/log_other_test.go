package model

import (
	"github.com/QuantumNous/new-api/common"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogOtherScopesAndMerges(t *testing.T) {
	var other LogOther

	assert.True(t, other.SetPublic("request_path", "/v1/chat/completions"))
	other.MergePublic(map[string]any{
		"zero": 0,
	})
	assert.True(t, other.SetAdmin("use_channel", []string{"channel-a"}))
	other.MergeAdmin(map[string]any{
		"rejected": false,
	})
	assert.True(t, other.SetRoot("upstream_request_id", "upstream-private"))
	other.MergeRoot(map[string]any{
		"generation": 0,
	})
	assert.True(t, other.SetAudit("method", "POST"))
	other.MergeAudit(map[string]any{
		"success": false,
	})

	require.JSONEq(t, `{
		"request_path": "/v1/chat/completions",
		"zero": 0,
		"admin_info": {
			"use_channel": ["channel-a"],
			"rejected": false
		},
		"root_info": {
			"upstream_request_id": "upstream-private",
			"generation": 0
		},
		"audit_info": {
			"method": "POST",
			"success": false
		}
	}`, other.JSONString())
}

func TestLogOtherRejectsSensitivePublicFields(t *testing.T) {
	other := NewLogOther()

	for _, key := range []string{
		"admin_info",
		"root_info",
		"audit_info",
		"channel_id",
		"channel_name",
		"channel_type",
		"reject_reason",
	} {
		assert.False(t, other.SetPublic(key, "must-not-leak"), key)
	}
	other.MergePublic(map[string]any{
		"request_path": "/v1/responses",
		"channel_name": "still-must-not-leak",
		"admin_info":   map[string]any{"secret": true},
	})

	require.JSONEq(t, `{"request_path":"/v1/responses"}`, other.JSONString())
	require.JSONEq(t, `{}`, NewLogOther().JSONString())
}

func TestLogOtherJSONStringDoesNotMutateReceiver(t *testing.T) {
	other := NewLogOther()
	require.True(t, other.SetPublic("request_path", "/v1/chat/completions"))
	require.True(t, other.SetAdmin("rejected", false))

	before := other.Snapshot()
	first := other.JSONString()
	after := other.Snapshot()
	second := other.JSONString()

	require.Equal(t, before, after)
	require.Equal(t, first, second)
}

func TestLogOtherUnmarshalRestoresScopes(t *testing.T) {
	var other LogOther
	require.NoError(t, other.UnmarshalJSON([]byte(`{
		"request_path":"/v1/images/generations",
		"admin_info":{"image_pricing":{"model":"gpt-image-1"}},
		"root_info":{"upstream_request_id":"upstream-1"},
		"audit_info":{"method":"POST"}
	}`)))

	require.JSONEq(t, `{
		"request_path":"/v1/images/generations",
		"admin_info":{"image_pricing":{"model":"gpt-image-1"}},
		"root_info":{"upstream_request_id":"upstream-1"},
		"audit_info":{"method":"POST"}
	}`, other.JSONString())
}

func TestLogOtherUnmarshalScopesLegacySensitiveFieldsAsAdmin(t *testing.T) {
	var other LogOther
	require.NoError(t, other.UnmarshalJSON([]byte(`{
		"request_path":"/v1/chat/completions",
		"reject_reason":"blocked",
		"channel_id":7
	}`)))

	require.JSONEq(t, `{
		"request_path":"/v1/chat/completions",
		"admin_info":{"reject_reason":"blocked","channel_id":7}
	}`, other.JSONString())
}

func TestLogOtherStructuredContentSurvivesAccountingOutbox(t *testing.T) {
	message := common.NewMessage("Operation {{action}}", map[string]any{"action": "render"})
	for _, value := range []any{message, []*common.Message{message}} {
		data, err := common.Marshal(RecordConsumeLogParams{Content: value})
		require.NoError(t, err)
		var restored RecordConsumeLogParams
		require.NoError(t, common.Unmarshal(data, &restored))
		other := NewLogOther()
		assert.Equal(t, "Operation render", other.setContent(restored.Content))
		assert.JSONEq(t, `{"content_parts":[{"key":"Operation {{action}}","params":{"action":"render"}}]}`, other.JSONString())
	}
	other := NewLogOther()
	assert.Equal(t, "历史日志", other.setContent("历史日志"))
	assert.JSONEq(t, `{}`, other.JSONString())
}
