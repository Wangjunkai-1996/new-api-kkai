package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskPollingGroupMetricsFixture(t *testing.T) (*model.Task, *model.Channel) {
	t.Helper()
	truncate(t)
	settings, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	oldSettings := *settings
	oldRedis, oldClient, oldMemory := common.RedisEnabled, common.RDB, common.MemoryCacheEnabled
	settings.Enabled = true
	common.RedisEnabled, common.RDB, common.MemoryCacheEnabled = false, nil, false
	t.Cleanup(func() {
		*settings = oldSettings
		common.RedisEnabled, common.RDB, common.MemoryCacheEnabled = oldRedis, oldClient, oldMemory
	})

	const channelID = 901
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_metrics", "upstream_metrics")
	task.Group = "poll-metrics-" + common.GetUUID()[:12]
	task.Properties.OriginModelName = "seedance-metrics-test"
	task.SubmitTime = time.Now().Add(-time.Minute).Unix()
	require.NoError(t, model.DB.Save(task).Error)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	return task, &channel
}

func assertTaskPollingGroupMetrics(t *testing.T, group string, outcomes ...bool) {
	t.Helper()
	signals := perfmetrics.QueryKKAIGroupRecentSignals([]string{group}, 60)
	require.Len(t, signals.Events, len(outcomes))
	var successes int64
	for i, outcome := range outcomes {
		assert.Equal(t, group, signals.Events[i].Group)
		assert.Equal(t, outcome, signals.Events[i].Success)
		if outcome {
			successes++
		}
	}
	now := time.Now()
	buckets := perfmetrics.QueryKKAIGroupMinuteBuckets(now.Add(-time.Minute).Unix(), now.Unix(), []string{group})
	var requests, recordedSuccesses, cacheSamples int64
	for _, bucket := range buckets.Buckets {
		requests += bucket.RequestCount
		recordedSuccesses += bucket.SuccessCount
		cacheSamples += bucket.CacheSampleCount
	}
	assert.EqualValues(t, len(outcomes), requests)
	assert.Equal(t, successes, recordedSuccesses)
	assert.Zero(t, cacheSamples, "async task outcomes must not enter the text cache rate")
}

func TestTaskPollingGroupMetricsVideoTerminal(t *testing.T) {
	for _, status := range []string{model.TaskStatusSuccess, model.TaskStatusFailure} {
		t.Run(status, func(t *testing.T) {
			task, channel := taskPollingGroupMetricsFixture(t)
			adaptor := &taskPollingResponseAdaptor{
				statusCode: http.StatusOK,
				body:       `{}`,
				result:     &relaycommon.TaskInfo{Status: status, Progress: "100%", Reason: "provider result"},
			}
			tasks := map[string]*model.Task{task.GetUpstreamTaskID(): task}
			require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, task.GetUpstreamTaskID(), tasks))
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			assert.EqualValues(t, status, stored.Status)
			assertTaskPollingGroupMetrics(t, task.Group, status == model.TaskStatusSuccess)

			// The original in-progress snapshot loses the status CAS after completion.
			require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, task.GetUpstreamTaskID(), tasks))
			assertTaskPollingGroupMetrics(t, task.Group, status == model.TaskStatusSuccess)

			// A current terminal snapshot may still update result metadata without
			// creating another completed request in group status.
			adaptor.body = `{"metadata":"refreshed"}`
			tasks[task.GetUpstreamTaskID()] = &stored
			require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, task.GetUpstreamTaskID(), tasks))
			assertTaskPollingGroupMetrics(t, task.Group, status == model.TaskStatusSuccess)
		})
	}
}

