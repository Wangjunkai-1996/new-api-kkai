package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type TaskBillingAuditHandler struct{}

func (TaskBillingAuditHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := model.TaskBillingAuditPayload{}
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid task billing audit payload: %w", err))
	}
	if err := validateTaskBillingAuditPayload(payload); err != nil {
		return PermanentKKAIOutboxError(err)
	}

	var task model.Task
	if err := model.DB.WithContext(ctx).First(&task, payload.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PermanentKKAIOutboxError(err)
		}
		return err
	}
	if payload.LogType == model.LogTypeConsume && !common.LogConsumeEnabled {
		return nil
	}

	other, content := taskBillingAuditLogDetails(&task, payload)
	username, _ := model.GetUsernameById(task.UserId, false)
	tokenName := ""
	if task.PrivateData.TokenId > 0 {
		if token, err := model.GetTokenById(task.PrivateData.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &model.Log{
		Username:  username,
		TokenName: tokenName,
		Type:      payload.LogType,
		Content:   content,
		ModelName: taskModelName(&task),
		Quota:     payload.Quota,
		Other:     common.MapToJsonStr(other),
	}
	created, err := model.CompleteTaskBillingAuditLog(ctx, payload.TaskID, payload.BillingRevision, log)
	if err != nil {
		return err
	}
	if created && payload.LogType == model.LogTypeConsume && common.DataExportEnabled {
		nodeName := task.PrivateData.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		model.LogQuotaData(model.QuotaDataLogParams{
			UserID:    task.UserId,
			Username:  username,
			ModelName: taskModelName(&task),
			Quota:     payload.Quota,
			CreatedAt: log.CreatedAt,
			UseGroup:  task.Group,
			TokenID:   task.PrivateData.TokenId,
			ChannelID: task.ChannelId,
			NodeName:  nodeName,
		})
	}
	return nil
}

func validateTaskBillingAuditPayload(payload model.TaskBillingAuditPayload) error {
	if payload.TaskID <= 0 || payload.BillingRevision <= 0 || payload.Quota <= 0 {
		return errors.New("invalid task billing audit payload")
	}
	switch payload.Operation {
	case model.TaskBillingAuditOperationRefund:
		if payload.LogType != model.LogTypeRefund {
			return errors.New("invalid task billing refund audit type")
		}
	case model.TaskBillingAuditOperationSettlement:
		if payload.LogType != model.LogTypeConsume && payload.LogType != model.LogTypeRefund {
			return errors.New("invalid task billing settlement audit type")
		}
	default:
		return errors.New("invalid task billing audit operation")
	}
	return nil
}

func taskBillingAuditLogDetails(task *model.Task, payload model.TaskBillingAuditPayload) (map[string]interface{}, string) {
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	if payload.Operation == model.TaskBillingAuditOperationRefund {
		other["reason"] = task.PublicFailureReason(payload.Reason)
		if task.IsAssetHostedResult() && payload.Reason != "" {
			adminInfo, _ := other["admin_info"].(map[string]interface{})
			if adminInfo == nil {
				adminInfo = make(map[string]interface{})
				other["admin_info"] = adminInfo
			}
			adminInfo["provider_failure_reason"] = payload.Reason
		}
		return other, ""
	}
	other["pre_consumed_quota"] = payload.PreviousQuota
	other["actual_quota"] = payload.CurrentQuota
	for _, clamp := range payload.QuotaClamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	return other, payload.Reason
}
