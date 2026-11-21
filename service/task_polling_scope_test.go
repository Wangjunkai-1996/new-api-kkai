package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type scopedPollBatch struct {
	taskPollingFetchAdaptor
	t      *testing.T
	info   *relaycommon.RelayInfo
	record func(int, string, string)
}

func (a *scopedPollBatch) Init(info *relaycommon.RelayInfo) { a.info = info }
func (a *scopedPollBatch) FetchMode() string                { return "batch" }
func (a *scopedPollBatch) FetchBatchTasks(_ string, key string, tasks []*model.Task, _ string) (*http.Response, error) {
	assert.Equal(a.t, a.info.ApiKey, key)
	for _, task := range tasks {
		assert.Equal(a.t, a.info.ChannelId, task.ChannelId)
		a.record(task.ChannelId, key, task.GetUpstreamTaskID())
	}
	if key == "broken" {
		return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("error"))}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}
func (a *scopedPollBatch) ParseBatchResult(tasks []*model.Task, _ *http.Response, _ []byte) (map[string]*BatchTaskResult, error) {
	out := map[string]*BatchTaskResult{"unrequested": {TaskInfo: relaycommon.TaskInfo{TaskID: "unrequested", Status: model.TaskStatusFailure}}}
	status := model.TaskStatusSuccess
	if a.info.ApiKey == "second" {
		status = model.TaskStatusFailure
	}
	for _, task := range tasks {
		id := task.GetUpstreamTaskID()
		out[id] = &BatchTaskResult{TaskInfo: relaycommon.TaskInfo{TaskID: id, Status: status, TotalTokens: 150}}
	}
	return out, nil
}

func TestPollingBindsSharedProviderIDsToChannelAndCredential(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	oldLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = oldLimit })
	for _, channel := range []int{501, 502} {
		seedTaskPollingChannel(t, channel, true)
	}
	const userStart = 550
	specs := []struct {
		channel               int
		key, public, upstream string
		expected              model.TaskStatus
		balance               int
	}{
		{501, "first", "first-public", "shared", model.TaskStatusSuccess, 850},
		{502, "second", "second-public", "shared", model.TaskStatusFailure, 1000},
		{501, "second", "third-public", "shared", model.TaskStatusFailure, 1000},
		{501, "first", "fourth-public", "collision", model.TaskStatusInProgress, 900},
		{501, "first", "fifth-public", "collision", model.TaskStatusInProgress, 900},
		{501, "", "unbound-public", "unrequested", model.TaskStatusInProgress, 900},
		{501, "first", "legacy", "", model.TaskStatusSuccess, 850},
		{502, "broken", "bad-upstream", "broken-id", model.TaskStatusInProgress, 900},
	}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, 501).Error)
	channel.ChannelInfo.IsMultiKey = true
	channel.Key = "first\nsecond"
	require.NoError(t, model.DB.Save(&channel).Error)
	var requests []string
	var mu sync.Mutex
	factory := GetTaskAdaptorFunc
	t.Cleanup(func() { GetTaskAdaptorFunc = factory })
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &scopedPollBatch{t: t, record: func(channel int, key, id string) {
			mu.Lock()
			defer mu.Unlock()
			requests = append(requests, fmt.Sprintf("%d/%s/%s", channel, key, id))
		}}
	}
	var rows []*model.Task
	for i, item := range specs {
		user := userStart + i
		require.NoError(t, model.DB.Create(&model.User{Id: user, Username: fmt.Sprintf("poll-user-%d", user), AffCode: fmt.Sprint(user), Quota: 900, Status: common.UserStatusEnabled}).Error)
		task := makeTask(user, item.channel, 100, 0, BillingSourceWallet, 0)
		task.TaskID, task.Progress, task.Platform = item.public, "30%", "scoped-batch"
		task.PrivateData.BillingState = model.TaskBillingStateAccepted
		task.PrivateData.BillingContext.ModelRatio = 1
		task.PrivateData.Key, task.PrivateData.UpstreamTaskID = item.key, item.upstream
		require.NoError(t, model.DB.Create(task).Error)
		rows = append(rows, task)
	}
	RunTaskPollingOnce(ctx, nil)
	assert.ElementsMatch(t, []string{"501/first/shared", "502/second/shared", "501/second/shared", "501/first/legacy", "502/broken/broken-id"}, requests)
	for i, item := range specs {
		var row model.Task
		require.NoError(t, model.DB.First(&row, rows[i].ID).Error)
		assert.Equal(t, item.expected, row.Status, item.public)
		assert.EqualValues(t, item.balance, getUserQuota(t, userStart+i), item.public)
		if terminalTaskStatus(row.Status) {
			require.NoError(t, PollTask(ctx, &row))
			assert.EqualValues(t, item.balance, getUserQuota(t, userStart+i))
		}
	}
	var events int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).Count(&events).Error)
	assert.EqualValues(t, 4, events)
}

