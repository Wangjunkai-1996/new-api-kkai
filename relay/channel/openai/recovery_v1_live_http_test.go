package openai

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedRecoveryCapturedSub2StreamsOverLocalHTTPBeforeTerminal(t *testing.T) {
	service.InitHttpClient()
	data, err := os.ReadFile(filepath.Join("testdata", "sub2_recovery", "responses_auto_dispatch.sse"))
	require.NoError(t, err)
	firstDelta := strings.Index(string(data), `"delta":"abandoned text"`)
	require.NotEqual(t, -1, firstDelta)
	end := strings.Index(string(data)[firstDelta:], "\n\n")
	require.NotEqual(t, -1, end)
	firstChunk := data[:firstDelta+end+2]
	remaining := data[firstDelta+end+2:]
	releaseTerminal := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseTerminal) }) }
	defer release()
	upstreamHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-KKAI-Recovery-Protocol", "1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(firstChunk)
		w.(http.Flusher).Flush()
		select {
		case <-releaseTerminal:
			_, _ = w.Write(remaining)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)

	t.Cleanup(func() { _ = channel.InitInternalAttribution() })
	t.Setenv("KKAI_ATTRIBUTION_ORIGINS", upstream.URL)
	t.Setenv("KKAI_ATTRIBUTION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("KKAI_ATTRIBUTION_CONFIG_FILE", "")
	require.NoError(t, channel.InitInternalAttribution())

	type outcome struct {
		usage *dto.Usage
		err   *types.NewAPIError
		owner bool
	}
	finished := make(chan outcome, 1)
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) {
		info := &relaycommon.RelayInfo{
			StartTime: time.Now(), UserId: 1, TokenId: 2, RequestId: "local-recovery-http",
			OriginModelName: "gpt-test", RelayMode: relayconstant.RelayModeResponses,
			RelayFormat: types.RelayFormatOpenAIResponses, RequestURLPath: "/v1/responses",
			IsStream: true, DisablePing: true,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSub2API,
				ChannelBaseUrl: upstream.URL, ApiKey: "local-test-key", UpstreamModelName: "gpt-test",
				ChannelSetting: dto.ChannelSettings{ManagedRecoveryV1: true}},
		}
		resp, err := channel.DoApiRequest(&Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-test","stream":true,"input":"test"}`))
		if err != nil {
			finished <- outcome{err: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)}
			return
		}
		usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
		finished <- outcome{usage: usage, err: apiErr, owner: c.GetBool("kkai_managed_recovery_v1")}
	})
	downstream := httptest.NewServer(engine)
	t.Cleanup(downstream.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, downstream.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-test","stream":true,"input":"test"}`))
	require.NoError(t, err)
	resp, err := downstream.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	reader := bufio.NewReader(resp.Body)
	var first strings.Builder
	for !strings.Contains(first.String(), `"delta":"abandoned text"`) {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		first.WriteString(line)
	}
	assert.NotContains(t, first.String(), "kkai.recovery.v1")
	assert.NotContains(t, first.String(), "Regenerating")
	assert.NotContains(t, first.String(), "winner text")
	assert.Equal(t, "1", (<-upstreamHeaders).Get("X-KKAI-Recovery-Protocol"))
	release()
	tail, err := io.ReadAll(reader)
	require.NoError(t, err)
	output := first.String() + string(tail)
	assert.NotContains(t, output, "kkai.recovery.v1")
	assert.Contains(t, output, "Regenerating")
	assert.Contains(t, output, "winner text")
	assert.Equal(t, 1, strings.Count(output, "event: response.completed"))
	result := <-finished
	require.Nil(t, result.err)
	require.NotNil(t, result.usage)
	assert.True(t, result.owner)
	assert.Equal(t, 5, result.usage.TotalTokens)
}
