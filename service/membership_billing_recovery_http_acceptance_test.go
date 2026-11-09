//go:build membership_acceptance

package service_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const recoveryMeasurementRequest = `{"model":"test-model","messages":[{"role":"user","content":"Reply with hello world."}],"max_tokens":32,"stream":true,"stream_options":{"include_usage":true}}`

type recoveryMeasurementSQL struct {
	gormlogger.Interface
	count  atomic.Int64
	nanos  atomic.Int64
	errors atomic.Int64
}

func (m *recoveryMeasurementSQL) Trace(_ context.Context, start time.Time, _ func() (string, int64), err error) {
	m.count.Add(1)
	m.nanos.Add(time.Since(start).Nanoseconds())
	if err != nil && err != gorm.ErrRecordNotFound {
		m.errors.Add(1)
	}
}

type recoveryHTTPSample struct {
	Index             int     `json:"index"`
	FirstSSEMS        float64 `json:"first_sse_ms"`
	FirstContentMS    float64 `json:"first_content_ms"`
	CompleteMS        float64 `json:"complete_ms"`
	FirstContentEvent int     `json:"first_content_event"`
	RoleEvents        int     `json:"role_events"`
	Content           string  `json:"content"`
	Error             string  `json:"error,omitempty"`
}

type recoveryHTTPReport struct {
	Scope                string               `json:"scope"`
	Dialect              string               `json:"dialect"`
	Mode                 string               `json:"mode"`
	Funding              string               `json:"funding"`
	Users                int                  `json:"users"`
	Conflicts            int                  `json:"conflict_rows"`
	Concurrency          int                  `json:"concurrency"`
	PoolSize             int                  `json:"pool_size"`
	Requested            int                  `json:"requested"`
	Succeeded            int                  `json:"succeeded"`
	Valid                bool                 `json:"valid_functional_measurement"`
	Unsettled            int64                `json:"unsettled_http_requests"`
	RequestWallMS        float64              `json:"request_wall_ms"`
	ObservationWallMS    float64              `json:"observation_wall_ms"`
	Throughput           float64              `json:"requests_per_second"`
	WorkerMS             float64              `json:"worker_ms"`
	WorkerError          string               `json:"worker_error,omitempty"`
	SQLCount             int64                `json:"sql_count_including_worker"`
	SQLMS                float64              `json:"sql_sum_ms_including_worker"`
	SQLErrors            int64                `json:"sql_errors_including_worker"`
	PoolWaitCount        int64                `json:"pool_wait_count"`
	PoolWaitMS           float64              `json:"pool_wait_ms"`
	ContentP50MS         float64              `json:"content_p50_ms"`
	ContentP95MS         float64              `json:"content_p95_ms"`
	ContentP99MS         *float64             `json:"content_p99_ms,omitempty"`
	P99SufficientSamples bool                 `json:"p99_minimum_100_samples"`
	Notes                string               `json:"notes"`
	Samples              []recoveryHTTPSample `json:"samples"`
}

// This opt-in measurement uses real preconsumption, HTTP dispatch, TextHelper,
// the OpenAI adaptor/stream scanner and normal billing settlement. The test
// wrapper supplies authenticated user/channel metadata and counted input, so
// auth, distribution and prompt-tokenization latency are explicitly excluded.
// It reports observations, never declares a latency or throughput PASS.
func TestMembershipRecoveryHTTPMeasurement(t *testing.T) {
	if os.Getenv("KKAI_MEMBERSHIP_RECOVERY_MEASURE") != "1" {
		t.Skip("set KKAI_MEMBERSHIP_RECOVERY_MEASURE=1 for opt-in real relay measurement")
	}
	count, concurrency := 40, 4
	for key, target := range map[string]*int{"KKAI_MEMBERSHIP_RECOVERY_SAMPLES": &count, "KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY": &concurrency} {
		if raw := os.Getenv(key); raw != "" {
			value, err := strconv.Atoi(raw)
			require.NoError(t, err)
			require.Greater(t, value, 0)
			*target = value
		}
	}
	require.LessOrEqual(t, count, 1000)
	require.LessOrEqual(t, concurrency, 8)
	service.InitTokenEncoders()
	service.InitHttpClient()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, dialect := range []string{"postgres", "mysql"} {
		for _, members := range []bool{true, false} {
			funding := "wallet"
			if members {
				funding = "membership"
			}
			for _, users := range []int{1, 4} {
				for _, conflicts := range []int{0, 501} {
					// Alternating order across cells reduces a fixed first-run bias;
					// use repeated independent runs for an actual performance decision.
					modes := []string{"off", "apply"}
					if users == 4 {
						modes = []string{"apply", "off"}
					}
					for _, mode := range modes {
						name := fmt.Sprintf("%s/%s/users-%d/conflicts-%d/%s", dialect, funding, users, conflicts, mode)
						t.Run(name, func(t *testing.T) {
							f := service.NewMembershipRecoveryAcceptanceBridge(t, dialect, members, users, conflicts)
							f.SetMode(mode)
							report := measureMembershipRecoveryHTTP(t, f, count, concurrency)
							report.Dialect, report.Mode, report.Funding, report.Users, report.Conflicts = dialect, mode, funding, users, conflicts
							encoded, err := common.Marshal(report)
							require.NoError(t, err)
							if path := os.Getenv("KKAI_MEMBERSHIP_RECOVERY_OUTPUT"); path != "" {
								out, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
								require.NoError(t, err)
								_, err = out.Write(append(encoded, '\n'))
								require.NoError(t, err)
								require.NoError(t, out.Close())
							} else {
								t.Log("MEMBERSHIP_RECOVERY_MEASUREMENT_JSON " + string(encoded))
							}
							assert.Equal(t, count, report.Succeeded, "functional errors invalidate this measurement; no performance verdict is inferred")
							assert.Empty(t, report.WorkerError)
							assert.Zero(t, report.SQLErrors, "SQL failures invalidate this measurement even if HTTP streaming succeeded")
							assert.True(t, report.Valid)
						})
					}
				}
			}
		}
	}
}

