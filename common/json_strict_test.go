package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalStrictRejectsAmbiguousWrites(t *testing.T) {
	type request struct {
		Version string            `json:"version"`
		Fields  map[string]string `json:"fields"`
	}
	for _, input := range []string{
		`{"version":"1","version":"2"}`,
		`{"version":"1","ver\u0073ion":"2"}`,
		`{"fields":{"password":"a","password":"b"}}`,
		`{"version":"1","amount":20}`,
		`{"version":"1"} {"version":"2"}`,
		`{"fields":{"password":"a"}`,
	} {
		t.Run(input, func(t *testing.T) {
			var value request
			assert.Error(t, UnmarshalStrict([]byte(input), &value))
		})
	}
	var value request
	require.NoError(t, UnmarshalStrict([]byte(`{"version":"1","fields":{"account":"a","password":"b"}}`), &value))
	assert.Equal(t, "1", value.Version)
	assert.Equal(t, map[string]string{"account": "a", "password": "b"}, value.Fields)
}
