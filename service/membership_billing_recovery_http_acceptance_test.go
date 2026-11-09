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
	"net/http/httptrace"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

const recoveryMeasurementStreamRequest = `{"model":"test-model","messages":[{"role":"user","content":"Reply with hello world."}],"max_tokens":32,"stream":true,"stream_options":{"include_usage":true}}`

const recoveryMeasurementJSONRequest = `{"model":"test-model","messages":[{"role":"user","content":"Reply with hello world."}],"max_tokens":32,"stream":false}`

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
	FirstByteMS       float64 `json:"first_byte_ms"`
	FirstContentMS    float64 `json:"first_content_ms"`
	CompleteMS        float64 `json:"complete_ms"`
	FirstContentEvent int     `json:"first_content_event"`
	RoleEvents        int     `json:"role_events"`
	Content           string  `json:"content"`
	Error             string  `json:"error,omitempty"`
}

type recoveryHTTPReport struct {
	CompleteP50MS          float64              `json:"complete_p50_ms"`
	CompleteP95MS          float64              `json:"complete_p95_ms"`
	CompleteP99MS          *float64             `json:"complete_p99_ms,omitempty"`
	SourceRevision         string               `json:"source_revision"`
	Protocol               string               `json:"protocol"`
	UnlimitedToken         bool                 `json:"unlimited_token"`
	PollRuns               int                  `json:"poll_runs"`
	PollMS                 float64              `json:"poll_ms"`
	PollError              string               `json:"poll_error,omitempty"`
	WorkerPolicy           string               `json:"worker_policy"`
	WorkerRuns             int                  `json:"worker_runs"`
	WorkerActiveMS         float64              `json:"worker_active_ms"`
	ProcessCPUMS           float64              `json:"process_cpu_ms"`
	HeapAllocatedBytes     uint64               `json:"heap_allocated_bytes"`
	HeapLiveBytes          uint64               `json:"heap_live_bytes"`
	GCCycles               uint32               `json:"gc_cycles"`
	BackgroundReservations int64                `json:"background_reservations"`
	Scope                  string               `json:"scope"`
	Dialect                string               `json:"dialect"`
	Mode                   string               `json:"mode"`
	Funding                string               `json:"funding"`
	Users                  int                  `json:"users"`
	Conflicts              int                  `json:"conflict_rows"`
	Concurrency            int                  `json:"concurrency"`
	PoolSize               int                  `json:"pool_size"`
	Requested              int                  `json:"requested"`
	Succeeded              int                  `json:"succeeded"`
	Valid                  bool                 `json:"valid_functional_measurement"`
	Unsettled              int64                `json:"unsettled_http_requests"`
	RequestWallMS          float64              `json:"request_wall_ms"`
	ObservationWallMS      float64              `json:"observation_wall_ms"`
	Throughput             float64              `json:"requests_per_second"`
	WorkerMS               float64              `json:"worker_ms"`
	WorkerError            string               `json:"worker_error,omitempty"`
	SQLCount               int64                `json:"sql_count_including_worker"`
	SQLMS                  float64              `json:"sql_sum_ms_including_worker"`
	SQLErrors              int64                `json:"sql_errors_including_worker"`
	PoolWaitCount          int64                `json:"pool_wait_count"`
	PoolWaitMS             float64              `json:"pool_wait_ms"`
	ContentP50MS           float64              `json:"content_p50_ms"`
	ContentP95MS           float64              `json:"content_p95_ms"`
	ContentP99MS           *float64             `json:"content_p99_ms,omitempty"`
	P99SufficientSamples   bool                 `json:"p99_minimum_100_samples"`
	Notes                  string               `json:"notes"`
	Samples                []recoveryHTTPSample `json:"samples"`
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
	oldTimeout, oldGinMode := constant.StreamingTimeout, gin.Mode()
	constant.StreamingTimeout = 30
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout; gin.SetMode(oldGinMode) })
	for _, dialect := range []string{"postgres", "mysql"} {
		for _, members := range []bool{true, false} {
			funding := "wallet"
			if members {
				funding = "membership"
			}
			for _, users := range []int{1, 4} {
				for _, conflicts := range []int{0, 501} {
					for _, mode := range []string{"off", "apply", "paused"} {
						for _, stream := range []bool{true, false} {
							protocol := "json"
							if stream {
								protocol = "sse"
							}
							for _, unlimited := range []bool{false, true} {
								tokenKind := "finite"
								if unlimited {
									tokenKind = "unlimited"
								}
								name := fmt.Sprintf("%s/%s/users-%d/conflicts-%d/%s/%s/%s", dialect, funding, users, conflicts, mode, protocol, tokenKind)
								t.Run(name, func(t *testing.T) {
									f := service.NewMembershipRecoveryAcceptanceBridge(t, dialect, members, users, conflicts, unlimited)
									f.SetMode(mode)
									report := measureMembershipRecoveryHTTP(t, f, count, concurrency, stream)
									report.SourceRevision = os.Getenv("KKAI_MEMBERSHIP_RECOVERY_SOURCE")
									report.Dialect, report.Mode, report.Funding, report.Users, report.Conflicts = dialect, mode, funding, users, conflicts
									report.Protocol, report.UnlimitedToken = protocol, unlimited
									report.Valid = report.Valid && report.BackgroundReservations == int64(conflicts)
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
									assert.Equal(t, count, report.Succeeded, "functional errors invalidate measurement")
									assert.Empty(t, report.WorkerError)
									assert.Empty(t, report.PollError)
									assert.Zero(t, report.SQLErrors, "SQL failures invalidate measurement")
									assert.EqualValues(t, conflicts, report.BackgroundReservations, "known conflicts must retain their funds")
									assert.True(t, report.Valid)
								})
							}
						}
					}
				}
			}
		}
	}
}

