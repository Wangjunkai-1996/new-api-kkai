package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubmitPreparedTaskRecordsImmediateTerminalMetricsOnce(t *testing.T) {
	metricsSetting, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	originalEnabled, originalRedisEnabled := metricsSetting.Enabled, common.RedisEnabled
	metricsSetting.Enabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		metricsSetting.Enabled, common.RedisEnabled = originalEnabled, originalRedisEnabled
	})

	for _, tc := range []struct {
		name             string
		status           model.TaskStatus
		failFirstPersist bool
		failAllPersists  bool
	}{
		{name: "success", status: model.TaskStatusSuccess},
		{name: "failure", status: model.TaskStatusFailure},
		{name: "pending", status: model.TaskStatusSubmitted},
		{name: "success after persistence recovery", status: model.TaskStatusSuccess, failFirstPersist: true},
		{name: "success survives exhausted persistence retries", status: model.TaskStatusSuccess, failAllPersists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTaskReliabilityDB(t)
			_, ctx, info := newTaskReliabilityContext(t)
			require.NoError(t, db.AutoMigrate(&model.User{}))
			require.NoError(t, db.Create(&model.User{Id: info.UserId, Username: "metrics-user"}).Error)
			info.UsingGroup = t.Name()
			if tc.failAllPersists {
				info.PriceData.Quota = 0
			}
			failFirstPersist := tc.failFirstPersist
			if failFirstPersist || tc.failAllPersists {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_immediate_acceptance", func(tx *gorm.DB) {
					task, ok := tx.Statement.Dest.(*model.Task)
					if ok && task.Status == model.TaskStatusSuccess && (failFirstPersist || tc.failAllPersists) {
						failFirstPersist = false
						tx.AddError(errors.New("transient acceptance persistence failure"))
					}
				}))
			}
			upstreamCalls := 0
			adaptor := &reliabilityTaskAdaptor{
				doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
					upstreamCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"immediate-task"}`))}, nil
				},
				doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *dto.TaskError) {
					return &channel.TaskSubmitResponse{
						UpstreamTaskID: "immediate-task",
						Immediate:      &relaycommon.TaskInfo{Status: string(tc.status), Url: "https://example.invalid/result", TotalTokens: 12},
					}, nil
				},
			}
			started := time.Now()
			result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("metrics-test"), nil)
			require.NotNil(t, result)
			if tc.failAllPersists {
				require.NotNil(t, taskErr)
				assert.Equal(t, "task_submission_unknown", taskErr.Code)
				assert.Equal(t, TaskSubmitUnknown, result.Outcome)
			} else {
				require.Nil(t, taskErr)
				assert.Equal(t, TaskSubmitAccepted, result.Outcome)
				assert.Equal(t, tc.status, result.Task.Status)
			}
			assert.False(t, failFirstPersist)

			signals := perfmetrics.QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events
			if tc.status == model.TaskStatusSubmitted {
				assert.Empty(t, signals, "an accepted asynchronous task is not a completed request")
			} else {
				require.Len(t, signals, 1)
				assert.Equal(t, tc.status == model.TaskStatusSuccess, signals[0].Success)
				buckets := perfmetrics.QueryKKAIGroupMinuteBuckets(started.Add(-time.Minute).Unix(), time.Now().Unix(), []string{info.UsingGroup}).Buckets
				require.Len(t, buckets, 1)
				assert.EqualValues(t, 1, buckets[0].RequestCount)
				var successes int64
				if tc.status == model.TaskStatusSuccess {
					successes = 1
				}
				assert.Equal(t, successes, buckets[0].SuccessCount)
			}

			if tc.failAllPersists {
				require.NoError(t, db.Callback().Update().Remove("test:fail_immediate_acceptance"))
				var recoveryEvent model.KKAIOutboxEvent
				require.NoError(t, db.Where("topic = ?", service.KKAIOutboxTopicTaskBillingRecovery).First(&recoveryEvent).Error)
				var receipt service.TaskBillingRecoveryPayload
				require.NoError(t, common.UnmarshalJsonStr(recoveryEvent.Payload, &receipt))
				require.NotNil(t, receipt.Acceptance)
				assert.Equal(t, tc.status, receipt.Acceptance.Status)
				require.NoError(t, (service.TaskBillingRecoveryHandler{}).Handle(context.Background(), recoveryEvent))
				var recovered model.Task
				require.NoError(t, db.First(&recovered, result.Task.ID).Error)
				assert.Equal(t, tc.status, recovered.Status)
				assert.Equal(t, signals, perfmetrics.QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events, "recovering the receipt must not count the result twice")
				require.ErrorContains(t, (service.TaskBillingRecoveryHandler{}).Handle(context.Background(), recoveryEvent), "conflicts with current task state")
				assert.Equal(t, signals, perfmetrics.QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events, "redelivering a completed receipt must not add another sample")
			}

			_, repeatErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("metrics-test"), result.Task)
			require.NotNil(t, repeatErr)
			assert.Equal(t, 1, upstreamCalls, "an accepted task must not be dispatched again")
			assert.Equal(t, signals, perfmetrics.QueryKKAIGroupRecentSignals([]string{info.UsingGroup}, 60).Events, "retrying an accepted task must not add another sample")
		})
	}
}

func TestSubmitPreparedTaskMetricsFollowAcceptedRetryGroup(t *testing.T) {
	metricsSetting, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	originalEnabled, originalRedisEnabled := metricsSetting.Enabled, common.RedisEnabled
	metricsSetting.Enabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		metricsSetting.Enabled, common.RedisEnabled = originalEnabled, originalRedisEnabled
	})
	db := setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{Id: info.UserId, Username: "metrics-user"}).Error)
	rejectedGroup, acceptedGroup := t.Name()+"-rejected", t.Name()+"-accepted"
	info.UsingGroup = rejectedGroup
	upstreamCalls := 0
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalls++
			if upstreamCalls == 1 {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{"error":"busy"}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"retried-task"}`))}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *dto.TaskError) {
			return &channel.TaskSubmitResponse{
				UpstreamTaskID: "retried-task",
				Immediate:      &relaycommon.TaskInfo{Status: model.TaskStatusSuccess},
			}, nil
		},
	}
	rejected, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("metrics-test"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, rejected)
	require.True(t, rejected.CanRetry())
	info.UsingGroup = acceptedGroup
	accepted, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("metrics-test"), rejected.Task)
	require.Nil(t, taskErr)
	require.NotNil(t, accepted)
	assert.Equal(t, acceptedGroup, accepted.Task.Group)
	var persisted model.Task
	require.NoError(t, db.First(&persisted, accepted.Task.ID).Error)
	assert.Equal(t, acceptedGroup, persisted.Group, "polling and accounting must retain the actual accepted group")
	signals := perfmetrics.QueryKKAIGroupRecentSignals([]string{rejectedGroup, acceptedGroup}, 60).Events
	require.Len(t, signals, 1)
	assert.Equal(t, acceptedGroup, signals[0].Group)
	assert.True(t, signals[0].Success)
	assert.Equal(t, 2, upstreamCalls)
}
