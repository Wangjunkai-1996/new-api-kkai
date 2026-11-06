package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaiattribution"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recoveryProtocolTestAdaptor struct {
	Adaptor
	url string
}

func TestManagedRecoveryContextPreservesParentDeadlineAndCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
			ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
	request, cancel := upstreamPolicyRequestContext(parent, info)
	defer cancel()
	_, ok := request.Deadline()
	assert.False(t, ok)
	next, cancelNext := upstreamPolicyRequestContext(parent, info)
	defer cancelNext()
	_, ok = next.Deadline()
	assert.False(t, ok)
	boundedParent, stop := context.WithTimeout(context.Background(), time.Hour)
	defer stop()
	bounded, cancelBounded := upstreamPolicyRequestContext(boundedParent, info)
	defer cancelBounded()
	parentDeadline, _ := boundedParent.Deadline()
	boundedDeadline, ok := bounded.Deadline()
	require.True(t, ok)
	assert.Equal(t, parentDeadline, boundedDeadline)
	nonStream := *info
	nonStream.IsStream = false
	nonStream.StartTime = time.Now().Add(-time.Minute)
	buffered, cancelBuffered := upstreamPolicyRequestContext(parent, &nonStream)
	defer cancelBuffered()
	bufferedDeadline, ok := buffered.Deadline()
	require.True(t, ok)
	assert.Equal(t, nonStream.StartTime.Add(managedRecoveryNonStreamTimeout), bufferedDeadline)
	nonStream.StartTime = time.Now().Add(-managedRecoveryNonStreamTimeout - time.Second)
	expired, cancelExpired := upstreamPolicyRequestContext(parent, &nonStream)
	defer cancelExpired()
	assert.ErrorIs(t, expired.Err(), context.DeadlineExceeded)
	cancelParent()
	assert.ErrorIs(t, request.Err(), context.Canceled)
	assert.ErrorIs(t, next.Err(), context.Canceled)
	assert.ErrorIs(t, buffered.Err(), context.Canceled)
}

