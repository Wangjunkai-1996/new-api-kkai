package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
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

type BatchTaskPollingAdaptor interface {
	TaskPollingAdaptor
	FetchMode() string
	FetchBatchTasks(baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error)
	ParseBatchResult(tasks []*model.Task, resp *http.Response, body []byte) (map[string]*BatchTaskResult, error)
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
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
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
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
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

	common.SysLog("任务进度轮询开始")
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
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTasks := make([]*model.Task, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				nullTasks = append(nullTasks, task)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		for _, task := range nullTasks {
			won, err := failPollingTask(ctx, task, "task is missing upstream task ID")
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("fail task with missing upstream ID %d: %v", task.ID, err))
				continue
			}
			if won {
				summary.NullTasksFailed++
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if adaptor := GetTaskAdaptorFunc(platform); adaptor != nil {
			if batchAdaptor, ok := adaptor.(BatchTaskPollingAdaptor); ok && batchAdaptor.FetchMode() == "batch" {
				if err := UpdateBatchTasks(ctx, batchAdaptor, taskChannelM, taskM); err != nil {
					common.SysLog(fmt.Sprintf("UpdateBatchTasks fail: %s", err))
				}
				return
			}
		}
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateBatchTasks polls a plugin batch endpoint once per channel and applies
// each returned task through the same durable CAS/billing path as single polls.
func UpdateBatchTasks(ctx context.Context, adaptor BatchTaskPollingAdaptor, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelID, taskIDs := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ch, err := model.CacheGetChannel(channelID)
		if err != nil {
			return err
		}
		tasks := make([]*model.Task, 0, len(taskIDs))
		for _, id := range taskIDs {
			if task := taskM[id]; task != nil {
				tasks = append(tasks, task)
			}
		}
		baseURL := ch.GetBaseURL()
		if baseURL == "" {
			baseURL = constant.ChannelBaseURLs[ch.Type]
		}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: ch.Type, ChannelId: ch.Id, ChannelBaseUrl: baseURL, ApiKey: ch.Key}}
		adaptor.Init(info)
		resp, err := adaptor.FetchBatchTasks(baseURL, ch.Key, tasks, ch.GetSetting().Proxy)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		results, err := adaptor.ParseBatchResult(tasks, resp, body)
		if err != nil {
			return err
		}
		for upstreamID, result := range results {
			task := taskM[upstreamID]
			if task == nil {
				continue
			}
			snap := task.Snapshot()
			task.Status = model.TaskStatus(result.TaskInfo.Status)
			if result.TaskInfo.Progress != "" {
				task.Progress = result.TaskInfo.Progress
			}
			if result.TaskInfo.Reason != "" {
				task.FailReason = result.TaskInfo.Reason
			}
			if result.TaskInfo.Url != "" {
				task.PrivateData.ResultURL = result.TaskInfo.Url
			}
			if len(result.TaskInfo.PluginState) > 0 {
				task.PrivateData.PluginState = result.TaskInfo.PluginState
			}
			if result.Data != nil {
				task.SetData(result.Data)
			}
			if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
				task.Progress = taskcommon.ProgressComplete
				task.FinishTime = time.Now().Unix()
			}
			won, updateErr := task.UpdateWithStatusPreservingBilling(snap)
			if updateErr != nil || !won {
				continue
			}
			if task.Status == model.TaskStatusSuccess {
				settleTaskBillingOnComplete(ctx, adaptor, task, &result.TaskInfo)
			}
			if task.Status == model.TaskStatusFailure {
				RefundTaskQuota(ctx, task, task.FailReason)
			}
		}
	}
	return nil
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		return fmt.Errorf("CacheGetChannel failed for channel %d: %w", channelId, err)
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	batchAdaptor, ok := adaptor.(BatchTaskPollingAdaptor)
	if !ok || batchAdaptor.FetchMode() != "batch" {
		return errors.New("suno adaptor does not support batch polling")
	}
	tasks := make([]*model.Task, 0, len(taskIds))
	for _, taskID := range taskIds {
		if task := taskM[taskID]; task != nil {
			tasks = append(tasks, task)
		}
	}
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[ch.Type]
	}
	resp, err := batchAdaptor.FetchBatchTasks(baseURL, ch.Key, tasks, ch.GetSetting().Proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	results, err := batchAdaptor.ParseBatchResult(tasks, resp, responseBody)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}

	for _, responseItem := range results {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskInfo.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskInfo.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem.TaskInfo) {
			continue
		}
		snap := task.Snapshot()
		shouldRefund := false

		task.Status = lo.If(model.TaskStatus(responseItem.TaskInfo.Status) != "", model.TaskStatus(responseItem.TaskInfo.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.TaskInfo.Reason != "", responseItem.TaskInfo.Reason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.TaskInfo.Reason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Progress = "100%"
			shouldRefund = task.Quota != 0
		}
		if responseItem.TaskInfo.Status == string(model.TaskStatusSuccess) {
			task.Progress = "100%"
		}
		if responseItem.Data != nil {
			task.SetData(responseItem.Data)
		}
		if len(responseItem.TaskInfo.PluginState) > 0 {
			task.PrivateData.PluginState = responseItem.TaskInfo.PluginState
		}

		won, updateErr := task.UpdateWithStatusPreservingBilling(snap)
		if updateErr != nil {
			common.SysLog("UpdateSunoTask task error: " + updateErr.Error())
			continue
		}
		if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task %s already transitioned, skip billing", task.TaskID))
			continue
		}
		if shouldRefund {
			RefundTaskQuota(ctx, task, task.FailReason)
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask relaycommon.TaskInfo) bool {
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.Reason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	if newTask.Progress != "" && oldTask.Progress != newTask.Progress {
		return true
	}
	if strings.TrimSpace(newTask.Url) != "" && oldTask.GetResultURL() != newTask.Url {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		return fmt.Errorf("CacheGetChannel failed for channel %d: %w", channelId, err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ChannelMeta.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
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

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	sourceTask := taskM[taskId]
	if sourceTask == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	pollingTask := *sourceTask
	task := &pollingTask
	managedResult := task.IsAssetHostedResult()
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, task, proxy)
	if err != nil {
		if managedResult {
			return fmt.Errorf("fetchTask failed for managed task %s: %T", taskId, err)
		}
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if managedResult {
			return fmt.Errorf("readAll failed for managed task %s: %T", taskId, err)
		}
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	if managedResult {
		logger.LogDebug(ctx, "updateVideoSingleTask managed response: status_code=%d response_bytes=%d content_type_bytes=%d", resp.StatusCode, len(responseBody), len(resp.Header.Get("Content-Type")))
	} else {
		logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("task status query returned HTTP %d for task %s", resp.StatusCode, taskId)
	}

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		if !managedResult {
			logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		}
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(task, resp, responseBody); err != nil {
		if managedResult {
			return fmt.Errorf("parseTaskResult failed for managed task %s: %T", taskId, err)
		}
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	if taskResult == nil || taskResult.Status == "" {
		if managedResult {
			logger.LogError(ctx, fmt.Sprintf("Managed task %s returned empty status, response_bytes=%d", taskId, len(responseBody)))
		} else {
			logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status, response: %s", taskId, string(responseBody)))
		}
		return fmt.Errorf("task status query returned empty status for task %s", taskId)
	}

	if len(taskResult.PluginState) > 0 {
		task.PrivateData.PluginState = append([]byte(nil), taskResult.PluginState...)
	}
	task.Data = redactVideoResponseBody(responseBody)

	if managedResult {
		logger.LogDebug(ctx, "updateVideoSingleTask managed result: status_bytes=%d progress_bytes=%d url_present=%t remote_url_present=%t", len(taskResult.Status), len(taskResult.Progress), taskResult.Url != "", taskResult.RemoteUrl != "")
	} else {
		logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)
	}

	now := time.Now().Unix()

	shouldRefund := false
	shouldSettle := false
	refundReason := ""
	quota := task.Quota

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
		shouldSettle = true
	case model.TaskStatusFailure:
		if !managedResult {
			logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		}
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		refundReason = taskResult.Reason
		task.FailReason = task.PublicFailureReason(refundReason)
		if managedResult {
			logger.LogInfo(ctx, fmt.Sprintf("Managed task %s failed: reason_bytes=%d", task.TaskID, len(task.FailReason)))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		}
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		if managedResult {
			return fmt.Errorf("unknown task status for managed task %s: status_bytes=%d", task.TaskID, len(taskResult.Status))
		}
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatusPreservingBilling(snap)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatusPreservingBilling(snap); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if shouldSettle {
		settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, refundReason)
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.TieredSnapshot != nil {
		if len(taskResult.UsageFacts) == 0 {
			return
		}
		result, facts, err := EvaluateTaskCompletionUsage(bc.TieredSnapshot, taskResult.UsageFacts)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("task completion usage settlement failed; retaining reserved quota: %v", err))
			return
		}
		bc.TieredSnapshot.UsageFacts = facts
		bc.TieredSnapshot.EstimatedTier = result.MatchedTier
		RecalculateTaskQuota(ctx, task, result.ActualQuotaAfterGroup, "task usage settlement", result.Clamp)
		return
	}
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
