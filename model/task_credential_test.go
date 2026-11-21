package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveChannelCredentialPreservesTaskScope(t *testing.T) {
	tests := []struct {
		name    string
		data    TaskPrivateData
		channel Channel
		want    string
		ok      bool
	}{
		{name: "keyless submission", data: TaskPrivateData{CredentialDigest: CredentialDigest("")}, ok: true},
		{name: "keyless submission after introducing a key", data: TaskPrivateData{CredentialDigest: CredentialDigest("")}, channel: Channel{Key: "new"}},
		{name: "keyless legacy channel", ok: true},
		{name: "legacy single key", channel: Channel{Key: "one"}, want: "one", ok: true},
		{name: "legacy frozen key", data: TaskPrivateData{Key: "saved"}, channel: Channel{Key: "new"}, want: "saved", ok: true},
		{name: "ambiguous legacy multi key", channel: Channel{Key: "one\ntwo", ChannelInfo: ChannelInfo{IsMultiKey: true}}},
		{name: "stale cached key cannot restore removed key", data: TaskPrivateData{CredentialDigest: CredentialDigest("old"), Key: "old"}, channel: Channel{Key: "new", Keys: []string{"old"}, ChannelInfo: ChannelInfo{IsMultiKey: true}}},
		{name: "structured key survives reorder", data: TaskPrivateData{CredentialDigest: CredentialDigest(`{"id":1}`)}, channel: Channel{Key: `[{"id":2},{"id":1}]`, ChannelInfo: ChannelInfo{IsMultiKey: true}}, want: `{"id":1}`, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{PrivateData: tt.data}
			key, ok := task.ResolveChannelCredential(&tt.channel)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, key)
		})
	}
}