func TestManagedRecoveryCancelsSub2AfterHeadersWithoutWaitingForPolicyDrain(t *testing.T) {
	service.InitHttpClient()
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-KKAI-Recovery-Protocol", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	var err error
	internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), UserId: 1, TokenId: 2, RequestId: "recovery-cancel", OriginModelName: "gpt-test",
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
			ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
	resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, strings.NewReader(`{}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	cancel()
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("Sub2 request kept running after downstream cancellation")
	}
}

func (a *recoveryProtocolTestAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a *recoveryProtocolTestAdaptor) SetupRequestHeader(_ *gin.Context, header *http.Header, _ *relaycommon.RelayInfo) error {
	header.Set("X-KKAI-Recovery-Protocol", "forged")
	return nil
}

func TestManagedRecoverySelectsProtocolFromOutboundBody(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("X-KKAI-Recovery-Protocol")
		if r.Header.Get("X-KKAI-Recovery-Protocol") == "1" {
			w.Header().Set("X-KKAI-Recovery-Protocol", "1")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	var err error
	internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		mode      int
		body      string
		want      string
		wantEntry string
	}{
		{"responses_json", relayconstant.RelayModeResponses, `{"text":{"format":{"type":"json_schema"}},"input":"hello"}`, "", "excluded_json_output"},
		{"responses_legacy_json", relayconstant.RelayModeResponses, `{"text":{"format":{"type":"text"}},"response_format":{"type":"json_object"},"input":"hello"}`, "", "excluded_json_output"},
		{"chat_json", relayconstant.RelayModeChatCompletions, `{"response_format":{"type":"json_object"},"messages":[]}`, "", "excluded_json_output"},
		{"responses_text_with_tools", relayconstant.RelayModeResponses, `{"tools":[{"type":"function","name":"lookup"}],"tool_choice":"auto","input":"hello"}`, "1", "handshake_succeeded"},
		{"chat_text_with_tools", relayconstant.RelayModeChatCompletions, `{"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto","messages":[]}`, "1", "handshake_succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RequestId: "recovery-shape", OriginModelName: "gpt-test",
				RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
					ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
			resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, strings.NewReader(tc.body))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tc.want, <-seen)
			assert.Equal(t, tc.want == "1", info.ManagedRecoveryEnabled())
			assert.Equal(t, tc.want == "1", c.GetBool("kkai_managed_recovery_v1"))
			assert.Equal(t, tc.wantEntry, c.GetString("kkai_managed_recovery_entry"))
		})
	}
}

func TestManagedRecoveryDiagnosticsHeaderIsCapturedAndStripped(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordManagedRecoveryDiagnostics(c, `{"same_account_retries":1,"account_switches":2,"outcome":"completed"}`)
	diagnostics, ok := c.Get("kkai_recovery_v1_diagnostics")
	require.True(t, ok)
	assert.Equal(t, map[string]any{"same_account_retries": 1, "account_switches": 2, "final_outcome": "completed", "recovered": true}, diagnostics)
	invalid, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordManagedRecoveryDiagnostics(invalid, `{"same_account_retries":99,"account_switches":0,"outcome":"completed"}`)
	_, ok = invalid.Get("kkai_recovery_v1_diagnostics")
	assert.False(t, ok)
}

func TestManagedRecoveryProtocolRequiresSignedChannelAndResponseEcho(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	for _, tc := range []struct {
		name      string
		signed    bool
		echo      string
		status    int
		wantErr   bool
		wantEntry string
	}{
		{"negotiated", true, "1", http.StatusOK, false, "handshake_succeeded"},
		{"missing_echo", true, "", http.StatusOK, true, "protocol_header_missing"},
		{"unsupported_echo", true, "2", http.StatusOK, true, "protocol_header_missing"},
		{"upstream_429", true, "", http.StatusTooManyRequests, false, "upstream_non_2xx"},
		{"missing_signature", false, "1", http.StatusOK, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seenProtocol := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenProtocol <- r.Header.Get("X-KKAI-Recovery-Protocol")
				w.Header().Set("X-KKAI-Recovery-Protocol", tc.echo)
				w.WriteHeader(tc.status)
			}))
			defer upstream.Close()
			internalAttributionSigner = nil
			if tc.signed {
				var err error
				internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
				require.NoError(t, err)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				UserId: 1, TokenId: 2, RequestId: "recovery-test", OriginModelName: "gpt-test",
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
					ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}},
			}
			resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, strings.NewReader(`{}`))
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				assert.False(t, c.GetBool("kkai_managed_recovery_v1"))
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Empty(t, resp.Header.Get("X-KKAI-Recovery-Protocol"))
				assert.Equal(t, tc.status == http.StatusOK, c.GetBool("kkai_managed_recovery_v1"))
				_ = resp.Body.Close()
			}
			if tc.signed {
				assert.Equal(t, "1", <-seenProtocol)
			} else {
				assert.Empty(t, seenProtocol)
			}
			assert.Equal(t, tc.wantEntry, c.GetString("kkai_managed_recovery_entry"))
		})
	}
}

func TestManagedRecoveryCapabilityFallbackReplaysOnceWithoutProtocol(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	var headers []http.Header
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		headers = append(headers, r.Header.Clone())
		bodies = append(bodies, string(body))
		if r.Header.Get("X-KKAI-Recovery-Protocol") == "1" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"error":{"type":"recovery_capability_unavailable","message":"Managed recovery is unavailable for this response format"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	var err error
	internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RequestId: "recovery-fallback", OriginModelName: "gpt-test",
		RelayMode: relayconstant.RelayModeResponses, IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
			ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
	payload := `{"model":"gpt-test","stream":true,"input":"hello"}`
	body, closer, err := relaycommon.NewOutboundJSONBody([]byte(payload))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, terminalData, found := strings.Cut(string(data), "data: ")
	require.True(t, found)
	terminalData, _, found = strings.Cut(terminalData, "\n")
	require.True(t, found)
	var terminal dto.ResponsesStreamResponse
	require.NoError(t, commonpkg.UnmarshalJsonStr(terminalData, &terminal))
	assert.Equal(t, "response.completed", terminal.Type)
	require.NotNil(t, terminal.Response)
	require.NotNil(t, terminal.Response.Usage)
	assert.Equal(t, 3, terminal.Response.Usage.TotalTokens)
	assert.Contains(t, string(data), "data: [DONE]")
	assert.Equal(t, []string{payload, payload}, bodies)
	require.Len(t, headers, 2)
	assert.Equal(t, "1", headers[0].Get("X-KKAI-Recovery-Protocol"))
	assert.Empty(t, headers[1].Get("X-KKAI-Recovery-Protocol"))
	assert.NotEmpty(t, headers[0].Get(kkaiattribution.SignatureHeader))
	assert.NotEqual(t, headers[0].Get(kkaiattribution.NonceHeader), headers[1].Get(kkaiattribution.NonceHeader))
	assert.False(t, info.ManagedRecoveryEnabled())
	assert.False(t, c.GetBool("kkai_managed_recovery_v1"))
	assert.Equal(t, "capability_fallback", c.GetString("kkai_managed_recovery_entry"))
}

func TestManagedRecoveryCapabilityFallbackIgnoresOther422AndNonReplayableBody(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	for _, tc := range []struct {
		name      string
		body      io.Reader
		errorType string
		wantEntry string
	}{
		{"unrelated 422", strings.NewReader(`{"input":"hello"}`), "invalid_request_error", "upstream_non_2xx"},
		{"nonreplayable body", struct{ io.Reader }{strings.NewReader(`{"input":"hello"}`)}, "recovery_capability_unavailable", "excluded_non_replayable_body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":{"type":"`+tc.errorType+`","message":"Managed recovery is unavailable for this response format"}}`)
			}))
			defer upstream.Close()
			var err error
			internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RequestId: "recovery-422", OriginModelName: "gpt-test",
				RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
					ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
			resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, tc.body)
			require.NoError(t, err)
			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, 1, calls)
			assert.Equal(t, tc.wantEntry, c.GetString("kkai_managed_recovery_entry"))
		})
	}
}