func measureMembershipRecoveryHTTP(t *testing.T, f service.MembershipRecoveryAcceptanceBridge, count, concurrency int) recoveryHTTPReport {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		// No artificial delays: these exact protocol frames are the common
		// upstream workload for both modes, including a non-content first frame.
		for _, event := range []string{
			`{"id":"local-completion","created":1,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"local-completion","created":1,"model":"test-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
			`{"id":"local-completion","created":1,"model":"test-model","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"local-completion","created":1,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`{"id":"local-completion","created":1,"model":"test-model","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`,
			`[DONE]`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			w.(http.Flusher).Flush()
		}
	}))
	defer upstream.Close()
	var requestIndex atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(requestIndex.Add(1) - 1)
		user, token := f.Users[index%len(f.Users)], f.Tokens[index%len(f.Tokens)]
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil || string(body) != recoveryMeasurementRequest {
			http.Error(w, "unexpected fixed request", http.StatusBadRequest)
			return
		}
		var request dto.GeneralOpenAIRequest
		if err := common.Unmarshal(body, &request); err != nil {
			http.Error(w, "invalid fixture", http.StatusBadRequest)
			return
		}
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		defer common.CleanupBodyStorage(c)
		id := fmt.Sprintf("http-acceptance-%06d", index)
		c.Set(common.RequestIdKey, id)
		c.Set("username", user.Username)
		common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
		common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		common.SetContextKey(c, constant.ContextKeyChannelId, f.Channel.Id)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
		common.SetContextKey(c, constant.ContextKeyChannelKey, "acceptance-upstream-key")
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "test-model")
		enabled := true
		info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, RequestId: id,
			Request: &request, StartTime: time.Now(), RelayMode: relayconstant.RelayModeChatCompletions,
			RelayFormat: relaytypes.RelayFormatOpenAI, RequestURLPath: "/v1/chat/completions", OriginModelName: "test-model",
			UsingGroup: "gpt", IsStream: true, DisablePing: true, MembershipBillingEnabled: &enabled}
		info.InitChannelMeta(c)
		info.SetEstimatePromptTokens(16)
		info.PriceData = types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}
		if err := service.PrepareMembershipBudget(c, info, &info.PriceData, 16, &relaytypes.TokenCountMeta{MaxTokens: 32}); err != nil {
			t.Logf("request %s stage=budget: %s", id, common.MaskSensitiveInfo(err.Error()))
			w.Header().Set("X-Acceptance-Stage", "budget")
			w.Header().Set("X-Acceptance-Code", "budget_failed")
			http.Error(w, "budget failed", http.StatusInternalServerError)
			return
		}
		if apiErr := service.PreConsumeBilling(c, info.PriceData.QuotaToPreConsume, info); apiErr != nil {
			t.Logf("request %s stage=reservation: %s", id, apiErr.MaskSensitiveErrorWithStatusCode())
			w.Header().Set("X-Acceptance-Stage", "reservation")
			w.Header().Set("X-Acceptance-Code", string(apiErr.GetErrorCode()))
			http.Error(w, "reservation failed", apiErr.StatusCode)
			return
		}
		w.Header().Add("Trailer", "X-Acceptance-Result")
		if apiErr := relay.TextHelper(c, info); apiErr != nil {
			t.Logf("request %s stage=relay: %s", id, apiErr.MaskSensitiveErrorWithStatusCode())
			relay.RefundFailedRequestBilling(c, info, apiErr)
			w.Header().Set("X-Acceptance-Result", "relay:"+string(apiErr.GetErrorCode()))
			return
		}
		w.Header().Set("X-Acceptance-Result", "complete")
	}))
	defer proxy.Close()
	client := &http.Client{Transport: &http.Transport{MaxIdleConns: concurrency, MaxIdleConnsPerHost: concurrency}, Timeout: 15 * time.Second}
	defer client.CloseIdleConnections()
	for i := 0; i < 4; i++ {
		sample := readRecoveryHTTPSample(client, proxy.URL, i)
		require.Empty(t, sample.Error, "warm-up must traverse the real relay")
		require.Equal(t, "hello world", sample.Content)
		require.Positive(t, sample.RoleEvents)
		require.Greater(t, sample.FirstContentEvent, 1, "role-only SSE must not count as first content")
	}
	counts := &recoveryMeasurementSQL{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	f.DB.Logger = counts
	before := f.Pool.Stats()
	start := time.Now()
	startBarrier := make(chan struct{})
	workerDone := make(chan struct {
		elapsed time.Duration
		err     error
	}, 1)
	go func() {
		<-startBarrier
		begin := time.Now()
		err := f.RunWorker(context.Background())
		workerDone <- struct {
			elapsed time.Duration
			err     error
		}{time.Since(begin), err}
	}()
	jobs := make(chan int, count)
	samples := make([]recoveryHTTPSample, count)
	var clients sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		clients.Add(1)
		go func() {
			defer clients.Done()
			<-startBarrier
			for index := range jobs {
				samples[index] = readRecoveryHTTPSample(client, proxy.URL, index)
			}
		}()
	}
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	close(startBarrier)
	clients.Wait()
	requestWall := time.Since(start)
	worker := <-workerDone
	after := f.Pool.Stats()
	report := recoveryHTTPReport{Scope: "real_text_relay_without_auth_distribution_or_input_tokenization", Concurrency: concurrency, PoolSize: 8,
		Requested: count, RequestWallMS: float64(requestWall) / float64(time.Millisecond), ObservationWallMS: float64(time.Since(start)) / float64(time.Millisecond),
		WorkerMS: float64(worker.elapsed) / float64(time.Millisecond), SQLCount: counts.count.Load(), SQLMS: float64(counts.nanos.Load()) / float64(time.Millisecond), SQLErrors: counts.errors.Load(),
		PoolWaitCount: after.WaitCount - before.WaitCount, PoolWaitMS: float64(after.WaitDuration-before.WaitDuration) / float64(time.Millisecond), Samples: samples,
		Notes: "One real recovery job starts with the clients; worker duration shows actual overlap, not continuous background load. Four warm-up requests. Fixed synthetic local upstream, real adaptor/scanner/billing. Schema business fixture is not migration acceptance. No performance PASS is asserted."}
	if worker.err != nil {
		report.WorkerError = worker.err.Error()
	}
	var times []float64
	for _, sample := range samples {
		if sample.Error == "" {
			report.Succeeded++
			times = append(times, sample.FirstContentMS)
		}
	}
	report.Throughput = float64(report.Succeeded) / requestWall.Seconds()
	sort.Float64s(times)
	if len(times) > 0 {
		report.ContentP50MS = times[int(math.Ceil(float64(len(times))*0.5))-1]
		report.ContentP95MS = times[int(math.Ceil(float64(len(times))*0.95))-1]
		if len(times) >= 100 {
			p99 := times[int(math.Ceil(float64(len(times))*0.99))-1]
			report.ContentP99MS, report.P99SufficientSamples = &p99, true
		}
	}
	var unclosed int64
	require.NoError(t, f.DB.Model(&model.MembershipBillingRecord{}).Where("request_id LIKE ? AND state <> ?", "http-acceptance-%", "settled").Count(&unclosed).Error)
	assert.Zero(t, unclosed, "stream delivery alone must not hide failed accounting")
	report.Unsettled = unclosed
	report.Valid = report.Succeeded == count && report.WorkerError == "" && report.SQLErrors == 0 && unclosed == 0
	return report
}

func readRecoveryHTTPSample(client *http.Client, url string, index int) recoveryHTTPSample {
	sample := recoveryHTTPSample{Index: index}
	start := time.Now()
	response, err := client.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(recoveryMeasurementRequest))
	if err != nil {
		sample.Error = "request: " + err.Error()
		return sample
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		sample.Error = fmt.Sprintf("http_status_%d:%s:%s", response.StatusCode, response.Header.Get("X-Acceptance-Stage"), response.Header.Get("X-Acceptance-Code"))
		return sample
	}
	scanner := bufio.NewScanner(response.Body)
	events := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		events++
		if events == 1 {
			sample.FirstSSEMS = float64(time.Since(start)) / float64(time.Millisecond)
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			sample.Error = "invalid_sse_json"
			break
		}
		for _, choice := range event.Choices {
			if choice.Delta.Role != "" && choice.Delta.Content == "" {
				sample.RoleEvents++
			}
			if choice.Delta.Content == "" {
				continue
			}
			if sample.FirstContentEvent == 0 {
				sample.FirstContentEvent, sample.FirstContentMS = events, float64(time.Since(start))/float64(time.Millisecond)
			}
			sample.Content += choice.Delta.Content
		}
	}
	sample.CompleteMS = float64(time.Since(start)) / float64(time.Millisecond)
	if scanner.Err() != nil {
		sample.Error = "stream: " + scanner.Err().Error()
	}
	if sample.Error == "" && (sample.Content != "hello world" || response.Trailer.Get("X-Acceptance-Result") != "complete") {
		sample.Error = "incomplete_relay_or_content:" + response.Trailer.Get("X-Acceptance-Result")
	}
	return sample
}
