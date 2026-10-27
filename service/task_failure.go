package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// LoadTaskFailures enriches only already-authorized tasks. It never writes
// historical tasks or bills, and performs at most one bounded log query.
func LoadTaskFailures(ctx context.Context, tasks []*model.Task) (map[int64]*dto.TaskFailure, error) {
	result := make(map[int64]*dto.TaskFailure, len(tasks))
	requests := make(map[string]*model.Task)
	needsData := make([]int64, 0, len(tasks))
	visible := make(map[int64]*model.Task, len(tasks))
	for _, task := range tasks {
		if task == nil || task.ID <= 0 {
			continue
		}
		visible[task.ID] = task
		if task.Status == model.TaskStatusFailure && task.PrivateData.Failure == nil && len(task.Data) == 0 {
			needsData = append(needsData, task.ID)
		}
	}
	// ListVideoWorks deliberately omits both raw task data and self-view
	// channel IDs. Read only failed rows on this authorized page in one batch;
	// neither raw data nor hidden channel IDs leave this function.
	internal := make(map[int64]*model.Task, len(needsData))
	if len(needsData) > 0 {
		var rows []model.Task
		// The legacy response is only useful within the parser's 64 KiB bound.
		// PostgreSQL JSON needs an explicit text cast for a portable size check.
		dataSize := "LENGTH(data) <= ?"
		if model.DB.Dialector.Name() == "postgres" {
			dataSize = "OCTET_LENGTH(data::text) <= ?"
		}
		err := model.DB.WithContext(ctx).Select("id", "tenant_id", "user_id", "channel_id", "properties", "data").
			Where("id IN ? AND status = ?", needsData, model.TaskStatusFailure).
			Where("data IS NULL OR "+dataSize, 64*1024).Find(&rows).Error
		if err != nil {
			return result, err
		}
		for i := range rows {
			row := &rows[i]
			original := visible[row.ID]
			if original == nil || original.UserId != row.UserId || original.TenantID != row.TenantID {
				continue
			}
			copy := *original
			copy.ChannelId = row.ChannelId
			copy.Data = row.Data
			copy.Properties = row.Properties
			internal[row.ID] = &copy
		}
	}
	for _, original := range tasks {
		if original == nil || original.ID <= 0 {
			continue
		}
		task := original
		if enriched := internal[task.ID]; enriched != nil {
			task = enriched
		}
		failure := task.FailureForDisplay()
		if failure == nil {
			continue
		}
		result[task.ID] = failure
		execution := task.PrivateData.Execution
		if task.PrivateData.Failure != nil || failure.Code != "historical_detail_unavailable" ||
			task.FailReason != "task submission failed" || task.PrivateData.UpstreamTaskID != "" ||
			execution == nil || execution.RequestID == "" || execution.RequestPath == "" ||
			task.UserId <= 0 || task.ChannelId <= 0 || task.Properties.OriginModelName == "" {
			continue
		}
		if _, exists := requests[execution.RequestID]; exists {
			// One request must identify one task, even within this page.
			requests[execution.RequestID] = nil
		} else {
			requests[execution.RequestID] = task
		}
	}
	ids := make([]string, 0, len(requests))
	for id, task := range requests {
		if task != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || model.LOG_DB == nil {
		return result, nil
	}
	// A pathological retry history must degrade rather than make a list request
	// read an unbounded amount of diagnostics or infer from a truncated history.
	limit := len(ids) * 32
	var logs []model.Log
	err := model.LOG_DB.WithContext(ctx).
		Select("id", "tenant_id", "user_id", "channel_id", "model_name", "request_id", "created_at", "content", "other").
		Where("request_id IN ? AND type = ?", ids, model.LogTypeError).
		Limit(limit + 1).Find(&logs).Error
	if err != nil {
		return result, err
	}
	if len(logs) > limit {
		return result, errors.New("task failure history exceeds bounded lookup")
	}
	byRequest := make(map[string][]model.Log, len(ids))
	for _, log := range logs {
		byRequest[log.RequestId] = append(byRequest[log.RequestId], log)
	}
	for id, task := range requests {
		if task == nil {
			continue
		}
		var matched *model.TaskFailureSnapshot
		ambiguous := false
		for _, log := range byRequest[id] {
			if log.UserId != task.UserId || log.TenantID != task.TenantID ||
				log.ChannelId != task.ChannelId || log.ModelName != task.Properties.OriginModelName ||
				log.CreatedAt < task.SubmitTime || task.FinishTime == 0 || log.CreatedAt > task.FinishTime {
				continue
			}
			var metadata struct {
				RequestPath string `json:"request_path"`
				AdminInfo   struct {
					RequestPolicy []PolicyEvent `json:"request_policy"`
				} `json:"admin_info"`
			}
			if common.UnmarshalJsonStr(log.Other, &metadata) != nil || metadata.RequestPath != task.PrivateData.Execution.RequestPath {
				continue
			}
			// A terminal stop recorded for the same final channel proves this was
			// not an earlier retry. Never choose the latest log by timestamp alone.
			events := metadata.AdminInfo.RequestPolicy
			if len(events) == 0 {
				continue
			}
			last := events[len(events)-1]
			if last.Attempt < 1 || last.ChannelID != task.ChannelId || last.Decision.Action != "stop" || last.Status == 0 {
				continue
			}
			if matched != nil {
				ambiguous = true
				break
			}
			matched = model.NewTaskFailureSnapshot("submission", "", log.Content, nil)
		}
		if !ambiguous && matched != nil && (matched.Message != "" || matched.Code != "generation_failure") {
			result[task.ID] = &dto.TaskFailure{Code: matched.Code, Message: matched.Message, Stage: matched.Stage}
		}
	}
	return result, nil
}
