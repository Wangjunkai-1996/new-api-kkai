package kkaiattribution

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSignerFromEnvironmentDisabledWhenUnset(t *testing.T) {
	t.Setenv(OriginsEnvironmentVariable, "")
	t.Setenv(SecretEnvironmentVariable, "")

	signer, err := NewSignerFromEnvironment()
	require.NoError(t, err)
	require.Nil(t, signer)
}

func TestNewSignerFromEnvironmentRejectsPartialOrWeakConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		origins string
		secret  string
	}{
		{name: "missing secret", origins: "https://guard.internal.example:8443"},
		{name: "missing origins", secret: attributionTestSecret},
		{name: "weak secret", origins: "https://guard.internal.example:8443", secret: "short"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(OriginsEnvironmentVariable, test.origins)
			t.Setenv(SecretEnvironmentVariable, test.secret)
			_, err := NewSignerFromEnvironment()
			require.ErrorIs(t, err, ErrInvalidConfiguration)
		})
	}
}

func TestNewSignerFromEnvironmentAcceptsCommaSeparatedOrigins(t *testing.T) {
	t.Setenv(OriginsEnvironmentVariable, "https://guard-a.internal.example:8443, https://guard-b.internal.example:9443")
	t.Setenv(SecretEnvironmentVariable, attributionTestSecret)

	signer, err := NewSignerFromEnvironment()
	require.NoError(t, err)
	require.NotNil(t, signer)
}