func TestManagedRecoveryCapabilityFallbackRedispatchesReservedMembership(t *testing.T) {
	c, info := membershipTransportFixture(t)
	info.UserId, info.TokenId = 7, 2
	info.OriginModelName = "gpt-test"
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true
	info.ChannelMeta.ChannelType = constant.ChannelTypeSub2API
	info.ChannelMeta.ChannelSetting.ManagedRecoveryV1 = true
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-KKAI-Recovery-Protocol") == "1" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"error":{"type":"recovery_capability_unavailable","message":"Managed recovery is unavailable for this response format"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"resp_1","status":"completed"}`)
	}))
	t.Cleanup(upstream.Close)
	var err error
	internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, 2, requests)
	assert.False(t, info.BillingOutcomeUnknown)
	record, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
	assert.NotZero(t, record.DispatchedAt)
	assert.Equal(t, info.ChannelId, record.ReviewChannelID)
}

func TestManagedRecoveryChecksFinalResponsesInput(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	requests := 0
	protocol := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		protocol = r.Header.Get("X-KKAI-Recovery-Protocol")
		if protocol == "1" {
			w.Header().Set("X-KKAI-Recovery-Protocol", "1")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	var err error
	internalAttributionSigner, err = kkaiattribution.NewSigner([]string{upstream.URL}, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	for _, tc := range []struct {
		name      string
		body      string
		want      string
		wantEntry string
	}{
		{"encrypted outbound", `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, "", "excluded_encrypted_input"},
		{"plaintext outbound", `{"input":"hello"}`, "1", "handshake_succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RequestId: "recovery-opaque", OriginModelName: "gpt-test",
				RelayMode: relayconstant.RelayModeResponses,
				Request:   &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"reasoning","encrypted_content":"opaque"}]`)},
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
					ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
			resp, err := DoApiRequest(&recoveryProtocolTestAdaptor{url: upstream.URL + "/v1/responses"}, c, info, strings.NewReader(tc.body))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tc.want, protocol)
			assert.Equal(t, tc.want == "1", info.ManagedRecoveryEnabled())
			assert.Equal(t, tc.wantEntry, c.GetString("kkai_managed_recovery_entry"))
		})
	}
	assert.Equal(t, 2, requests)
}
