package perfmetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusRetainsDeliveredResponseAfterConnectionCloses(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	originalEnabled, originalRedis := settings.Enabled, common.RedisEnabled
	settings.Enabled, common.RedisEnabled = true, false
	t.Cleanup(func() { settings.Enabled, common.RedisEnabled = originalEnabled, originalRedis })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", UsingGroup: t.Name(), StartTime: time.Now()}
	body := []byte(`{"data":[{"url":"https://example.invalid/image.png"}]}`)
	service.IOCopyBytesGracefully(c, nil, body)
	require.Equal(t, http.StatusOK, writer.Code)
	require.Equal(t, body, writer.Body.Bytes())
	require.True(t, writer.Flushed)
	// The response is already delivered while settlement is still in progress.
	cancel()
	perfmetrics.RecordRelayResult(c.Request.Context(), info, nil)
	events := perfmetrics.QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events
	require.Len(t, events, 1)
	assert.True(t, events[0].Success)
}
