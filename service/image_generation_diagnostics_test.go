package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageUpstreamErrorDiagnostic(t *testing.T) {
	const natural = "抱歉，刚才图片生成服务出现错误，这次没能成功生成 BLG 夺冠海报。目前无法提供成品图片。"
	for _, tc := range []struct {
		name, message, key, want  string
		structuredOnly, truncated bool
	}{
		{name: "natural_reason", message: natural, want: natural},
		{name: "ordinary_braces_and_angles", message: "{width} must be < 2048", want: "{width} must be < 2048"},
		{name: "exact_key", message: "rejected active-provider-secret", key: "active-provider-secret", want: "rejected [redacted]"},
		{name: "escaped_exact_key", message: `rejected quoted\"secret`, key: `quoted"secret`, want: "rejected [redacted]"},
		{name: "authorization", message: "rejected Authorization: Bearer abcdef-secret", want: "rejected [redacted]"},
		{name: "basic_auth", message: "rejected authorization = Basic encoded-secret", want: "rejected [redacted]"},
		{name: "json_key", message: `rejected "API_KEY" : "json-secret"`, want: `rejected "[redacted]`},
		{name: "quoted_value", message: `rejected secret = 'two word secret'`, want: "rejected [redacted]"},
		{name: "equals_key", message: "rejected api_key=equals-secret", want: "rejected [redacted]"},
		{name: "colon_key", message: "rejected api_key:colon-secret", want: "rejected [redacted]"},
		{name: "sk_key", message: "rejected sk-other-provider-secret", want: "rejected [redacted]"},
		{name: "foreign_url", message: "rejected https://user:password@foreign.example/secret?key=query-secret#fragment-secret", want: "rejected [redacted URL]"},
		{name: "upstream_url", message: "rejected https://upstream.example/v1?key=query-secret#fragment-secret", want: "rejected [redacted URL]"},
		{name: "upstream_host_ip", message: "lookup upstream.example 10.0.0.1 [2001:db8::1]", want: "lookup ***.example ***.***.***.*** [***]"},
		{name: "controls", message: "request\r\nfailed\t\x1b\u202e", want: "request  failed"},
		{name: "control_reassembles_secret_field", message: "api_\u200bkey=hidden", structuredOnly: true},
		{name: "control_reassembles_url", message: "htt\u200bps:/\u200b/foreign.example/hidden", structuredOnly: true},
		{name: "control_reassembles_key", message: "active-\u200bsecret", key: "active-secret", structuredOnly: true},
		{name: "unterminated_secret", message: `rejected "api_key": "unfinished private words`, structuredOnly: true},
		{name: "json_document", message: `{"error":{"message":"private"}}`, structuredOnly: true},
		{name: "html_document", message: "<html><body>private</body></html>", structuredOnly: true},
		{name: "invalid_utf8", message: "invalid\xffsecret", structuredOnly: true},
		{name: "too_large", message: strings.Repeat("x", 64*1024+1), structuredOnly: true},
		{name: "utf8_limit", message: strings.Repeat("图", 800), want: strings.Repeat("图", 682), truncated: true},
		{name: "redact_before_limit", message: strings.Repeat("x", 2040) + "long-provider-secret-crossing-boundary", key: "long-provider-secret-crossing-boundary", want: strings.Repeat("x", 2040) + "[redacte", truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := types.WithOpenAIError(types.OpenAIError{Message: tc.message, Type: "upstream_error", Code: "image_provider_error"}, 400, types.ErrOptionWithOriginalStatusCode(502))
			before := *apiErr
			diagnostic := imageGenerationDiagnostic(apiErr, tc.key, []string{"https://upstream.example"}, false)
			assert.Equal(t, before, *apiErr, "diagnostics must not change outcome evidence")
			assert.Equal(t, 502, diagnostic.StatusCode)
			assert.Equal(t, "upstream_error", diagnostic.Type)
			assert.Equal(t, "image_provider_error", diagnostic.Code)
			assert.Equal(t, tc.want, diagnostic.Message)
			assert.Equal(t, tc.truncated, diagnostic.Truncated)
			assert.True(t, utf8.ValidString(diagnostic.Message))
			assert.LessOrEqual(t, len(diagnostic.Message), 2048)
			if tc.structuredOnly {
				encoded, err := common.Marshal(diagnostic)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), `"message"`)
			}
		})
	}

	t.Run("safe_metadata_and_wire_status_only", func(t *testing.T) {
		for _, tag := range []string{"unsafe value", "https://secret.example", strings.Repeat("a", 129), "active-secret", "sk-other-provider-secret", "api_key:upstream-secret", "password:secret"} {
			apiErr := types.WithOpenAIError(types.OpenAIError{Message: natural, Type: tag, Code: tag}, 502)
			diagnostic := imageGenerationDiagnostic(apiErr, "active-secret", nil, false)
			assert.Empty(t, diagnostic.Type, tag)
			assert.Empty(t, diagnostic.Code, tag)
			assert.Zero(t, diagnostic.StatusCode, "mapped/final HTTP status is not wire evidence")
		}
		apiErr := types.WithOpenAIError(types.OpenAIError{Message: natural, Type: "upstream_error", Code: map[string]any{"secret": "value"}}, 502)
		assert.Empty(t, imageGenerationDiagnostic(apiErr, "", nil, false).Code)
	})

	t.Run("policy_has_no_free_text", func(t *testing.T) {
		for _, code := range []string{"content_policy", "content_policy_violation", "request_policy_blocked"} {
			apiErr := types.WithOpenAIError(types.OpenAIError{Message: "private policy description", Type: "upstream_error", Code: code}, 400, types.ErrOptionWithOriginalStatusCode(400))
			before := *apiErr
			assert.Equal(t, "image request was rejected by a policy check", imageGenerationDiagnostic(apiErr, "", nil, false).Message)
			assert.Equal(t, before, *apiErr)
		}
		apiErr := types.WithOpenAIError(types.OpenAIError{Message: "private evidence description", Type: "upstream_error", Code: "unknown_error"}, 502, types.ErrOptionWithPolicyEvidence("policy evidence"))
		assert.Equal(t, "image request was rejected by a policy check", imageGenerationDiagnostic(apiErr, "", nil, false).Message)
	})
}

