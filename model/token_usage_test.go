package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenUsageValidity(t *testing.T) {
	now := int64(100)
	tests := []struct {
		name   string
		token  *Token
		valid  bool
		reason string
	}{
		{
			name:  "enabled token",
			token: &Token{Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1},
			valid: true,
		},
		{
			name:   "disabled token",
			token:  &Token{Status: common.TokenStatusDisabled, ExpiredTime: -1, RemainQuota: 1},
			reason: TokenUsageInvalidReasonDisabled,
		},
		{
			name:   "expired token",
			token:  &Token{Status: common.TokenStatusEnabled, ExpiredTime: now - 1, RemainQuota: 1},
			reason: TokenUsageInvalidReasonExpired,
		},
		{
			name:   "exhausted token",
			token:  &Token{Status: common.TokenStatusEnabled, ExpiredTime: -1},
			reason: TokenUsageInvalidReasonExhausted,
		},
		{
			name:  "unlimited token",
			token: &Token{Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.token)
			valid, reason := tt.token.GetUsageValidity(now)
			assert.Equal(t, tt.valid, valid)
			assert.Equal(t, tt.reason, reason)
		})
	}

	valid, reason := (*Token)(nil).GetUsageValidity(now)
	assert.False(t, valid)
	assert.Equal(t, TokenUsageInvalidReasonStatusUnavailable, reason)
}