func TestPollingWaitsForSubmissionReceipt(t *testing.T) {
	truncate(t)
	const channelID = 639
	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })
	seedTaskPollingChannel(t, channelID, true)
	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	for _, state := range []string{model.TaskBillingStatePending, model.TaskBillingStateReserved, model.TaskBillingStateDispatching} {
		task := seedPollingTask(t, channelID, "local-"+state, "")
		task.PrivateData.BillingState = state
		task.SubmitTime = time.Now().Add(-2 * time.Hour).Unix()
		require.NoError(t, model.DB.Save(task).Error)
		require.ErrorIs(t, PollTask(t.Context(), task), model.ErrTaskBillingInvalidRequest)
	}
	RunTaskPollingOnce(t.Context(), nil)
	assert.Empty(t, adaptor.fetchedTaskIDs())
	var waiting int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("status = ? AND progress = ?", model.TaskStatusInProgress, "30%").Count(&waiting).Error)
	assert.EqualValues(t, 3, waiting)
}

func TestPollingFinalChargeSurvivesInterruptedSettlement(t *testing.T) {
	truncate(t)
	const channelID, userID = 615, 616
	seedTaskPollingChannel(t, channelID, true)
	seedUser(t, userID, 900)
	task := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	task.Platform, task.Progress = "test", "30%"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.UpstreamTaskID = "provider-job"
	task.PrivateData.TargetQuota = common.GetPointer(90)
	task.PrivateData.BillingContext.ModelRatio = 1
	require.NoError(t, model.DB.Create(task).Error)
	a := &taskPollingResponseAdaptor{statusCode: 200, body: "{}", result: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 150}}
	factory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return a }
	t.Cleanup(func() { GetTaskAdaptorFunc = factory })
	ctx := context.Background()
	failCAS, failFunds := true, true
	callback := "test:failed_terminal_commit"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" && failCAS || tx.Statement.Table == "users" && failFunds {
			tx.AddError(errors.New("simulated interrupted write"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callback) })
	require.Error(t, PollTask(ctx, task))
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Nil(t, stored.PrivateData.CompletionCharge)
	failCAS = false
	_ = PollTask(ctx, task)
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
	require.NotNil(t, stored.PrivateData.TargetQuota)
	assert.Equal(t, 150, *stored.PrivateData.TargetQuota)
	assert.EqualValues(t, 900, getUserQuota(t, userID))
	require.ErrorIs(t, model.CompleteTaskBillingRecovery(ctx, task.ID), model.ErrTaskAccountingNotReady)
	failFunds = false
	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID, Acceptance: &TaskBillingRecoveryAcceptanceReceipt{UpstreamTaskID: "provider-job", Status: model.TaskStatusSubmitted, TargetQuota: 90, ChannelID: channelID}})
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(ctx, model.KKAIOutboxEvent{Payload: string(payload)}))
	}
	_, err = model.AdjustTaskBilling(ctx, task.ID, 90)
	require.NoError(t, err)
	var completed model.Task
	require.NoError(t, model.DB.First(&completed, task.ID).Error)
	assert.Equal(t, 150, completed.Quota)
	assert.Equal(t, model.TaskBillingStateCompleted, completed.PrivateData.BillingState)
	assert.Nil(t, completed.PrivateData.TargetQuota)
	assert.EqualValues(t, 850, getUserQuota(t, userID))
	var events int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).Count(&events).Error)
	assert.EqualValues(t, 1, events)
}

