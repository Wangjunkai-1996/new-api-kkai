package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRateLimitLookupPrefersID(t *testing.T) {
	original := ModelRequestRateLimitUser2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRequestRateLimitUserByJSONString(original))
	})

	require.NoError(t, UpdateModelRequestRateLimitUserByJSONString(`{
		"username:alice":[20,10],
		"id:42":[8,6]
	}`))

	total, success, found := GetUserRateLimit(42, "alice")
	require.True(t, found)
	assert.Equal(t, 8, total)
	assert.Equal(t, 6, success)

	total, success, found = GetUserRateLimit(7, "alice")
	require.True(t, found)
	assert.Equal(t, 20, total)
	assert.Equal(t, 10, success)

	_, _, found = GetUserRateLimit(7, "bob")
	assert.False(t, found)
}

func TestCheckModelRequestRateLimitUser(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: `{"id:1":[0,1],"username:alice":[100,80]}`},
		{name: "invalid json", value: `{`, wantErr: true},
		{name: "null", value: `null`, wantErr: true},
		{name: "unknown selector", value: `{"alice":[10,8]}`, wantErr: true},
		{name: "zero id", value: `{"id:0":[10,8]}`, wantErr: true},
		{name: "non canonical id", value: `{"id:01":[10,8]}`, wantErr: true},
		{name: "empty username", value: `{"username:":[10,8]}`, wantErr: true},
		{name: "spaced username", value: `{"username: alice":[10,8]}`, wantErr: true},
		{name: "negative total", value: `{"id:1":[-1,8]}`, wantErr: true},
		{name: "zero success", value: `{"id:1":[10,0]}`, wantErr: true},
		{name: "extra value", value: `{"id:1":[10,8,6]}`, wantErr: true},
		{name: "overflow", value: `{"id:1":[2147483648,8]}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckModelRequestRateLimitUser(test.value)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestInvalidUserRateLimitUpdatePreservesCurrentConfig(t *testing.T) {
	original := ModelRequestRateLimitUser2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRequestRateLimitUserByJSONString(original))
	})

	require.NoError(t, UpdateModelRequestRateLimitUserByJSONString(`{"id:9":[12,10]}`))
	require.Error(t, UpdateModelRequestRateLimitUserByJSONString(`{"id:0":[1,1]}`))

	total, success, found := GetUserRateLimit(9, "")
	require.True(t, found)
	assert.Equal(t, 12, total)
	assert.Equal(t, 10, success)
}