func measureMembershipRecoveryHTTP(t *testing.T, f service.MembershipRecoveryAcceptanceBridge, count, concurrency int, stream bool) recoveryHTTPReport {
	t.Helper()
	requestBody := recoveryMeasurementJSONRequest
	if stream {
		requestBody = recoveryMeasurementStreamRequest
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		_, _ = io.Copy(io.Discard, r.Body)
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"local-completion","created":1,"model":"test-model","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
			return
		}
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
	var handlers sync.WaitGroup
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		index := int(requestIndex.Add(1) - 1)
		user, token := f.Users[index%len(f.Users)], f.Tokens[index%len(f.Tokens)]
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil || string(body) != requestBody {
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
			UsingGroup: "gpt", IsStream: stream, DisablePing: true, MembershipBillingEnabled: &enabled}
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
		sample := readRecoveryHTTPSample(client, proxy.URL, i, requestBody, stream)
		require.Empty(t, sample.Error, "warm-up must traverse the real relay")
		require.Equal(t, "hello world", sample.Content)
		if stream {
			require.Positive(t, sample.RoleEvents)
			require.Greater(t, sample.FirstContentEvent, 1, "role-only SSE must not count as first content")
		} else {
			require.Zero(t, sample.FirstSSEMS)
			require.Zero(t, sample.FirstContentEvent)
		}
	}
	handlers.Wait()
	counts := &recoveryMeasurementSQL{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	f.DB.Logger = counts
	before := f.Pool.Stats()
	var memoryBefore, memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	var cpuBefore, cpuAfter syscall.Rusage
	require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &cpuBefore))
	start := time.Now()
	startBarrier := make(chan struct{})
	workerPolicy := os.Getenv("KKAI_MEMBERSHIP_RECOVERY_WORKER")
	if workerPolicy == "" {
		workerPolicy = "continuous"
	}
	require.Contains(t, []string{"continuous", "none"}, workerPolicy)
	workerStop := make(chan struct{})
	workerDone := make(chan struct {
		elapsed, active time.Duration
		runs            int
		err             error
	}, 1)
	go func() {
		<-startBarrier
		begin := time.Now()
		runs := 0
		var active time.Duration
		var workerErr error
		for workerPolicy != "none" {
			phaseStart := time.Now()
			runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := f.RunWorker(runCtx)
			cancel()
			active += time.Since(phaseStart)
			runs++
			if err != nil {
				workerErr = err
				break
			}
			// Deliberately accelerated continuous pressure: at most ten job starts
			// per second, no overlapping workers. Production schedules once a minute.
			timer := time.NewTimer(max(0, 100*time.Millisecond-time.Since(phaseStart)))
			select {
			case <-workerStop:
				timer.Stop()
				workerDone <- struct {
					elapsed, active time.Duration
					runs            int
					err             error
				}{time.Since(begin), active, runs, workerErr}
				return
			case <-timer.C:
			}
		}
		workerDone <- struct {
			elapsed, active time.Duration
			runs            int
			err             error
		}{time.Since(begin), active, runs, workerErr}
	}()
	pollDone := make(chan struct {
		runs    int
		elapsed time.Duration
		err     error
	}, 1)
	go func() {
		<-startBarrier
		var elapsed time.Duration
		runs := 0
		var pollErr error
		if f.RunPoll != nil && workerPolicy != "none" {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				begin := time.Now()
				pollErr = f.RunPoll(context.Background())
				elapsed += time.Since(begin)
				runs++
				if pollErr != nil {
					break
				}
				select {
				case <-workerStop:
					pollDone <- struct {
						runs    int
						elapsed time.Duration
						err     error
					}{runs, elapsed, pollErr}
					return
				case <-ticker.C:
				}
			}
		}
		pollDone <- struct {
			runs    int
			elapsed time.Duration
			err     error
		}{runs, elapsed, pollErr}
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
				samples[index] = readRecoveryHTTPSample(client, proxy.URL, index, requestBody, stream)
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
	handlers.Wait()
	close(workerStop)
	worker := <-workerDone
	poll := <-pollDone
	runtime.ReadMemStats(&memoryAfter)
	require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &cpuAfter))
	after := f.Pool.Stats()
	report := recoveryHTTPReport{Scope: "real_text_relay_without_auth_distribution_or_input_tokenization", Concurrency: concurrency, PoolSize: 8,
		Requested: count, RequestWallMS: float64(requestWall) / float64(time.Millisecond), ObservationWallMS: float64(time.Since(start)) / float64(time.Millisecond),
		PollRuns: poll.runs, PollMS: float64(poll.elapsed) / float64(time.Millisecond), WorkerPolicy: workerPolicy, WorkerRuns: worker.runs, WorkerActiveMS: float64(worker.active) / float64(time.Millisecond),
		ProcessCPUMS:       float64(cpuAfter.Utime.Nano()+cpuAfter.Stime.Nano()-cpuBefore.Utime.Nano()-cpuBefore.Stime.Nano()) / float64(time.Millisecond),
		HeapAllocatedBytes: memoryAfter.TotalAlloc - memoryBefore.TotalAlloc, HeapLiveBytes: memoryAfter.HeapAlloc, GCCycles: memoryAfter.NumGC - memoryBefore.NumGC,
		WorkerMS: float64(worker.elapsed) / float64(time.Millisecond), SQLCount: counts.count.Load(), SQLMS: float64(counts.nanos.Load()) / float64(time.Millisecond), SQLErrors: counts.errors.Load(),
		PoolWaitCount: after.WaitCount - before.WaitCount, PoolWaitMS: float64(after.WaitDuration-before.WaitDuration) / float64(time.Millisecond), Samples: samples,
		Notes: "Real worker starts repeatedly at an accelerated 100ms cadence until HTTP completes; no overlapping jobs. worker_active_ms excludes cadence waits. Production worker cadence is 60 seconds; when supported the real control poll runs independently every1second. CPU and heap include foreground and worker. Four warm-up requests. Fixed synthetic local upstream, real adaptor/scanner/billing. Schema business fixture is not migration acceptance. JSON first content is measured after decoding its actual body; all server handlers finish before ledger verification. No performance PASS is asserted."}
	if poll.err != nil {
		report.PollError = poll.err.Error()
	}
	if worker.err != nil {
		report.WorkerError = worker.err.Error()
	}
	var times, completedTimes []float64
	for _, sample := range samples {
		if sample.Error == "" {
			report.Succeeded++
			times = append(times, sample.FirstContentMS)
			completedTimes = append(completedTimes, sample.CompleteMS)
		}
	}
	report.Throughput = float64(report.Succeeded) / requestWall.Seconds()
	sort.Float64s(times)
	sort.Float64s(completedTimes)
	if len(times) > 0 {
		report.ContentP50MS = times[int(math.Ceil(float64(len(times))*0.5))-1]
		report.ContentP95MS = times[int(math.Ceil(float64(len(times))*0.95))-1]
		report.CompleteP50MS = completedTimes[int(math.Ceil(float64(len(times))*0.5))-1]
		report.CompleteP95MS = completedTimes[int(math.Ceil(float64(len(times))*0.95))-1]
		if len(times) >= 100 {
			p99 := times[int(math.Ceil(float64(len(times))*0.99))-1]
			report.ContentP99MS, report.P99SufficientSamples = &p99, true
			completeP99 := completedTimes[int(math.Ceil(float64(len(times))*0.99))-1]
			report.CompleteP99MS = &completeP99
		}
	}
	var unclosed int64
	require.NoError(t, f.DB.Model(&model.MembershipBillingRecord{}).Where("request_id LIKE ? AND state <> ?", "http-acceptance-%", "settled").Count(&unclosed).Error)
	assert.Zero(t, unclosed, "stream delivery alone must not hide failed accounting")
	require.NoError(t, f.DB.Model(&model.MembershipBillingRecord{}).Where("request_id LIKE ? AND state = ?", "conflict-%", "reserved").Count(&report.BackgroundReservations).Error)
	report.Unsettled = unclosed
	report.Valid = report.Succeeded == count && report.WorkerError == "" && report.PollError == "" && report.SQLErrors == 0 && unclosed == 0
	if f.RunPoll != nil && workerPolicy != "none" {
		report.Valid = report.Valid && report.PollRuns > 0
	}
	return report
}

