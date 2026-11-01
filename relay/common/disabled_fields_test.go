package common

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRemoveDisabledFieldsPreservesUnrelatedJSON(t *testing.T) {
	input := []byte(`{"service_tier":"flex","store":true,"input":[{"id":9007199254740993,"content":"keep","service_tier":"nested"}],"tools":[{"type":"function","parameters":{"const":18446744073709551615,"multipleOf":0.1234567890123456789}}],"stream_options":{"include_obfuscation":false,"include_usage":false,"extension":9007199254740993}}`)
	out, err := RemoveDisabledFields(input, dto.ChannelOtherSettings{DisableStore: true}, false)
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(out, "service_tier").Exists())
	assert.False(t, gjson.GetBytes(out, "store").Exists())
	assert.False(t, gjson.GetBytes(out, "stream_options.include_obfuscation").Exists())
	for _, path := range []string{"input", "tools", "stream_options.include_usage", "stream_options.extension"} {
		assert.Equal(t, gjson.GetBytes(input, path).Raw, gjson.GetBytes(out, path).Raw, path)
	}
}

func TestRemoveDisabledFieldsStreamOptions(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"empty after filtering", `{"service_tier":"flex","stream_options":{"include_obfuscation":false}}`, `{}`},
		{"empty object", `{"service_tier":"flex","stream_options":{}}`, `{}`},
		{"null", `{"service_tier":"flex","stream_options":null}`, `{"stream_options":null}`},
		{"scalar", `{"service_tier":"flex","stream_options":false}`, `{"stream_options":false}`},
		{"array", `{"service_tier":"flex","stream_options":[]}`, `{"stream_options":[]}`},
		{"escaped and duplicate keys", `{"\u0073ervice_tier":"flex","service_tier":"priority","stream_options":{"include_obfuscation":true,"include_obfuscation":false,"include_usage":true,"include_usage":false}}`, `{"stream_options":{"include_usage":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RemoveDisabledFields([]byte(tc.input), dto.ChannelOtherSettings{}, false)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(out))
		})
	}
}

func BenchmarkRemoveDisabledFields(b *testing.B) {
	message := `{"role":"user","content":"` + strings.Repeat("Please review this function and preserve its behavior. ", 6) + `","metadata":{"line":17,"tags":["review","code"]}}`
	for _, tc := range []struct {
		name  string
		turns int
	}{{"short", 1}, {"long_history", 1024}} {
		body := []byte(`{"service_tier":"flex","input":[` + strings.Repeat(message+",", tc.turns-1) + message + `],"stream_options":{"include_obfuscation":false,"include_usage":true}}`)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				if _, err := RemoveDisabledFields(body, dto.ChannelOtherSettings{}, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
