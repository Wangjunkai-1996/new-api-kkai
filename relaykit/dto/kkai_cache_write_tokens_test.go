package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKKAICacheCreationTokensTotalDoesNotDoubleCount(t *testing.T) {
	tests := []struct {
		name    string
		details InputTokenDetails
		want    int
	}{
		{
			name: "native value wins",
			details: InputTokenDetails{
				CachedCreationTokens: 80,
				CacheWriteTokens:     100,
			},
			want: 100,
		},
		{
			name: "compatibility value wins",
			details: InputTokenDetails{
				CachedCreationTokens: 100,
				CacheWriteTokens:     80,
			},
			want: 100,
		},
		{
			name: "negative values cannot reduce a charge",
			details: InputTokenDetails{
				CachedCreationTokens: -10,
				CacheWriteTokens:     -20,
			},
			want: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.details.CacheCreationTokensTotal())
		})
	}
}