func TestTaskPollingGroupMetricsIgnoresPendingAndHTTPFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     string
		httpStatus int
	}{
		{name: "queued", status: model.TaskStatusQueued, httpStatus: http.StatusOK},
		{name: "in progress", status: model.TaskStatusInProgress, httpStatus: http.StatusOK},
		{name: "polling HTTP failure", status: model.TaskStatusFailure, httpStatus: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			task, channel := taskPollingGroupMetricsFixture(t)
			adaptor := &taskPollingResponseAdaptor{
				statusCode: test.httpStatus,
				body:       `{}`,
				result:     &relaycommon.TaskInfo{Status: test.status},
			}
			err := updateVideoSingleTask(context.Background(), adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task})
			if test.httpStatus != http.StatusOK {
				require.ErrorContains(t, err, "HTTP 502")
				assert.Zero(t, adaptor.parseCalls)
			} else {
				require.NoError(t, err)
			}
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			assert.Zero(t, stored.FinishTime)
			assert.NotEqualValues(t, model.TaskStatusFailure, stored.Status)
			assertTaskPollingGroupMetrics(t, task.Group)
		})
	}
}

func TestTaskPollingGroupMetricsBatchAndSunoTerminal(t *testing.T) {
	for _, path := range []string{"batch", "suno"} {
		for _, status := range []string{model.TaskStatusSuccess, model.TaskStatusFailure} {
			t.Run(path+"/"+status, func(t *testing.T) {
				task, channel := taskPollingGroupMetricsFixture(t)
				stale := *task
				upstreamID := task.GetUpstreamTaskID()
				adaptor := &membershipSunoPollingAdaptor{result: &BatchTaskResult{
					TaskInfo:   relaycommon.TaskInfo{TaskID: upstreamID, Status: status, Progress: "100%"},
					FinishTime: time.Now().Unix(),
				}}
				oldFactory := GetTaskAdaptorFunc
				GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
				t.Cleanup(func() { GetTaskAdaptorFunc = oldFactory })

				// Poll the initial snapshot, a stale competing snapshot, then the
				// current terminal snapshot. Only the first transition is a sample.
				for _, observed := range []*model.Task{task, &stale, task} {
					tasks := map[string]*model.Task{upstreamID: observed}
					if path == "suno" {
						require.NoError(t, updateSunoTasks(context.Background(), channel.Id, []string{upstreamID}, tasks))
					} else {
						require.NoError(t, UpdateBatchTasks(context.Background(), adaptor, map[int][]string{channel.Id: {upstreamID}}, tasks))
					}
					var stored model.Task
					require.NoError(t, model.DB.First(&stored, task.ID).Error)
					assert.EqualValues(t, status, stored.Status)
					assertTaskPollingGroupMetrics(t, task.Group, status == model.TaskStatusSuccess)
				}
			})
		}
	}
}

func TestTaskPollingGroupMetricsTimeout(t *testing.T) {
	task, _ := taskPollingGroupMetricsFixture(t)
	oldTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = oldTimeout })
	require.NoError(t, model.DB.Model(task).Update("submit_time", time.Now().Add(-2*time.Minute).Unix()).Error)

	sweepTimedOutTasks(context.Background())
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, stored.Status)
	assertTaskPollingGroupMetrics(t, task.Group, false)

	sweepTimedOutTasks(context.Background())
	assertTaskPollingGroupMetrics(t, task.Group, false)
}

func TestTaskPollingGroupMetricsUnpollableFailure(t *testing.T) {
	task, _ := taskPollingGroupMetricsFixture(t)
	won, err := failPollingTask(context.Background(), task, "missing upstream ID")
	require.NoError(t, err)
	require.True(t, won)
	assertTaskPollingGroupMetrics(t, task.Group, false)

	won, err = failPollingTask(context.Background(), task, "missing upstream ID")
	require.NoError(t, err)
	assert.False(t, won)
	assertTaskPollingGroupMetrics(t, task.Group, false)

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, stored.Status)
	_, err = failPollingTask(context.Background(), &stored, "updated failure detail")
	require.NoError(t, err)
	assertTaskPollingGroupMetrics(t, task.Group, false)
}
