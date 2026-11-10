package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskMediaContentPathPreservesOperationStateAndSafeHeaders(t *testing.T) {
	oldLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = oldLimit })
	for _, tc := range []struct {
		name, method, body, want string
		status                   int
	}{
		{"inline", http.MethodGet, `{"done":true,"response":{"bytes":["dmlkZW8="]}}`, "video", 200},
		{"head", http.MethodHead, `{"done":true,"response":{"bytes":["dmlkZW8="]}}`, "", 200},
		{"url-safe", http.MethodGet, `{"status":"completed","response":{"bytes":["-_8="]}}`, "\xfb\xff", 200},
		{"running", http.MethodGet, `{"done":false}`, "", 409},
		{"failed", http.MethodGet, `{"done":true,"error":{"message":"secret"}}`, "", 502},
		{"gone", http.MethodGet, `{"done":true,"response":{}}`, "", 410},
		{"invalid", http.MethodGet, `{"done":true,"response":{"bytes":["!invalid!"]}}`, "", 502},
		{"oversize", http.MethodGet, `{"done":true,"response":{"bytes":["` + strings.Repeat("A", 3<<20) + `"]}}`, "", 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(tc.method, "/content", nil)
			err := writeTaskMediaContentPath(ctx, strings.NewReader(tc.body), &relaychannel.TaskContentRequest{ContentPath: []any{"response", "bytes", int64(0)}, MimeType: "video/webm"})
			if tc.status != 200 {
				var mediaErr *taskMediaProxyError
				require.ErrorAs(t, err, &mediaErr)
				assert.Equal(t, tc.status, mediaErr.status)
				assert.False(t, ctx.Writer.Written())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, recorder.Body.String())
			assert.Equal(t, "video/webm", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
			if tc.method == http.MethodHead {
				assert.Equal(t, "5", recorder.Header().Get("Content-Length"))
			}
		})
	}
}
