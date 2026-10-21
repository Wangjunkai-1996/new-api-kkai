package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayJSONQuantities(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"omitted", `{"input":"hello"}`, true},
		{"null and zero", `{"max_tokens":null,"max_output_tokens":0,"n":0,"candidateCount":null}`, true},
		{"boundaries", fmt.Sprintf(`{"max_tokens":%d,"n":%d}`, dto.MaxTokensLimit, dto.MaxImageN), true},
		{"decimal and exponent integers", `{"max_tokens":1.00e2,"max_output_tokens":10e-1,"n":128.0}`, true},
		{"negative zero", `{"max_tokens":-0,"n":-0.0e99999999999999999999}`, true},
		{"negative", `{"max_tokens":-1}`, false},
		{"fraction", `{"max_output_tokens":1.5}`, false},
		{"fraction rounding", `{"max_output_tokens":1.00000000000000001}`, false},
		{"fraction near bound", fmt.Sprintf(`{"max_tokens":%d.000000000000001}`, dto.MaxTokensLimit), false},
		{"token overflow", fmt.Sprintf(`{"max_completion_tokens":%d}`, dto.MaxTokensLimit+1), false},
		{"count overflow", fmt.Sprintf(`{"n":%d}`, dto.MaxImageN+1), false},
		{"string", `{"n":"1"}`, false},
		{"bool", `{"n":true}`, false},
		{"object", `{"n":{"value":1}}`, false},
		{"large exponent", `{"max_tokens":1e99999999999999999999}`, false},
		{"small exponent", `{"max_tokens":1e-99999999999999999999}`, false},
		{"int64 exponent", `{"max_tokens":1e-9223372036854775808}`, false},
		{"duplicate quantity", `{"max_tokens":1,"max_tokens":2}`, false},
		{"escaped duplicate", `{"n":1,"\u006e":2}`, false},
		{"duplicate container", `{"parameters":{},"parameters":{}}`, false},
		{"unknown duplicates", `{"extension":1,"extension":2}`, true},
		{"configs", `{"generationConfig":{"maxOutputTokens":100,"candidateCount":2},"generation_config":{"candidate_count":1},"parameters":{"max_new_tokens":10},"inferenceConfig":{"maxTokens":10},"options":{"num_predict":10}}`, true},
		{"config count overflow", `{"generationConfig":{"candidateCount":129}}`, false},
		{"config token overflow", `{"parameters":{"max_new_tokens":9999999999999999999}}`, false},
		{"options token overflow", `{"options":{"num_predict":-1}}`, false},
		{"batch", `{"requests":[{"generationConfig":{"candidateCount":1}},{"parameters":{"max_new_tokens":5}}]}`, true},
		{"batch count overflow", `{"requests":[{"generationConfig":{"candidateCount":129}}]}`, false},
		{"nested batch overflow", `{"requests":[{"requests":[{"max_tokens":-1}]}]}`, false},
		{"opaque schemas and content", `{"tools":[{"parameters":{"properties":{"n":{"type":"integer","maximum":9999999999999999},"max_tokens":{"const":-1}}}}],"input":[{"content":"max_tokens=-10"}],"generationConfig":{"responseSchema":{"n":-1,"max_tokens":-1}},"parameters":{"response_schema":{"n":-1}},"options":{"schema":{"num_predict":-1}}}`, true},
		{"nonobject protocol handling", `[null,1,"n"]`, true},
		{"nonobject containers", `{"generationConfig":[],"options":null,"requests":[null,1]}`, true},
		{"invalid JSON", `{"n":`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRelayJSONQuantities([]byte(test.body))
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, field := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "max_tokens_to_sample", "max_new_tokens", "maxTokens", "maxOutputTokens", "n", "candidateCount", "candidate_count"} {
		t.Run(field+" known at request and config", func(t *testing.T) {
			for _, body := range []string{fmt.Sprintf(`{%q:-1}`, field), fmt.Sprintf(`{"parameters":{%q:-1}}`, field)} {
				require.Error(t, ValidateRelayJSONQuantities([]byte(body)))
			}
		})
	}
}

type quantityTrackingBody struct {
	io.Reader
	bytesRead int
	closed    bool
}

func (b *quantityTrackingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytesRead += n
	return n, err
}

