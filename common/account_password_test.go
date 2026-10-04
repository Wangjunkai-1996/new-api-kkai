package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountPasswordVerificationSupportsStoredHashAlgorithms(t *testing.T) {
	for _, algorithm := range []string{"bcrypt", "argon2id"} {
		t.Run(algorithm, func(t *testing.T) {
			t.Setenv("ACCOUNT_PASSWORD_HASH_ALGORITHM", algorithm)
			hash, err := HashAccountPassword("CorrectPassword123")
			require.NoError(t, err)
			assert.True(t, ValidatePasswordAndHash("CorrectPassword123", hash))
			assert.False(t, ValidatePasswordAndHash("WrongPassword123", hash))
		})
	}
}