func readRecoveryHTTPSample(client *http.Client, url string, index int, requestBody string, stream bool) recoveryHTTPSample {
	sample := recoveryHTTPSample{Index: index}
	start := time.Now()
	request, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(requestBody))
	if err != nil {
		sample.Error = "build_request"
		return sample
	}
	request.Header.Set("Content-Type", "application/json")
	var firstByte atomic.Int64
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte.CompareAndSwap(0, time.Since(start).Nanoseconds()) }}))
	response, err := client.Do(request)
	sample.FirstByteMS = float64(firstByte.Load()) / float64(time.Millisecond)
	if err != nil {
		sample.Error = "request: " + err.Error()
		return sample
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		sample.Error = fmt.Sprintf("http_status_%d:%s:%s", response.StatusCode, response.Header.Get("X-Acceptance-Stage"), response.Header.Get("X-Acceptance-Code"))
		return sample
	}
	if !stream {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			sample.Error = "json_body: " + err.Error()
			return sample
		}
		var completion struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := common.Unmarshal(body, &completion); err != nil {
			sample.Error = "invalid_completion_json"
			return sample
		}
		for _, choice := range completion.Choices {
			sample.Content += choice.Message.Content
		}
		sample.CompleteMS = float64(time.Since(start)) / float64(time.Millisecond)
		// Nonstreaming content becomes usable only after the JSON body is decoded;
		// header arrival and SSE event timestamps do not apply to this protocol.
		if sample.Content != "" {
			sample.FirstContentMS = sample.CompleteMS
		}
		if sample.Content != "hello world" {
			sample.Error = "incomplete_json_content"
		}
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