func (b *quantityTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestRelayOutboundQuantities(t *testing.T) {
	const payload = `{"input":"hello","max_output_tokens":10}`
	t.Run("GetBody preserves original body and replay metadata", func(t *testing.T) {
		original := &quantityTrackingBody{Reader: strings.NewReader(payload)}
		clone := &quantityTrackingBody{Reader: strings.NewReader(payload)}
		request, err := http.NewRequest(http.MethodPost, "http://upstream/responses", original)
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.ContentLength = int64(len(payload))
		request.GetBody = func() (io.ReadCloser, error) { return clone, nil }
		require.NoError(t, ValidateRelayOutboundQuantities(request))
		assert.Same(t, original, request.Body)
		assert.Zero(t, original.bytesRead)
		assert.False(t, original.closed)
		assert.True(t, clone.closed)
		assert.EqualValues(t, len(payload), request.ContentLength)
		assert.NotNil(t, request.GetBody)
		actual, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.Equal(t, payload, string(actual))
		require.NoError(t, request.Body.Close())
		assert.True(t, original.closed)
	})
	t.Run("nonreplayable body keeps bytes and original closer", func(t *testing.T) {
		original := &quantityTrackingBody{Reader: strings.NewReader(payload)}
		request, err := http.NewRequest(http.MethodPost, "http://upstream/responses", original)
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.ContentLength = int64(len(payload))
		require.NoError(t, ValidateRelayOutboundQuantities(request))
		assert.Nil(t, request.GetBody)
		assert.EqualValues(t, len(payload), request.ContentLength)
		assert.False(t, original.closed)
		actual, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.Equal(t, payload, string(actual))
		require.NoError(t, request.Body.Close())
		assert.True(t, original.closed)
	})
	t.Run("shared resource bound limits reads and preserves remaining bytes", func(t *testing.T) {
		previous := constant.MaxRequestBodyMB
		constant.MaxRequestBodyMB = 1
		t.Cleanup(func() { constant.MaxRequestBodyMB = previous })
		body := bytes.Repeat([]byte("x"), 2<<20)
		original := &quantityTrackingBody{Reader: bytes.NewReader(body)}
		request, err := http.NewRequest(http.MethodPost, "http://upstream/responses", original)
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		require.ErrorContains(t, ValidateRelayOutboundQuantities(request), "shared request body limit")
		assert.Equal(t, (1<<20)+1, original.bytesRead)
		assert.False(t, original.closed)
		actual, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.Equal(t, body, actual)
		require.NoError(t, request.Body.Close())
		assert.True(t, original.closed)
	})
	t.Run("30 MiB stays within shared limit", func(t *testing.T) {
		previous := constant.MaxRequestBodyMB
		constant.MaxRequestBodyMB = 0 // The same default as common.GetRequestBody.
		t.Cleanup(func() { constant.MaxRequestBodyMB = previous })
		body := append([]byte(`{"input":"`), bytes.Repeat([]byte("x"), 30<<20)...)
		body = append(body, []byte(`","max_output_tokens":10}`)...)
		request, err := http.NewRequest(http.MethodPost, "http://upstream/responses", bytes.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		require.NoError(t, ValidateRelayOutboundQuantities(request))
		actual, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.Equal(t, body, actual)
		require.NoError(t, request.Body.Close())
	})
	for _, test := range []struct {
		name        string
		contentType string
		encoding    string
		body        string
		valid       bool
		unread      bool
	}{
		{"multipart unchanged", "multipart/form-data; boundary=x", "", `n=-1`, true, true},
		{"nonJSON unchanged", "text/plain", "", `{"n":-1}`, true, true},
		{"missing type plain unchanged", "", "", `plain`, true, false},
		{"missing type JSON checked", "", "", `{"n":-1}`, false, false},
		{"vendor JSON checked", "application/vnd.test+json; charset=utf-8", "identity", `{"n":-1}`, false, false},
		{"encoded JSON rejected without reading", "application/json", "gzip", payload, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := &quantityTrackingBody{Reader: strings.NewReader(test.body)}
			request, err := http.NewRequest(http.MethodPost, "http://upstream/responses", original)
			require.NoError(t, err)
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("Content-Encoding", test.encoding)
			err = ValidateRelayOutboundQuantities(request)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if test.unread {
				assert.Zero(t, original.bytesRead)
			}
			assert.False(t, original.closed)
			actual, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			assert.Equal(t, test.body, string(actual))
			require.NoError(t, request.Body.Close())
			assert.True(t, original.closed)
		})
	}
}