type cancelOnCompletionPollingAdaptor struct {
	taskPollingResponseAdaptor
	cancel context.CancelFunc
}

func (a *cancelOnCompletionPollingAdaptor) AdjustBillingOnCompleteContext(context.Context, *model.Task, *relaycommon.TaskInfo) int {
	a.cancel()
	return 150
}

func TestPollingCancellationDuringCompletionAssessmentPreservesTask(t *testing.T) {
	truncate(t)
	const channelID, userID = 625, 626
	seedTaskPollingChannel(t, channelID, true)
	seedUser(t, userID, 900)
	task := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	task.Platform = "test"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.UpstreamTaskID = "provider-job"
	task.PrivateData.BillingContext.ModelRatio = 1
	require.NoError(t, model.DB.Create(task).Error)
	ctx, cancel := context.WithCancel(context.Background())
	a := &cancelOnCompletionPollingAdaptor{
		taskPollingResponseAdaptor: taskPollingResponseAdaptor{
			statusCode: 200,
			body:       "{}",
			result:     &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 150},
		},
		cancel: cancel,
	}
	factory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return a }
	t.Cleanup(func() {
		GetTaskAdaptorFunc = factory
		cancel()
	})

	require.ErrorIs(t, PollTask(ctx, task), context.Canceled)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.Equal(t, string(model.TaskStatusInProgress), string(stored.Status))
	assert.Equal(t, 100, stored.Quota)
	assert.Nil(t, stored.PrivateData.CompletionCharge)
	assert.EqualValues(t, 900, getUserQuota(t, userID))
}

type trackedPollingBody struct {
	io.Reader
	closed int
}

func (b *trackedPollingBody) Close() error { b.closed++; return nil }

type boundedPollResponse struct {
	taskPollingResponseAdaptor
	body *trackedPollingBody
}

func (a *boundedPollResponse) PollResponseBodyLimit(int) int64 { return 2 }
func (a *boundedPollResponse) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: a.statusCode, Body: a.body, ContentLength: -1}, nil
}
func TestPollingBodyFailurePreservesTaskAndClosesResponse(t *testing.T) {
	truncate(t)
	seedTaskPollingChannel(t, 617, true)
	task := seedPollingTask(t, 617, "local-job", "provider-job")
	var ch model.Channel
	require.NoError(t, model.DB.First(&ch, 617).Error)
	for _, tc := range []struct {
		name   string
		code   int
		reader io.Reader
		valid  bool
	}{
		{"exact limit", 200, strings.NewReader("{}"), true},
		{"one byte over", 200, strings.NewReader("{} "), false},
		{"empty", 200, strings.NewReader(""), false},
		{"bad gateway", 502, strings.NewReader("{}"), false},
		{"truncated", 200, io.MultiReader(bytes.NewBufferString("{"), failedPollReader{}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedPollingBody{Reader: tc.reader}
			a := &boundedPollResponse{taskPollingResponseAdaptor: taskPollingResponseAdaptor{statusCode: tc.code, result: &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "30%"}}, body: body}
			err := pollVideoTask(context.Background(), a, &ch, ch.Key, task)
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, 1, a.parseCalls)
			} else {
				require.Error(t, err)
				assert.Zero(t, a.parseCalls)
			}
			assert.Equal(t, 1, body.closed)
			var row model.Task
			require.NoError(t, model.DB.First(&row, task.ID).Error)
			assert.Equal(t, task.Status, row.Status)
			assert.Equal(t, task.Quota, row.Quota)
		})
	}
}

