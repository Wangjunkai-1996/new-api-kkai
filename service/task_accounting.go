package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	KKAIOutboxTopicTaskAccounting = model.KKAIOutboxTopicTaskAccounting
	taskAccountingRetryInterval   = 5 * time.Second
)

type TaskAccountingPayload = model.TaskAccountingPayload

type TaskAccountingHandler struct{}

func (TaskAccountingHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := TaskAccountingPayload{}
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid task accounting payload: %w", err))
	}
	if payload.TaskID <= 0 {
		return PermanentKKAIOutboxError(errors.New("invalid task accounting payload: task_id must be positive"))
	}

	mutation, err := model.RecordTaskAccountingStatistics(ctx, payload.TaskID)
	if errors.Is(err, model.ErrTaskAccountingNotReady) {
		return DeferKKAIOutboxUntil(time.Now().Add(taskAccountingRetryInterval), err)
	}
	if err != nil {
		return err
	}
	if mutation == nil || mutation.Task == nil || mutation.Skipped ||
		mutation.Task.PrivateData.AccountingState == "" ||
		mutation.Task.PrivateData.AccountingState == model.TaskAccountingStateCompleted {
		return nil
	}

	task := mutation.Task
	accountingContext := task.PrivateData.AccountingContext
	if accountingContext == nil {
		accountingContext = &model.TaskAccountingContext{}
	}
	other := taskBillingOther(task)
	other["is_task"] = true
	if accountingContext.RequestPath != "" {
		other["request_path"] = accountingContext.RequestPath
	}
	if accountingContext.HasUserGroupRatio {
		other["user_group_ratio"] = accountingContext.UserGroupRatio
	}
	attachQuotaSaturationToOther(other, accountingContext.QuotaClamp)

	logContent := fmt.Sprintf("操作 %s", task.Action)
	billingContext := task.PrivateData.BillingContext
	if billingContext != nil && billingContext.PerCallBilling {
		logContent += "，按次计费"
	} else if billingContext != nil {
		priceData := taskBillingContextPriceData(billingContext)
		if priceData != nil {
			ratios := priceData.OtherRatios()
			keys := make([]string, 0, len(ratios))
			for key, ratio := range ratios {
				if ratio != 1 {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			contents := make([]string, 0, len(keys))
			for _, key := range keys {
				contents = append(contents, fmt.Sprintf("%s: %.2f", key, ratios[key]))
			}
			if len(contents) > 0 {
				logContent += ", 计算参数：" + strings.Join(contents, ", ")
			}
		}
	}

	log := &model.Log{
		Username:  accountingContext.Username,
		TokenName: accountingContext.TokenName,
		ModelName: taskModelName(task),
		Content:   logContent,
		Other:     common.MapToJsonStr(other),
	}
	created, err := model.CompleteTaskAccountingLog(ctx, task.ID, log)
	if err != nil {
		return err
	}
	if created && common.DataExportEnabled {
		nodeName := task.PrivateData.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		model.LogQuotaData(model.QuotaDataLogParams{
			UserID:    task.UserId,
			Username:  accountingContext.Username,
			ModelName: taskModelName(task),
			Quota:     task.PrivateData.AccountingQuota,
			CreatedAt: log.CreatedAt,
			UseGroup:  task.Group,
			TokenID:   task.PrivateData.TokenId,
			ChannelID: task.ChannelId,
			NodeName:  nodeName,
		})
	}
	return nil
}