func TestImageAsyncErrorLogDiagnostics(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "image-diagnostic", Password: "password"}).Error)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousErrorLog, previousCache := constant.ErrorLogEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB, constant.ErrorLogEnabled, common.MemoryCacheEnabled = db, db, true, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &output, &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, constant.ErrorLogEnabled, common.MemoryCacheEnabled = previousDB, previousLogDB, previousErrorLog, previousCache
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
		common.LogWriterMu.Unlock()
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set("id", 7)
	c.Set("token_id", 17)
	c.Set(common.RequestIdKey, "image-diagnostic-request")
	const reason = "Image provider did not produce an image"
	apiErr := types.WithOpenAIError(types.OpenAIError{Message: reason + " active-secret Authorization: Bearer other-secret", Type: "upstream_error", Code: "image_provider_error"}, 502, types.ErrOptionWithOriginalStatusCode(502))
	before := *apiErr
	ProcessChannelErrorAfterPolicy(c, types.ChannelError{ChannelId: 3, UsingKey: "active-secret"}, apiErr, &relaycommon.RelayInfo{DurableImageExecution: true}, false)
	assert.Equal(t, before, *apiErr)
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "status_code=502, image provider request failed", logs[0].Content)
	assert.NotContains(t, output.String(), reason)
	for _, secret := range []string{"active-secret", "other-secret"} {
		assert.NotContains(t, logs[0].Other, secret)
		assert.NotContains(t, output.String(), secret)
	}
	var other struct {
		Admin struct {
			Diagnostic imageUpstreamErrorDiagnostic `json:"image_upstream_error"`
		} `json:"admin_info"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, reason+" [redacted] [redacted]", other.Admin.Diagnostic.Message)
	assert.Equal(t, 502, other.Admin.Diagnostic.StatusCode)
	tokenLogs, err := model.GetLogByTokenId(17)
	require.NoError(t, err)
	require.Len(t, tokenLogs, 1)
	assert.NotContains(t, tokenLogs[0].Other, "image_upstream_error")
	assert.NotContains(t, tokenLogs[0].Other, reason)
}

func TestImageAsyncMalformedResponseOmitsBodyFromSystemLog(t *testing.T) {
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})
	ctx := WithImageExecutionFence(context.Background(), 7, model.ImageExecutionFence{})
	apiErr := RelayErrorHandler(ctx, &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("<html>private malformed body bearer secret</html>"))}, false)
	require.NotNil(t, apiErr)
	assert.Equal(t, "bad response status code 502", apiErr.Error())
	assert.Contains(t, output.String(), "response body omitted")
	assert.NotContains(t, output.String(), "private")
	assert.NotContains(t, output.String(), "secret")
}
