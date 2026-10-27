package service

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLoadTaskFailuresUsesOnlyUniquelyProvenFinalAttempt(t *testing.T) {
	for _, test := range []struct {
		name      string
		change    func(*model.Task, *[]model.Log)
		recovered bool
	}{
		{name: "exact terminal error", recovered: true},
		{name: "retry followed by unique final stop", recovered: true, change: func(_ *model.Task, logs *[]model.Log) {
			earlier := (*logs)[0]
			earlier.Other = `{"request_path":"/v1/videos","admin_info":{"request_policy":[{"attempt":1,"channel_id":20,"status":400,"decision":{"action":"retry"}}]}}`
			earlier.Content = `{"error":{"message":"copyright restrictions"}}`
			*logs = append(*logs, earlier)
		}},
		{name: "ambiguous terminal stops", change: func(_ *model.Task, logs *[]model.Log) { *logs = append(*logs, (*logs)[0]) }},
		{name: "missing final retry decision", change: func(_ *model.Task, logs *[]model.Log) {
			(*logs)[0].Other = `{"request_path":"/v1/videos"}`
		}},
		{name: "earlier retry is not final cause", change: func(_ *model.Task, logs *[]model.Log) {
			(*logs)[0].Other = `{"request_path":"/v1/videos","admin_info":{"request_policy":[{"attempt":1,"channel_id":20,"status":400,"decision":{"action":"retry"}}]}}`
		}},
		{name: "another user", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].UserId++ }},
		{name: "another tenant", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].TenantID++ }},
		{name: "another final channel", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].ChannelId++ }},
		{name: "another model", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].ModelName = "other-model" }},
		{name: "another request", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].RequestId = "other-request" }},
		{name: "another path", change: func(task *model.Task, _ *[]model.Log) { task.PrivateData.Execution.RequestPath = "/other" }},
		{name: "outside terminal time", change: func(_ *model.Task, logs *[]model.Log) { (*logs)[0].CreatedAt = 301 }},
		{name: "accepted upstream task", change: func(task *model.Task, _ *[]model.Log) { task.PrivateData.UpstreamTaskID = "accepted-task" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			task := &model.Task{
				ID: 42, UserId: 10, TenantID: 12, ChannelId: 20, Status: model.TaskStatusFailure,
				FailReason: "task submission failed", SubmitTime: 100, FinishTime: 300,
				Properties:  model.Properties{OriginModelName: "video-model"},
				PrivateData: model.TaskPrivateData{Execution: &model.TaskExecutionSnapshot{RequestID: "exact-request", RequestPath: "/v1/videos"}},
			}
			logs := []model.Log{{
				UserId: 10, TenantID: 12, ChannelId: 20, ModelName: "video-model", RequestId: "exact-request",
				Type: model.LogTypeError, CreatedAt: 200,
				Content: `status_code=400, {"error":{"message":"The input image 'content[1]' may contain real person."}}`,
				Other:   `{"request_path":"/v1/videos","admin_info":{"request_policy":[{"attempt":2,"channel_id":20,"status":400,"decision":{"action":"stop"}}]}}`,
			}}
			if test.change != nil {
				test.change(task, &logs)
			}
			for i := range logs {
				require.NoError(t, model.LOG_DB.Session(&gorm.Session{SkipHooks: true}).Create(&logs[i]).Error)
			}
			require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).Create(task).Error)
			// The real video list omits data and the self-view channel ID.
			visible := *task
			visible.ChannelId = 0
			visible.Data = nil
			before, err := common.Marshal(visible)
			require.NoError(t, err)
			failures, err := LoadTaskFailures(context.Background(), []*model.Task{&visible})
			require.NoError(t, err)
			require.NotNil(t, failures[task.ID])
			if test.recovered {
				assert.Equal(t, "privacy_restriction", failures[task.ID].Code)
				assert.Equal(t, "The input image 'content[1]' may contain real person.", failures[task.ID].Message)
			} else {
				assert.Equal(t, "historical_detail_unavailable", failures[task.ID].Code)
				assert.Empty(t, failures[task.ID].Message)
			}
			after, err := common.Marshal(visible)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "read recovery must not rewrite the task or its billing")
			assert.Nil(t, visible.PrivateData.Failure)
		})
	}
}

func TestLoadTaskFailuresReadsOnlyFailedPageTaskData(t *testing.T) {
	truncate(t)
	stored := &model.Task{
		TaskID: "historical-polling", UserId: 10, TenantID: 12, ChannelId: 20,
		Status: model.TaskStatusFailure, FailReason: "video generation failed",
		Data: []byte(`{"error":{"code":"video_generation_failed","message":"The input image 'content[1]' may contain real person."},"request":{"api_key":"private-key"}}`),
	}
	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).Create(stored).Error)
	visible := *stored
	visible.Data = nil
	visible.ChannelId = 0
	result, err := LoadTaskFailures(context.Background(), []*model.Task{&visible})
	require.NoError(t, err)
	assert.Equal(t, "privacy_restriction", result[stored.ID].Code)
	assert.Equal(t, "The input image 'content[1]' may contain real person.", result[stored.ID].Message)
	assert.Zero(t, visible.ChannelId)
	assert.Nil(t, visible.Data)
	encoded, err := common.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-key")

	// Never reuse a fetched row when the visible ownership or tenancy differs.
	visible.TenantID++
	result, err = LoadTaskFailures(context.Background(), []*model.Task{&visible})
	require.NoError(t, err)
	assert.NotEqual(t, "privacy_restriction", result[stored.ID].Code)
}

func TestLoadTaskFailuresSkipsOversizedLegacyResponse(t *testing.T) {
	truncate(t)
	stored := &model.Task{
		TaskID: "historical-oversized", UserId: 10, TenantID: 12,
		Status: model.TaskStatusFailure, FailReason: "task submission failed",
		Data: []byte(`{"error":{"message":"` + strings.Repeat("x", 64*1024) + `"}}`),
	}
	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).Create(stored).Error)
	visible := *stored
	visible.Data = nil
	result, err := LoadTaskFailures(context.Background(), []*model.Task{&visible})
	require.NoError(t, err)
	assert.Equal(t, "historical_detail_unavailable", result[stored.ID].Code)
	assert.Nil(t, visible.Data)
}