type failedPollReader struct{}

func (failedPollReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestPollingChecksGatewayTaskResponseIdentity(t *testing.T) {
	truncate(t)
	seedTaskPollingChannel(t, 618, true)
	var ch model.Channel
	require.NoError(t, model.DB.First(&ch, 618).Error)
	for _, tc := range []struct {
		name, returned string
		allowed        bool
	}{
		{"upstream", "expected-upstream", true}, {"local", "public-local", true}, {"omitted", "", true}, {"mismatch", "other-operation", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := seedPollingTask(t, 618, "public-local", "expected-upstream")
			a := &taskPollingResponseAdaptor{statusCode: 200, body: fmt.Sprintf(`{"code":"success","data":{"task_id":%q,"status":"SUCCESS"}}`, tc.returned)}
			err := pollVideoTask(context.Background(), a, &ch, ch.Key, task)
			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, task.ID).Error)
			if tc.allowed {
				require.NoError(t, err)
				assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
			} else {
				require.Error(t, err)
				assert.Equal(t, task.Status, persisted.Status)
			}
		})
	}
}

func TestPollingSunoReasonRefundsLegacyTaskOnlyOnce(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(fmt.Sprint("durable=", durable), func(t *testing.T) {
			truncate(t)
			seedTaskPollingChannel(t, 619, true)
			seedUser(t, 620, 900)
			task := makeTask(620, 619, 100, 0, BillingSourceWallet, 0)
			task.Platform, task.Progress = constant.TaskPlatformSuno, "30%"
			if durable {
				task.PrivateData.BillingState = model.TaskBillingStateAccepted
			}
			task.PrivateData.UpstreamTaskID = "suno-job"
			require.NoError(t, model.DB.Create(task).Error)
			a := &membershipSunoPollingAdaptor{result: &BatchTaskResult{TaskInfo: relaycommon.TaskInfo{TaskID: "suno-job", Reason: "provider reported a reason"}}}
			factory := GetTaskAdaptorFunc
			GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return a }
			t.Cleanup(func() { GetTaskAdaptorFunc = factory })
			for range 2 {
				var row model.Task
				require.NoError(t, model.DB.First(&row, task.ID).Error)
				require.NoError(t, PollTask(context.Background(), &row))
			}
			var updated model.Task
			require.NoError(t, model.DB.First(&updated, task.ID).Error)
			if durable {
				assert.Equal(t, "30%", updated.Progress)
				assert.Equal(t, 100, updated.Quota)
				assert.EqualValues(t, 900, getUserQuota(t, 620))
			} else {
				assert.Equal(t, "100%", updated.Progress)
				assert.Zero(t, updated.Quota)
				assert.EqualValues(t, 1000, getUserQuota(t, 620))
				assert.EqualValues(t, 1, countLogs(t))
			}
		})
	}
}

func TestPollingRefusesRowReboundDuringProviderRequest(t *testing.T) {
	truncate(t)
	seedTaskPollingChannel(t, 624, true)
	task := seedPollingTask(t, 624, "local-operation", "first-receipt")
	var ch model.Channel
	require.NoError(t, model.DB.First(&ch, 624).Error)
	newReceipt := task.PrivateData
	newReceipt.UpstreamTaskID = "retry-receipt"
	require.NoError(t, model.DB.Model(task).Update("private_data", newReceipt).Error)
	task.PrivateData.UpstreamTaskID = "first-receipt"
	a := &taskPollingResponseAdaptor{statusCode: 200, body: "{}", result: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}}
	require.ErrorIs(t, pollVideoTask(context.Background(), a, &ch, ch.Key, task), ErrTaskPollingCASLost)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Equal(t, "retry-receipt", stored.GetUpstreamTaskID())
}
