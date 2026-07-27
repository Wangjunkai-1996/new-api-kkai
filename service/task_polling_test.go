package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskPollingFetchAdaptor struct {
	mu           sync.Mutex
	taskIDs      []string
	fetched      chan string
	blockTaskID  string
	blockStarted chan struct{}
	releaseBlock chan struct{}
	blockOnce    sync.Once
}

type completedArchiveSourceAdaptor struct {
	taskPollingFetchAdaptor
	result *relaycommon.TaskInfo
	body   string
}

func (a *completedArchiveSourceAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return a.result, nil
}

func (a *completedArchiveSourceAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	body := a.body
	if body == "" {
		body = `{"response":{"videos":[{"bytesBase64Encoded":"dmlkZW8=","mimeType":"video/mp4"}]}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}, nil
}

func (a *taskPollingFetchAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *taskPollingFetchAdaptor) FetchTask(_ string, _ string, body map[string]any, _ string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if taskID == a.blockTaskID && a.releaseBlock != nil {
		a.blockOnce.Do(func() {
			if a.blockStarted != nil {
				close(a.blockStarted)
			}
		})
		<-a.releaseBlock
	}

	a.mu.Lock()
	a.taskIDs = append(a.taskIDs, taskID)
	a.mu.Unlock()
	if a.fetched != nil {
		select {
		case a.fetched <- taskID:
		default:
		}
	}

	response := dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			TaskID:   taskID,
			Status:   model.TaskStatusInProgress,
			Progress: "30%",
		},
	}
	responseBody, err := common.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}, nil
}

func (a *taskPollingFetchAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
}

func (a *taskPollingFetchAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *taskPollingFetchAdaptor) fetchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.taskIDs)
}

func (a *taskPollingFetchAdaptor) fetchedTaskIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.taskIDs...)
}

func seedTaskPollingChannel(t *testing.T, id int, disableSleep bool) {
	t.Helper()
	ch := &model.Channel{
		Id:     id,
		Type:   constant.ChannelTypeKling,
		Name:   "polling_channel",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
	}
	if disableSleep {
		ch.SetOtherSettings(dto.ChannelOtherSettings{DisableTaskPollingSleep: true})
	}
	require.NoError(t, model.DB.Create(ch).Error)
}

func seedPollingTask(t *testing.T, channelID int, publicID string, upstreamID string) *model.Task {
	t.Helper()
	task := &model.Task{
		TaskID:    publicID,
		Platform:  constant.TaskPlatform("kling"),
		UserId:    1,
		ChannelId: channelID,
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: upstreamID,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func TestUpdateVideoTasksDefaultSleepWaitsBetweenTasks(t *testing.T) {
	truncate(t)

	const channelID = 101
	seedTaskPollingChannel(t, channelID, false)
	first := seedPollingTask(t, channelID, "task_public_1", "upstream_1")
	second := seedPollingTask(t, channelID, "task_public_2", "upstream_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, adaptor.fetchCount())
}

func TestUpdateVideoTasksCanSkipPollingSleepPerChannel(t *testing.T) {
	truncate(t)

	const channelID = 102
	seedTaskPollingChannel(t, channelID, true)
	first := seedPollingTask(t, channelID, "task_public_3", "upstream_3")
	second := seedPollingTask(t, channelID, "task_public_4", "upstream_4")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, adaptor.fetchCount())
}

func TestUpdateVideoTasksDoesNotMutateCallerOwnedTaskSnapshots(t *testing.T) {
	truncate(t)

	const channelID = 103
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_public_snapshot", "upstream_snapshot")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	upstreamID := task.GetUpstreamTaskID()
	require.NoError(t, UpdateVideoTasks(context.Background(), constant.TaskPlatform("kling"), map[int][]string{
		channelID: {upstreamID},
	}, map[string]*model.Task{
		upstreamID: task,
	}))

	assert.Nil(t, task.Data)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
	assert.Equal(t, "30%", task.Progress)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.NotEmpty(t, persisted.Data)
}

func TestUpdateVideoSingleTaskPersistsManagedArchiveSourceBeforeRedactingRawResponse(t *testing.T) {
	truncate(t)

	const channelID = 104
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_public_data_archive", "upstream_data_archive")
	task.PrivateData.AssetHostedResult = true
	require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	archiveSource := "data:video/mp4;base64,dmlkZW8="
	adaptor := &completedArchiveSourceAdaptor{result: &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, Progress: "100%", Url: archiveSource,
	}}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)

	err := updateVideoSingleTask(context.Background(), adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.NoError(t, err)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), persisted.Status)
	assert.Equal(t, archiveSource, persisted.PrivateData.ArchiveSource)
	assert.Contains(t, persisted.PrivateData.ResultURL, "/v1/videos/"+task.TaskID+"/content")
	assert.NotContains(t, string(persisted.Data), "dmlkZW8=")
}

func TestUpdateVideoSingleTaskPreservesManagedArchiveSourceWhenProviderOmitsURL(t *testing.T) {
	truncate(t)

	const channelID = 105
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_public_existing_archive", "upstream_existing_archive")
	existingSource := "https://provider.example/existing.mp4"
	task.PrivateData.AssetHostedResult = true
	task.PrivateData.ArchiveSource = existingSource
	require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	adaptor := &completedArchiveSourceAdaptor{result: &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, Progress: "100%",
	}}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)

	err := updateVideoSingleTask(context.Background(), adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.NoError(t, err)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), persisted.Status)
	assert.Equal(t, existingSource, persisted.PrivateData.ArchiveSource)
}

func TestUpdateVideoSingleTaskManagedPollingLogsOnlyBoundedMetadata(t *testing.T) {
	truncate(t)

	const channelID = 106
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_public_managed_logs", "upstream_managed_logs")
	task.PrivateData.AssetHostedResult = true
	require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)

	base64Secret := "base64-provider-secret-payload"
	temporaryURL := "https://provider.example/temporary.mp4?token=temporary-secret"
	responseBody := fmt.Sprintf(`{"response":{"videos":[{"bytesBase64Encoded":%q,"uri":%q,"mimeType":"video/mp4"}]}}`, base64Secret, temporaryURL)
	adaptor := &completedArchiveSourceAdaptor{
		result: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%", Url: temporaryURL},
		body:   responseBody,
	}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)

	previousDebug := common.DebugEnabled
	common.DebugEnabled = true
	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultWriter
	previousErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = &logs
	gin.DefaultErrorWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.DebugEnabled = previousDebug
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousErrorWriter
		common.LogWriterMu.Unlock()
	})

	err := updateVideoSingleTask(context.Background(), adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.NoError(t, err)

	assert.Contains(t, logs.String(), "managed response")
	assert.Contains(t, logs.String(), fmt.Sprintf("response_bytes=%d", len(responseBody)))
	assert.NotContains(t, logs.String(), base64Secret)
	assert.NotContains(t, logs.String(), temporaryURL)
	assert.NotContains(t, logs.String(), "temporary-secret")
}

func TestUpdateVideoSingleTaskManagedFailureKeepsProviderReasonOutOfUserLogs(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 108, 108, 108
	const chargedQuota = 1_000
	const rawProviderReason = "provider rejected https://provider.example/temp.mp4?token=query-secret api_key=echoed-provider-key"
	const publicFailureReason = "video generation failed"

	seedUser(t, userID, 9_000)
	seedToken(t, tokenID, userID, "sk-managed-failure", 7_000)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", chargedQuota).Error)
	seedTaskPollingChannel(t, channelID, true)

	task := makeTask(userID, channelID, chargedQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_public_managed_failure"
	task.Status = model.TaskStatusInProgress
	task.Progress = "60%"
	task.PrivateData.AssetHostedResult = true
	task.PrivateData.UpstreamTaskID = "upstream_managed_failure"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.TokenQuota = chargedQuota
	task.PrivateData.TokenBilling = true
	task.PrivateData.BillingRevision = 1
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &completedArchiveSourceAdaptor{
		result: &relaycommon.TaskInfo{
			Status: model.TaskStatusFailure,
			Reason: rawProviderReason,
		},
		body: `{"status":"failed"}`,
	}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	assert.Equal(t, publicFailureReason, persisted.FailReason)
	assert.Equal(t, publicFailureReason, persisted.PublicFailReason())
	assert.NotContains(t, persisted.FailReason, "provider.example")
	assert.NotContains(t, persisted.FailReason, "query-secret")
	assert.NotContains(t, persisted.FailReason, "echoed-provider-key")
	assert.Equal(t, model.TaskBillingStateRefunded, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
	assert.EqualValues(t, 10_000, getUserQuota(t, userID))
	assert.Equal(t, 8_000, getTokenRemainQuota(t, tokenID))

	var event model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).First(&event).Error)
	handler := TaskBillingAuditHandler{}
	require.NoError(t, handler.Handle(context.Background(), event))
	require.NoError(t, handler.Handle(context.Background(), event))

	userLogs, total, err := model.GetUserLogs(userID, model.LogTypeRefund, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	assert.NotContains(t, userLogs[0].Other, "provider.example")
	assert.NotContains(t, userLogs[0].Other, "query-secret")
	assert.NotContains(t, userLogs[0].Other, "echoed-provider-key")
	var userOther map[string]any
	require.NoError(t, common.UnmarshalJsonStr(userLogs[0].Other, &userOther))
	assert.Equal(t, publicFailureReason, userOther["reason"])
	assert.NotContains(t, userOther, "admin_info")

	adminLogs, adminTotal, err := model.GetAllLogs(model.LogTypeRefund, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, adminTotal)
	require.Len(t, adminLogs, 1)
	var adminOther map[string]any
	require.NoError(t, common.UnmarshalJsonStr(adminLogs[0].Other, &adminOther))
	assert.Equal(t, publicFailureReason, adminOther["reason"])
	adminInfo, ok := adminOther["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, rawProviderReason, adminInfo["provider_failure_reason"])
}

func TestUpdateVideoSingleTaskUnmanagedPollingRetainsDebugResponse(t *testing.T) {
	truncate(t)

	const channelID = 107
	seedTaskPollingChannel(t, channelID, true)
	task := seedPollingTask(t, channelID, "task_public_unmanaged_logs", "upstream_unmanaged_logs")
	debugMarker := "unmanaged-debug-response-marker"
	responseBody := fmt.Sprintf(`{"marker":%q}`, debugMarker)
	adaptor := &completedArchiveSourceAdaptor{
		result: &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "50%"},
		body:   responseBody,
	}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)

	previousDebug := common.DebugEnabled
	common.DebugEnabled = true
	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousErrorWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.DebugEnabled = previousDebug
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousErrorWriter
		common.LogWriterMu.Unlock()
	})

	err := updateVideoSingleTask(context.Background(), adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), debugMarker)
}

func TestUpdateVideoTasksDefaultSleepDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const firstChannelID = 201
	const secondChannelID = 202
	seedTaskPollingChannel(t, firstChannelID, false)
	seedTaskPollingChannel(t, secondChannelID, false)
	firstChannelFirst := seedPollingTask(t, firstChannelID, "task_public_5", "upstream_a_1")
	firstChannelSecond := seedPollingTask(t, firstChannelID, "task_public_6", "upstream_a_2")
	secondChannelFirst := seedPollingTask(t, secondChannelID, "task_public_7", "upstream_b_1")
	secondChannelSecond := seedPollingTask(t, secondChannelID, "task_public_8", "upstream_b_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		firstChannelID: {
			firstChannelFirst.GetUpstreamTaskID(),
			firstChannelSecond.GetUpstreamTaskID(),
		},
		secondChannelID: {
			secondChannelFirst.GetUpstreamTaskID(),
			secondChannelSecond.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		firstChannelFirst.GetUpstreamTaskID():   firstChannelFirst,
		firstChannelSecond.GetUpstreamTaskID():  firstChannelSecond,
		secondChannelFirst.GetUpstreamTaskID():  secondChannelFirst,
		secondChannelSecond.GetUpstreamTaskID(): secondChannelSecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_a_1", "upstream_b_1"}, adaptor.fetchedTaskIDs())
}

func TestUpdateVideoTasksSlowChannelDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const slowChannelID = 251
	const fastChannelID = 252
	seedTaskPollingChannel(t, slowChannelID, false)
	seedTaskPollingChannel(t, fastChannelID, true)
	slowTask := seedPollingTask(t, slowChannelID, "task_public_slow", "upstream_slow_1")
	fastFirst := seedPollingTask(t, fastChannelID, "task_public_fast_1", "upstream_fast_parallel_1")
	fastSecond := seedPollingTask(t, fastChannelID, "task_public_fast_2", "upstream_fast_parallel_2")

	adaptor := &taskPollingFetchAdaptor{
		fetched:      make(chan string, 4),
		blockTaskID:  slowTask.GetUpstreamTaskID(),
		blockStarted: make(chan struct{}),
		releaseBlock: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseBlockedTask := func() {
		releaseOnce.Do(func() {
			close(adaptor.releaseBlock)
		})
	}
	t.Cleanup(releaseBlockedTask)
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	errCh := make(chan error, 1)
	gopool.Go(func() {
		errCh <- UpdateVideoTasks(context.Background(), constant.TaskPlatform("kling"), map[int][]string{
			slowChannelID: {
				slowTask.GetUpstreamTaskID(),
			},
			fastChannelID: {
				fastFirst.GetUpstreamTaskID(),
				fastSecond.GetUpstreamTaskID(),
			},
		}, map[string]*model.Task{
			slowTask.GetUpstreamTaskID():   slowTask,
			fastFirst.GetUpstreamTaskID():  fastFirst,
			fastSecond.GetUpstreamTaskID(): fastSecond,
		})
	})

	select {
	case <-adaptor.blockStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow channel did not start blocking")
	}

	require.Eventually(t, func() bool {
		fetchedTaskIDs := adaptor.fetchedTaskIDs()
		return len(fetchedTaskIDs) == 2 &&
			fetchedTaskIDs[0] == fastFirst.GetUpstreamTaskID() &&
			fetchedTaskIDs[1] == fastSecond.GetUpstreamTaskID()
	}, 500*time.Millisecond, 10*time.Millisecond)

	releaseBlockedTask()
	require.NoError(t, <-errCh)
	assert.ElementsMatch(t, []string{
		slowTask.GetUpstreamTaskID(),
		fastFirst.GetUpstreamTaskID(),
		fastSecond.GetUpstreamTaskID(),
	}, adaptor.fetchedTaskIDs())
}

func TestUpdateVideoTasksMixedChannelSleepSettings(t *testing.T) {
	truncate(t)

	const sleepyChannelID = 301
	const fastChannelID = 302
	seedTaskPollingChannel(t, sleepyChannelID, false)
	seedTaskPollingChannel(t, fastChannelID, true)
	sleepyFirst := seedPollingTask(t, sleepyChannelID, "task_public_9", "upstream_sleepy_1")
	sleepySecond := seedPollingTask(t, sleepyChannelID, "task_public_10", "upstream_sleepy_2")
	fastFirst := seedPollingTask(t, fastChannelID, "task_public_11", "upstream_fast_1")
	fastSecond := seedPollingTask(t, fastChannelID, "task_public_12", "upstream_fast_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		sleepyChannelID: {
			sleepyFirst.GetUpstreamTaskID(),
			sleepySecond.GetUpstreamTaskID(),
		},
		fastChannelID: {
			fastFirst.GetUpstreamTaskID(),
			fastSecond.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		sleepyFirst.GetUpstreamTaskID():  sleepyFirst,
		sleepySecond.GetUpstreamTaskID(): sleepySecond,
		fastFirst.GetUpstreamTaskID():    fastFirst,
		fastSecond.GetUpstreamTaskID():   fastSecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_sleepy_1", "upstream_fast_1", "upstream_fast_2"}, adaptor.fetchedTaskIDs())
}

func TestSweepTimedOutTasksLeavesAcceptedUnknownPollable(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 401, 401, 401
	const chargedQuota = 700
	seedUser(t, userID, 5_000)
	seedToken(t, tokenID, userID, "sk-accepted-unknown", 3_000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, chargedQuota, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusUnknown
	task.Progress = "10%"
	task.SubmitTime = time.Now().Add(-2 * time.Hour).Unix()
	task.PrivateData.UpstreamTaskID = "upstream-accepted-unknown"
	require.NoError(t, model.DB.Create(task).Error)

	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })

	sweepTimedOutTasks(context.Background())

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, "10%", persisted.Progress)
	assert.Equal(t, "upstream-accepted-unknown", persisted.GetUpstreamTaskID())
	assert.EqualValues(t, 5_000, getUserQuota(t, userID))
	assert.Equal(t, 3_000, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, countLogs(t))

	unfinished := model.GetAllUnFinishSyncTasks(10)
	require.Len(t, unfinished, 1)
	assert.Equal(t, task.ID, unfinished[0].ID)
}

func TestFailPollingTaskDoesNotOverwriteConcurrentSuccessOrRefund(t *testing.T) {
	truncate(t)

	const userID, channelID = 402, 402
	const chargedQuota = 700
	seedUser(t, userID, 4_300)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, chargedQuota, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusInProgress
	task.Progress = "30%"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.UpstreamTaskID = "upstream-concurrent-success"
	require.NoError(t, model.DB.Create(task).Error)

	stalePoller := *task
	successfulPoller := *task
	successSnapshot := successfulPoller.Snapshot()
	successfulPoller.Status = model.TaskStatusSuccess
	successfulPoller.Progress = "100%"
	successfulPoller.FinishTime = time.Now().Unix()
	won, err := successfulPoller.UpdateWithStatusPreservingBilling(successSnapshot)
	require.NoError(t, err)
	require.True(t, won)

	won, err = failPollingTask(context.Background(), &stalePoller, "stale channel failure")
	require.NoError(t, err)
	assert.False(t, won)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, model.TaskBillingStateAccepted, persisted.PrivateData.BillingState)
	assert.EqualValues(t, 4_300, getUserQuota(t, userID))

	var auditCount int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
		Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).
		Count(&auditCount).Error)
	assert.Zero(t, auditCount)
}

func TestMissingChannelPollingFailureKeepsTaskRecoverable(t *testing.T) {
	tests := []struct {
		name     string
		platform constant.TaskPlatform
		fail     func(context.Context, int, []string, map[string]*model.Task) error
	}{
		{
			name:     "suno",
			platform: constant.TaskPlatformSuno,
			fail:     updateSunoTasks,
		},
		{
			name:     "video",
			platform: constant.TaskPlatform("kling"),
			fail: func(ctx context.Context, channelID int, taskIDs []string, taskM map[string]*model.Task) error {
				return updateVideoTasks(ctx, constant.TaskPlatform("kling"), channelID, taskIDs, taskM)
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)

			userID := 410 + index
			channelID := 510 + index
			const chargedQuota = 700
			seedUser(t, userID, 4_300)

			task := makeTask(userID, channelID, chargedQuota, 0, BillingSourceWallet, 0)
			task.Platform = test.platform
			task.Status = model.TaskStatusInProgress
			task.Progress = "30%"
			task.PrivateData.BillingState = model.TaskBillingStateAccepted
			task.PrivateData.UpstreamTaskID = "upstream-missing-channel-" + test.name
			require.NoError(t, model.DB.Create(task).Error)

			taskIDs := []string{task.PrivateData.UpstreamTaskID}
			taskM := map[string]*model.Task{task.PrivateData.UpstreamTaskID: task}
			require.Error(t, test.fail(context.Background(), channelID, taskIDs, taskM))
			require.Error(t, test.fail(context.Background(), channelID, taskIDs, taskM))

			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
			assert.Equal(t, "30%", persisted.Progress)
			assert.Empty(t, persisted.FailReason)
			assert.Equal(t, model.TaskBillingStateAccepted, persisted.PrivateData.BillingState)
			assert.Equal(t, chargedQuota, persisted.Quota)
			assert.EqualValues(t, 4_300, getUserQuota(t, userID))

			var auditCount int64
			require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
				Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).
				Count(&auditCount).Error)
			assert.Zero(t, auditCount)
		})
	}
}

func TestMissingVideoChannelPollingRecoversWithoutRefund(t *testing.T) {
	truncate(t)

	const userID, channelID = 420, 520
	const chargedQuota = 700
	seedUser(t, userID, 4_300)

	task := makeTask(userID, channelID, chargedQuota, 0, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusInProgress
	task.Progress = "30%"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.UpstreamTaskID = "upstream-restored-channel"
	require.NoError(t, model.DB.Create(task).Error)

	taskIDs := []string{task.PrivateData.UpstreamTaskID}
	taskM := map[string]*model.Task{task.PrivateData.UpstreamTaskID: task}
	require.Error(t, updateVideoTasks(context.Background(), task.Platform, channelID, taskIDs, taskM))

	seedTaskPollingChannel(t, channelID, true)
	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	require.NoError(t, updateVideoTasks(context.Background(), task.Platform, channelID, taskIDs, taskM))
	assert.Equal(t, []string{task.PrivateData.UpstreamTaskID}, adaptor.fetchedTaskIDs())

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
	assert.Equal(t, model.TaskBillingStateAccepted, persisted.PrivateData.BillingState)
	assert.Equal(t, chargedQuota, persisted.Quota)
	assert.EqualValues(t, 4_300, getUserQuota(t, userID))

	var auditCount int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
		Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).
		Count(&auditCount).Error)
	assert.Zero(t, auditCount)
}
