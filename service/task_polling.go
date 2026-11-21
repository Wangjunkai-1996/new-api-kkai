package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/MoeForge/moego/ojson"
	"github.com/bytedance/gopkg/util/gopool"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, task *model.Task, proxy string) (*http.Response, error)
	ParseTaskResult(task *model.Task, resp *http.Response, body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// ContextTaskPollingAdaptor is implemented by adaptors that can propagate the
// caller's cancellation to provider requests and plugin hooks. It is optional
// so existing built-in adaptors keep the small legacy interface above.
type ContextTaskPollingAdaptor interface {
	FetchTaskContext(ctx context.Context, baseURL string, key string, task *model.Task, proxy string) (*http.Response, error)
	ParseTaskResultContext(ctx context.Context, task *model.Task, resp *http.Response, body []byte) (*relaycommon.TaskInfo, error)
}

type BatchTaskPollingAdaptor interface {
	TaskPollingAdaptor
	FetchMode() string
	FetchBatchTasks(baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error)
	ParseBatchResult(tasks []*model.Task, resp *http.Response, body []byte) (map[string]*BatchTaskResult, error)
}

// ContextBatchTaskPollingAdaptor is the context-aware counterpart for batch
// plugin adaptors. Implementations may provide either this optional interface
// or the legacy methods on BatchTaskPollingAdaptor.
type ContextBatchTaskPollingAdaptor interface {
	FetchBatchTasksContext(ctx context.Context, baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error)
	ParseBatchResultContext(ctx context.Context, tasks []*model.Task, resp *http.Response, body []byte) (map[string]*BatchTaskResult, error)
}

// TaskPollingResponseBodyLimiter lets a protocol choose a decompressed
// response budget. A non-positive value keeps the legacy unbounded read for
// adaptors whose valid result size is not known to the service layer.
type TaskPollingResponseBodyLimiter interface {
	PollResponseBodyLimit(taskCount int) int64
}

// ErrTaskPollingCASLost indicates that another worker committed a newer task
// snapshot while this poll was in flight. Callers that need the current result
// should reload the task and render that durable row.
var ErrTaskPollingCASLost = errors.New("task polling compare-and-swap lost")
var ErrTaskPollingResponseTooLarge = errors.New("task polling response exceeds the configured body limit")

func taskPollingResponseBodyLimit(adaptor TaskPollingAdaptor, taskCount int) int64 {
	if limiter, ok := adaptor.(TaskPollingResponseBodyLimiter); ok {
		return limiter.PollResponseBodyLimit(taskCount)
	}
	return 0
}

func consumePollingResponse(ctx context.Context, adaptor TaskPollingAdaptor, resp *http.Response, fetchErr error, taskCount int, requireOK bool) ([]byte, error) {
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	if resp == nil || resp.Body == nil {
		return nil, ErrVideoArchiveResponseRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || requireOK && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("task status query returned HTTP %d", resp.StatusCode)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := taskPollingResponseBodyLimit(adaptor, taskCount)
	var reader io.Reader = resp.Body
	if limit > 0 && limit < math.MaxInt64 {
		reader = io.LimitReader(reader, limit+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, ErrTaskPollingResponseTooLarge
	}
	if len(body) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

type BatchTaskResult struct {
	TaskInfo   relaycommon.TaskInfo
	Action     string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Data       any
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

func fetchTaskWithContext(ctx context.Context, adaptor TaskPollingAdaptor, baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	if contextual, ok := adaptor.(ContextTaskPollingAdaptor); ok {
		return contextual.FetchTaskContext(ctx, baseURL, key, task, proxy)
	}
	return adaptor.FetchTask(baseURL, key, task, proxy)
}

func parseTaskResultWithContext(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, resp *http.Response, body []byte) (*relaycommon.TaskInfo, error) {
	if contextual, ok := adaptor.(ContextTaskPollingAdaptor); ok {
		return contextual.ParseTaskResultContext(ctx, task, resp, body)
	}
	return adaptor.ParseTaskResult(task, resp, body)
}

func fetchBatchTasksWithContext(ctx context.Context, adaptor BatchTaskPollingAdaptor, baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error) {
	if contextual, ok := adaptor.(ContextBatchTaskPollingAdaptor); ok {
		return contextual.FetchBatchTasksContext(ctx, baseURL, key, tasks, proxy)
	}
	return adaptor.FetchBatchTasks(baseURL, key, tasks, proxy)
}

func parseBatchResultWithContext(ctx context.Context, adaptor BatchTaskPollingAdaptor, tasks []*model.Task, resp *http.Response, body []byte) (map[string]*BatchTaskResult, error) {
	if contextual, ok := adaptor.(ContextBatchTaskPollingAdaptor); ok {
		return contextual.ParseBatchResultContext(ctx, tasks, resp, body)
	}
	return adaptor.ParseBatchResult(tasks, resp, body)
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := "Task timed out"
	legacyReason := "Task timed out (a task left by the old system is not refunded; contact the administrator)"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		if task.PrivateData.MembershipBillingRequestID != "" &&
			(task.PrivateData.BillingState == model.TaskBillingStateDispatching || task.PrivateData.BillingState == model.TaskBillingStateAccepted || task.PrivateData.BillingState == model.TaskBillingStateAmbiguous) {
			if err := model.MarkTaskMembershipBillingUncertain(ctx, task.ID, "task_polling_deadline_elapsed"); err != nil {
				logger.LogError(ctx, fmt.Sprintf("retain timed-out membership task row %d (%T)", task.ID, err))
			}
			continue
		}
		if awaitingTaskSubmissionReceipt(task) {
			continue
		}
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		snap := task.Snapshot()
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatusPreservingBilling(snap)
		if err != nil {
			logger.LogError(ctx, common.LogText("sweepTimedOutTasks CAS update error for task row %d (%T)", task.ID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, common.LogText("sweepTimedOutTasks: task row %d already transitioned, skip", task.ID))
			continue
		}
		timedOutCount++
		perfmetrics.RecordTaskResult(task, nil)
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, common.LogText("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if GetTaskAdaptorFunc == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	common.SysLog(common.LogText("task progress polling started"))
	sweepTimedOutTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		channelTasks := make(map[int][]*model.Task)
		nullTasks := make([]*model.Task, 0)
		for _, task := range tasks {
			if awaitingTaskSubmissionReceipt(task) {
				continue
			}
			if pollingUpstreamID(task) == "" {
				nullTasks = append(nullTasks, task)
				continue
			}
			channelTasks[task.ChannelId] = append(channelTasks[task.ChannelId], task)
		}
		for _, task := range nullTasks {
			won, err := failPollingTask(ctx, task, "task is missing upstream task ID")
			if err != nil {
				logger.LogError(ctx, common.LogText("fail task row %d with missing upstream ID (%T)", task.ID, err))
				continue
			}
			if won {
				summary.NullTasksFailed++
			}
		}
		if platform != constant.TaskPlatformMidjourney {
			pollChannels(ctx, platform, channelTasks)
		}
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog(common.LogText("task progress polling finished"))
	return summary
}

// requestedPollingRows also checks ownership for callers using the legacy ID maps.
func requestedPollingRows(channelIDs map[int][]string, tasks map[string]*model.Task) map[int][]*model.Task {
	rows := make(map[int][]*model.Task, len(channelIDs))
	for channelID, ids := range channelIDs {
		for _, id := range ids {
			t := tasks[id]
			if t != nil && t.ChannelId == channelID && id != "" && id == pollingUpstreamID(t) {
				rows[channelID] = append(rows[channelID], t)
			}
		}
	}
	return rows
}

// Submission recovery owns provisional rows until the provider accepts the task.
func awaitingTaskSubmissionReceipt(t *model.Task) bool {
	if t.PrivateData.UpstreamTaskID != "" {
		return false
	}
	switch t.PrivateData.BillingState {
	case model.TaskBillingStatePending, model.TaskBillingStateReserved, model.TaskBillingStateDispatching:
		return true
	}
	return false
}

func pollingUpstreamID(t *model.Task) string {
	if t.PrivateData.MembershipBillingRequestID != "" || awaitingTaskSubmissionReceipt(t) {
		return t.PrivateData.UpstreamTaskID
	}
	return t.GetUpstreamTaskID()
}

func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, ids map[int][]string, tasks map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	if platform != constant.TaskPlatformMidjourney {
		pollChannels(ctx, platform, requestedPollingRows(ids, tasks))
	}
}

func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, ids map[int][]string, tasks map[string]*model.Task) error {
	pollChannels(ctx, platform, requestedPollingRows(ids, tasks))
	return ctx.Err()
}

func pollChannels(ctx context.Context, platform constant.TaskPlatform, channels map[int][]*model.Task) {
	var wg sync.WaitGroup
	for channelID, rows := range channels {
		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := pollChannelTasks(ctx, platform, channelID, rows, nil); err != nil && !errors.Is(err, ErrTaskPollingCASLost) {
				logger.LogWarn(ctx, common.LogText("task polling deferred for channel %d (%T)", channelID, err))
			}
		})
	}
	wg.Wait()
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelID int, ids []string, tasks map[string]*model.Task) error {
	rows := requestedPollingRows(map[int][]string{channelID: ids}, tasks)
	return pollChannelTasks(ctx, platform, channelID, rows[channelID], nil)
}

func updateSunoTasks(ctx context.Context, channelID int, ids []string, tasks map[string]*model.Task) error {
	err := updateVideoTasks(ctx, constant.TaskPlatformSuno, channelID, ids, tasks)
	if errors.Is(err, ErrTaskPollingCASLost) {
		return nil
	}
	return err
}

func UpdateSunoTasks(ctx context.Context, ids map[int][]string, tasks map[string]*model.Task) error {
	return UpdateVideoTasks(ctx, constant.TaskPlatformSuno, ids, tasks)
}

func UpdateBatchTasks(ctx context.Context, batch BatchTaskPollingAdaptor, ids map[int][]string, tasks map[string]*model.Task) error {
	var failures []error
	for channelID, rows := range requestedPollingRows(ids, tasks) {
		err := pollChannelTasks(ctx, "", channelID, rows, batch)
		if err != nil && !errors.Is(err, ErrTaskPollingCASLost) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// PollTask applies a single observed row through the same provider and CAS path as the scheduler.
func PollTask(ctx context.Context, task *model.Task) error {
	if task == nil || task.ID <= 0 || pollingUpstreamID(task) == "" {
		return model.ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return pollChannelTasks(ctx, task.Platform, task.ChannelId, []*model.Task{task}, nil)
}

func pollingCredential(ch *model.Channel, t *model.Task) (string, bool) {
	return t.ResolveChannelCredential(ch)
}

func initPollingAdaptor(adaptor TaskPollingAdaptor, ch *model.Channel, key string) string {
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(ch.Type)
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: ch.Type, ChannelId: ch.Id, ChannelBaseUrl: baseURL, ApiKey: key,
		ChannelSetting: ch.GetSetting(), HeadersOverride: ch.GetHeaderOverride(),
	}})
	return baseURL
}

func pollChannelTasks(ctx context.Context, platform constant.TaskPlatform, channelID int, rows []*model.Task, adaptor TaskPollingAdaptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelID)
	if err != nil {
		return err
	}
	if adaptor == nil && GetTaskAdaptorFunc != nil {
		adaptor = GetTaskAdaptorFunc(platform)
	}
	if adaptor == nil {
		return errors.New("task polling adaptor unavailable")
	}
	batch, isBatch := adaptor.(BatchTaskPollingAdaptor)
	isBatch = isBatch && batch.FetchMode() == "batch"
	suno := platform == constant.TaskPlatformSuno
	if suno && !isBatch {
		return errors.New("suno adaptor does not support batch polling")
	}
	type binding struct{ key, id string }
	owners := make(map[binding]*model.Task)
	var ordered []binding
	var failures []error
	for _, t := range rows {
		key, ok := pollingCredential(ch, t)
		if !ok {
			logger.LogWarn(ctx, common.LogText("missing polling credential for channel %d task row %d", channelID, t.ID))
			failures = append(failures, errors.New("missing polling credential"))
			continue
		}
		b := binding{key, pollingUpstreamID(t)}
		owner, seen := owners[b]
		if !seen {
			ordered = append(ordered, b)
			owners[b] = t
		} else if owner != nil && owner.ID != t.ID {
			logger.LogWarn(ctx, common.LogText("ambiguous polling ID on channel %d for task rows %d and %d", channelID, owner.ID, t.ID))
			owners[b] = nil
		}
	}
	grouped := make(map[string][]*model.Task)
	singleSent := false
	for _, b := range ordered {
		t := owners[b]
		if t == nil {
			continue
		}
		if isBatch {
			grouped[b.key] = append(grouped[b.key], t)
			continue
		}
		if singleSent && !ch.GetOtherSettings().DisableTaskPollingSleep {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		singleSent = true
		if err := pollVideoTask(ctx, adaptor, ch, b.key, t); err != nil {
			failures = append(failures, err)
		}
	}
	for key, tasks := range grouped {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := pollBatch(ctx, batch, ch, key, tasks, suno); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func pollBatch(ctx context.Context, adaptor BatchTaskPollingAdaptor, ch *model.Channel, key string, tasks []*model.Task, suno bool) error {
	baseURL := initPollingAdaptor(adaptor, ch, key)
	resp, fetchErr := fetchBatchTasksWithContext(ctx, adaptor, baseURL, key, tasks, ch.GetSetting().Proxy)
	body, err := consumePollingResponse(ctx, adaptor, resp, fetchErr, len(tasks), suno)
	if err != nil {
		return err
	}
	results, err := parseBatchResultWithContext(ctx, adaptor, tasks, resp, body)
	if err != nil {
		return err
	}
	var failures []error
	for _, observed := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := pollingUpstreamID(observed)
		item := results[id]
		if item == nil || item.TaskInfo.TaskID != id {
			continue
		}
		status := model.TaskStatus(item.TaskInfo.Status)
		switch status {
		case model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure, model.TaskStatusUnknown:
		case "":
			if !suno {
				continue
			}
		default:
			continue
		}
		t := *observed
		before := t.Snapshot()
		if status != "" {
			t.Status = status
		}
		if item.TaskInfo.Progress != "" {
			t.Progress = item.TaskInfo.Progress
		}
		if item.TaskInfo.Reason != "" {
			t.FailReason = item.TaskInfo.Reason
		}
		if item.TaskInfo.Url != "" {
			t.PrivateData.ResultURL = item.TaskInfo.Url
		}
		if len(item.TaskInfo.PluginState) > 0 {
			t.PrivateData.PluginState = item.TaskInfo.PluginState
		}
		if item.Data != nil {
			t.SetData(item.Data)
		}
		if suno {
			if item.SubmitTime != 0 {
				t.SubmitTime = item.SubmitTime
			}
			if item.StartTime != 0 {
				t.StartTime = item.StartTime
			}
			if item.FinishTime != 0 {
				t.FinishTime = item.FinishTime
			}
		}
		// Old Suno drivers can report a reason without a failure status.
		legacyRefund := suno && t.PrivateData.BillingState == "" && t.PrivateData.MembershipBillingRequestID == "" && item.TaskInfo.Reason != "" && !terminalTaskStatus(before.Status) && before.Progress != "100%"
		if legacyRefund || terminalTaskStatus(t.Status) {
			t.Progress = taskcommon.ProgressComplete
			if terminalTaskStatus(t.Status) && t.FinishTime == 0 {
				t.FinishTime = time.Now().Unix()
			}
		}
		err := persistPolledTask(ctx, adaptor, &t, before, &item.TaskInfo, legacyRefund)
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// failPollingTask owns failure transitions discovered outside a provider
// response when the task itself is no longer pollable (for example a missing
// upstream task ID). The full snapshot guard prevents a stale poller from
// overwriting a concurrent success, and only the CAS winner may refund quota.
func failPollingTask(ctx context.Context, observed *model.Task, reason string) (bool, error) {
	if observed == nil || observed.ID <= 0 {
		return false, model.ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reason == "" {
		reason = "task polling failed"
	}
	if observed.PrivateData.MembershipBillingRequestID != "" &&
		(observed.PrivateData.BillingState == model.TaskBillingStateDispatching || observed.PrivateData.BillingState == model.TaskBillingStateAccepted || observed.PrivateData.BillingState == model.TaskBillingStateAmbiguous) {
		return false, model.MarkTaskMembershipBillingUncertain(ctx, observed.ID, "task_polling_result_unavailable")
	}

	failedTask := *observed
	snapshot := observed.Snapshot()
	failedTask.Status = model.TaskStatusFailure
	failedTask.Progress = "100%"
	failedTask.FailReason = reason
	failedTask.FinishTime = time.Now().Unix()

	won, err := failedTask.UpdateWithStatusPreservingBilling(snapshot)
	if err != nil || !won {
		return won, err
	}
	if snapshot.Status != model.TaskStatusSuccess && snapshot.Status != model.TaskStatusFailure {
		perfmetrics.RecordTaskResult(&failedTask, nil)
	}
	if failedTask.Quota == 0 {
		return true, nil
	}
	if failedTask.PrivateData.BillingState != "" {
		if err := refundDurableTaskQuota(ctx, &failedTask, reason); err != nil {
			return true, err
		}
		return true, nil
	}
	RefundTaskQuota(ctx, &failedTask, reason)
	return true, nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, id string, tasks map[string]*model.Task) error {
	t := tasks[id]
	if t == nil || t.ChannelId != ch.Id || pollingUpstreamID(t) != id {
		return model.ErrTaskBillingInvalidRequest
	}
	key, ok := pollingCredential(ch, t)
	if !ok {
		return errors.New("missing polling credential")
	}
	err := pollVideoTask(ctx, adaptor, ch, key, t)
	if errors.Is(err, ErrTaskPollingCASLost) {
		return nil
	}
	return err
}

func pollVideoTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, key string, observed *model.Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pollingTask := *observed
	task := &pollingTask
	taskId := pollingUpstreamID(task)
	managedResult := task.IsAssetHostedResult()
	baseURL := initPollingAdaptor(adaptor, ch, key)
	resp, fetchErr := fetchTaskWithContext(ctx, adaptor, baseURL, key, task, ch.GetSetting().Proxy)
	responseBody, err := consumePollingResponse(ctx, adaptor, resp, fetchErr, 1, false)
	if err != nil {
		return err
	}

	if managedResult {
		logger.LogDebug(ctx, "updateVideoSingleTask managed response: status_code=%d response_bytes=%d content_type_bytes=%d", resp.StatusCode, len(responseBody), len(resp.Header.Get("Content-Type")))
	}
	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		if responseItems.Data.TaskID != "" && responseItems.Data.TaskID != taskId && responseItems.Data.TaskID != task.TaskID {
			return fmt.Errorf("task status query returned an unexpected task identity for local task %d", task.ID)
		}
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = parseTaskResultWithContext(ctx, adaptor, task, resp, responseBody); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("task result parsing failed for local task %d (%T)", task.ID, err)
	}

	if taskResult == nil || taskResult.Status == "" {
		return fmt.Errorf("task status query returned empty status for local task %d (response bytes %d)", task.ID, len(responseBody))
	}

	if len(taskResult.PluginState) > 0 {
		task.PrivateData.PluginState = append([]byte(nil), taskResult.PluginState...)
	}
	task.Data = redactVideoResponseBody(responseBody)

	if managedResult {
		logger.LogDebug(ctx, "updateVideoSingleTask managed result: status_bytes=%d progress_bytes=%d url_present=%t remote_url_present=%t", len(taskResult.Status), len(taskResult.Progress), taskResult.Url != "", taskResult.RemoteUrl != "")
	} else {
		logger.LogDebug(ctx, "updateVideoSingleTask response: status_code=%d response_bytes=%d content_type_bytes=%d", resp.StatusCode, len(responseBody), len(resp.Header.Get("Content-Type")))
	}

	now := time.Now().Unix()

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if task.IsAssetHostedResult() {
			archiveSource := strings.TrimSpace(taskResult.Url)
			if archiveSource == "" {
				archiveSource = strings.TrimSpace(taskResult.RemoteUrl)
			}
			if archiveSource != "" {
				task.PrivateData.ArchiveSource = archiveSource
			}
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	case model.TaskStatusFailure:
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		refundReason := taskResult.Reason
		task.PrivateData.Failure = model.NewTaskFailureSnapshot("generation", "", refundReason, task.Data)
		task.FailReason = task.PublicFailureReason(refundReason)
		logger.LogInfo(ctx, common.LogText("task polling failed local_id=%d reason_bytes=%d", task.ID, len(refundReason)))
		taskResult.Progress = taskcommon.ProgressComplete
	default:
		return fmt.Errorf("unknown status for local task %d (status bytes %d)", task.ID, len(taskResult.Status))
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	return persistPolledTask(ctx, adaptor, task, snap, taskResult, false)
}

func terminalTaskStatus(s model.TaskStatus) bool {
	return s == model.TaskStatusSuccess || s == model.TaskStatusFailure
}

func persistPolledTask(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, before model.TaskSnapshot, result *relaycommon.TaskInfo, legacyRefund bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if before.Equal(task.Snapshot()) {
		return nil
	}
	firstTerminal := !terminalTaskStatus(before.Status) && terminalTaskStatus(task.Status)
	var charge *model.TaskCompletionCharge
	if firstTerminal && task.Status == model.TaskStatusSuccess && !legacyRefund {
		if bc := task.PrivateData.BillingContext; bc != nil && bc.TieredSnapshot != nil {
			billing := *bc
			snapshot := *bc.TieredSnapshot
			billing.TieredSnapshot = &snapshot
			task.PrivateData.BillingContext = &billing
		}
		if err := prepareTaskMembershipCompletion(task, result); err != nil {
			return err
		}
		charge = assessTaskCompletion(ctx, adaptor, task, result)
		if task.PrivateData.BillingState != "" && task.PrivateData.MembershipBillingRequestID == "" {
			task.PrivateData.CompletionCharge = charge
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	won, err := task.UpdateWithStatusPreservingBilling(before)
	if err != nil {
		return err
	}
	if !won {
		return ErrTaskPollingCASLost
	}
	if firstTerminal {
		perfmetrics.RecordTaskResult(task, result)
	}
	if (firstTerminal && task.Status == model.TaskStatusFailure) || legacyRefund {
		if !RefundTaskQuota(ctx, task, result.Reason) {
			return errors.New("task refund pending")
		}
	} else if charge != nil {
		RecalculateTaskQuota(ctx, task, charge.Quota, charge.Reason, charge.Clamp)
	}
	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	// Only the redacted members change; the rest keeps the upstream bytes.
	if !ojson.ValidBytes(body) || !ojson.GetBytesNoCopy(body, "@this").IsObject() {
		return body
	}
	doc := ojson.NewDoc(body)
	if resp := ojson.GetBytesNoCopy(body, "response"); resp.IsObject() {
		_ = doc.Delete("response.bytesBase64Encoded")
		if video := resp.Get("video"); video.Type == ojson.String {
			_ = doc.Set("response.video", truncateBase64(video.Str))
		}
		if videos := resp.Get("videos"); videos.IsArray() {
			for i, video := range videos.Array() {
				if video.IsObject() {
					_ = doc.Delete(fmt.Sprintf("response.videos.%d.bytesBase64Encoded", i))
				}
			}
		}
	}
	if steps := ojson.GetBytesNoCopy(body, "steps"); steps.IsArray() {
		for i, step := range steps.Array() {
			parts := step.Get("content")
			if !parts.IsArray() {
				continue
			}
			for j, part := range parts.Array() {
				if part.Get("type").String() == "video" {
					_ = doc.Delete(fmt.Sprintf("steps.%d.content.%d.data", i, j))
				}
			}
		}
	}
	return doc.Bytes()
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// assessTaskCompletion computes the target before the first terminal CAS. The
// same assessment drives the initial settlement and its durable retry.
func assessTaskCompletion(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) *model.TaskCompletionCharge {
	if quota := task.PrivateData.MembershipCompletionQuota; quota != nil {
		return &model.TaskCompletionCharge{Quota: *quota, Reason: "task completion"}
	}
	bc := task.PrivateData.BillingContext
	if bc != nil && bc.TieredSnapshot != nil {
		if len(result.UsageFacts) == 0 {
			return nil
		}
		settlement, facts, err := EvaluateTaskCompletionUsage(bc.TieredSnapshot, result.UsageFacts)
		if err != nil {
			logger.LogWarn(ctx, common.LogText("task completion usage settlement failed; retaining reserved quota (local_id=%d)", task.ID))
			return nil
		}
		bc.TieredSnapshot.UsageFacts, bc.TieredSnapshot.EstimatedTier = facts, settlement.MatchedTier
		return &model.TaskCompletionCharge{Quota: settlement.ActualQuotaAfterGroup, Reason: "Settled by the task usage expression", Clamp: settlement.Clamp}
	}
	if bc != nil && bc.PerCallBilling {
		seconds, ok := result.UsageFacts["seconds"].(float64)
		if !ok || bc.OtherRatios["seconds"] <= 0 {
			return nil
		}
		return assessTaskQuotaBySeconds(task, seconds)
	}
	var quota int
	if a, ok := adaptor.(interface {
		AdjustBillingOnCompleteContext(context.Context, *model.Task, *relaycommon.TaskInfo) int
	}); ok {
		quota = a.AdjustBillingOnCompleteContext(ctx, task, result)
	} else {
		quota = adaptor.AdjustBillingOnComplete(task, result)
	}
	if quota > 0 {
		return &model.TaskCompletionCharge{Quota: quota, Reason: "Adjusted by the channel adaptor"}
	}
	return assessTaskQuotaByTokens(task, result.TotalTokens)
}

// settleTaskBillingOnComplete keeps the legacy direct settlement entry point
// available to callers and tests. Polling itself uses assessTaskCompletion
// before its terminal CAS so the target is durable before money moves.
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) {
	if charge := assessTaskCompletion(ctx, adaptor, task, result); charge != nil {
		RecalculateTaskQuota(ctx, task, charge.Quota, charge.Reason, charge.Clamp)
	}
}
