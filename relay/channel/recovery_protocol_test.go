package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
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

func TestManagedRecoveryContextKeepsOriginalDeadlineAndParentCancellation(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	info := &relaycommon.RelayInfo{StartTime: startedAt, RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
			ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}}}
	request, cancel := upstreamPolicyRequestContext(parent, info)
	defer cancel()
	deadline, ok := request.Deadline()
	require.True(t, ok)
	assert.Equal(t, startedAt.Add(relaycommon.ManagedRecoveryRequestTimeout), deadline)
	next, cancelNext := upstreamPolicyRequestContext(parent, info)
	defer cancelNext()
	nextDeadline, ok := next.Deadline()
	require.True(t, ok)
	assert.Equal(t, deadline, nextDeadline)
	cancelParent()
	assert.ErrorIs(t, request.Err(), context.Canceled)
	assert.ErrorIs(t, next.Err(), context.Canceled)
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

func TestManagedRecoveryProtocolRequiresSignedChannelAndResponseEcho(t *testing.T) {
	service.InitHttpClient()
	previousSigner := internalAttributionSigner
	t.Cleanup(func() { internalAttributionSigner = previousSigner })
	for _, tc := range []struct {
		name    string
		signed  bool
		echo    string
		status  int
		wantErr bool
	}{
		{"negotiated", true, "1", http.StatusOK, false},
		{"missing_echo", true, "", http.StatusOK, true},
		{"unsupported_echo", true, "2", http.StatusOK, true},
		{"upstream_429", true, "", http.StatusTooManyRequests, false},
		{"missing_signature", false, "1", http.StatusOK, true},
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
		})
	}
}
