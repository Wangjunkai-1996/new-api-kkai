package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateUserTokenIPUsesTokenAllowList(t *testing.T) {
	allowIPs := "10.0.0.0/8\n192.0.2.10"
	token := &Token{AllowIps: &allowIPs}

	require.NoError(t, ValidateUserTokenIP(token, "10.1.2.3"))
	require.NoError(t, ValidateUserTokenIP(token, "192.0.2.10"))
	require.ErrorIs(t, ValidateUserTokenIP(token, "203.0.113.8"), ErrTokenIPNotAllowed)
	require.ErrorIs(t, ValidateUserTokenIP(token, "not-an-ip"), ErrTokenClientIPInvalid)
}

func TestValidateUserTokenIPAllowsAnyClientWithoutRestrictions(t *testing.T) {
	require.NoError(t, ValidateUserTokenIP(&Token{}, ""))
}
