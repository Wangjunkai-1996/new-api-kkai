package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compressedRelayBody(t *testing.T, encoding string, payload []byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	var writer io.WriteCloser
	if encoding == "gzip" {
		writer = gzip.NewWriter(&encoded)
	} else {
		writer = brotli.NewWriter(&encoded)
	}
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return encoded.Bytes()
}

func TestDecompressRequestBodyMetadataAndReplay(t *testing.T) {
	payload := []byte(`{"model":"ordinary-model","input":"` + strings.Repeat("hello ", 256) + `"}`)
	for _, encoding := range []string{"gzip", "br"} {
		for _, knownLength := range []bool{true, false} {
			t.Run(encoding+"/known_length="+strconv.FormatBool(knownLength), func(t *testing.T) {
				encoded := compressedRelayBody(t, encoding, payload)
				router := gin.New()
				router.POST("/v1/responses", DecompressRequestMiddleware(), BodyStorageCleanup(), func(c *gin.Context) {
					assert.EqualValues(t, -1, c.Request.ContentLength, "the compressed length cannot describe the decoded body")
					assert.Empty(t, c.Request.Header.Get("Content-Length"))
					assert.Empty(t, c.Request.Header.Get("Content-Encoding"))
					for range 2 {
						storage, err := common.GetBodyStorage(c)
						require.NoError(t, err)
						raw, err := io.ReadAll(storage)
						require.NoError(t, err)
						assert.Equal(t, payload, raw)
					}
					c.Status(http.StatusNoContent)
				})
				request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(encoded))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Content-Encoding", encoding)
				if knownLength {
					request.Header.Set("Content-Length", strconv.Itoa(len(encoded)))
				} else {
					request.ContentLength = -1
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				assert.Equal(t, http.StatusNoContent, response.Code)
			})
		}
	}
}

func TestCompressedRelayBodyRejectsInvalidAndOversizedBeforeDispatch(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousMax := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = previousMax })
	_, err := jsplugin.DefaultRegistry.Register(genericTaskPluginSource, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("generic-entry-test")) })
	payload := []byte(`{"model":"ordinary-model","input":"hello"}`)
	oversized := []byte(`{"model":"ordinary-model","input":"` + strings.Repeat("x", 1<<20) + `"}`)
	for _, encoding := range []string{"gzip", "br"} {
		valid := compressedRelayBody(t, encoding, payload)
		for _, testCase := range []struct {
			name   string
			body   []byte
			status int
		}{
			{name: "truncated", body: valid[:len(valid)-1], status: http.StatusBadRequest},
			{name: "invalid", body: []byte("not a compressed request"), status: http.StatusBadRequest},
			{name: "expanded limit", body: compressedRelayBody(t, encoding, oversized), status: http.StatusRequestEntityTooLarge},
		} {
			for _, withPlugin := range []bool{false, true} {
				t.Run(encoding+"/"+testCase.name+"/plugin="+strconv.FormatBool(withPlugin), func(t *testing.T) {
					reachedDispatch := false
					router := gin.New()
					handlers := []gin.HandlerFunc{DecompressRequestMiddleware(), BodyStorageCleanup()}
					if withPlugin {
						handlers = append(handlers, PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint())
					}
					handlers = append(handlers, Distribute(), func(c *gin.Context) {
						reachedDispatch = true
						c.Status(http.StatusNoContent)
					})
					router.POST("/v1/responses", handlers...)
					request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(testCase.body))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Content-Encoding", encoding)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					assert.Equal(t, testCase.status, response.Code)
					assert.False(t, reachedDispatch, "invalid uploads must stop before channel dispatch or billing")
				})
			}
		}
	}
}

type interruptedRelayBody struct {
	reads, closes int
	readErr       error
}

func (body *interruptedRelayBody) Read(data []byte) (int, error) {
	body.reads++
	if body.reads == 1 {
		return 0, body.readErr
	}
	return copy(data, `{"model":"ordinary-model"}`), io.EOF
}

func (body *interruptedRelayBody) Close() error {
	body.closes++
	return nil
}

func TestRelayBodyReadFailureIsNotRetriedAfterPluginFallback(t *testing.T) {
	require.NoError(t, i18n.Init())
	_, err := jsplugin.DefaultRegistry.Register(genericTaskPluginSource, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("generic-entry-test")) })
	for _, readErr := range []error{errors.New("upload interrupted"), context.Canceled} {
		t.Run(readErr.Error(), func(t *testing.T) {
			body := &interruptedRelayBody{readErr: readErr}
			router := gin.New()
			router.POST("/v1/chat/completions", DecompressRequestMiddleware(), BodyStorageCleanup(), func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{})
			}, PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), func(c *gin.Context) {
				_, cachedErr := common.GetBodyStorage(c)
				assert.ErrorIs(t, cachedErr, readErr)
			}, Distribute(), func(c *gin.Context) {
				t.Error("a failed upload reached dispatch")
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, 1, body.reads, "a consumed or closed upload must never be read again")
			assert.Equal(t, 1, body.closes)
			assert.Contains(t, response.Body.String(), readErr.Error())
		})
	}
}

func TestTaskPluginBodyParseMissPreservesOrdinaryFallback(t *testing.T) {
	_, err := jsplugin.DefaultRegistry.Register(genericTaskPluginSource, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("generic-entry-test")) })
	payload := []byte(`{"model":123,"messages":[]}`)
	reachedOrdinaryRoute := false
	router := gin.New()
	router.POST("/v1/chat/completions", DecompressRequestMiddleware(), BodyStorageCleanup(), PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), func(c *gin.Context) {
		reachedOrdinaryRoute = true
		_, pinned := c.Get(jsplugin.ContextKeyPinnedEndpoint)
		assert.False(t, pinned)
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		raw, err := storage.Bytes()
		require.NoError(t, err)
		assert.Equal(t, payload, raw, "an identification miss must preserve the ordinary validator's input")
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.True(t, reachedOrdinaryRoute)
	assert.Equal(t, http.StatusNoContent, response.Code)
}
