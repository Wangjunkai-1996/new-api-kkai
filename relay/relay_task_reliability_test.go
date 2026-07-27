package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type reliabilityTaskAdaptor struct {
	taskcommon.BaseBilling
	doRequest  func(*gin.Context, *relaycommon.RelayInfo, io.Reader) (*http.Response, error)
	doResponse func(*gin.Context, *http.Response, *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError)
}

func (a *reliabilityTaskAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *reliabilityTaskAdaptor) ValidateRequestAndSetAction(_ *gin.Context, _ *relaycommon.RelayInfo) *dto.TaskError {
	return nil
}

func (a *reliabilityTaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return "https://example.invalid/tasks", nil
}

func (a *reliabilityTaskAdaptor) BuildRequestHeader(_ *gin.Context, _ *http.Request, _ *relaycommon.RelayInfo) error {
	return nil
}

func (a *reliabilityTaskAdaptor) BuildRequestBody(_ *gin.Context, _ *relaycommon.RelayInfo) (io.Reader, error) {
	return strings.NewReader(`{"prompt":"test"}`), nil
}

func (a *reliabilityTaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return a.doRequest(c, info, body)
}

func (a *reliabilityTaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
	return a.doResponse(c, resp, info)
}

func (a *reliabilityTaskAdaptor) GetModelList() []string { return nil }
func (a *reliabilityTaskAdaptor) GetChannelName() string { return "reliability-test" }

func (a *reliabilityTaskAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (a *reliabilityTaskAdaptor) ParseTaskResult(_ []byte) (*relaycommon.TaskInfo, error) {
	return nil, errors.New("not implemented")
}

func TestSubmitPreparedTaskPersistsBeforeUpstreamAndBuffersSuccess(t *testing.T) {
	setupTaskReliabilityDB(t)
	recorder, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{}
	adaptor.doRequest = func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
		var persisted model.Task
		require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
		assert.Equal(t, model.TaskStatusNotStart, persisted.Status)
		assert.Equal(t, taskcommon.ProgressComplete, persisted.Progress)
		assert.Empty(t, persisted.PrivateData.UpstreamTaskID)
		var accountingEvents int64
		require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
			Where("topic = ? AND aggregate_id = ?", service.KKAIOutboxTopicTaskAccounting, persisted.TaskID).
			Count(&accountingEvents).Error)
		assert.Zero(t, accountingEvents, "provisional task must not enqueue accounting before its outcome is known")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
		}, nil
	}
	adaptor.doResponse = func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
		clientBody := map[string]string{"id": info.PublicTaskID}
		response, err := channel.NewJSONTaskSubmitResponse("upstream-task", []byte(`{"id":"upstream-task"}`), clientBody)
		require.NoError(t, err)
		return response, nil
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Empty(t, recorder.Body.String(), "success must remain buffered until durable task update completes")

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), persisted.Status)
	assert.Equal(t, taskcommon.ProgressSubmitted, persisted.Progress)
	assert.Equal(t, "upstream-task", persisted.GetUpstreamTaskID())
	assert.JSONEq(t, `{"id":"upstream-task"}`, string(persisted.Data))
	var recoveryEvents int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
		Where("topic = ? AND aggregate_id = ?", service.KKAIOutboxTopicTaskBillingRecovery, persisted.TaskID).
		Count(&recoveryEvents).Error)
	assert.EqualValues(t, 1, recoveryEvents)
	var accountingEvents int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
		Where("topic = ? AND aggregate_id = ?", service.KKAIOutboxTopicTaskAccounting, persisted.TaskID).
		Count(&accountingEvents).Error)
	assert.EqualValues(t, 1, accountingEvents)

	require.NoError(t, result.Response.WriteTo(ctx))
	assert.JSONEq(t, `{"id":"task_public"}`, recorder.Body.String())
}

func TestSubmitPreparedTaskDoesNotCallUpstreamWhenProvisionalInsertFails(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_task_create", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.Task); ok {
			tx.AddError(errors.New("forced task insert failure"))
		}
	}))
	_, ctx, info := newTaskReliabilityContext(t)

	upstreamCalled := false
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalled = true
			return nil, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "persist_task_failed", taskErr.Code)
	assert.False(t, upstreamCalled)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry())
}

func TestSubmitPreparedTaskRollsBackProvisionalTaskWhenPersistHookFails(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	SetTaskProvisionalPersistHook(ctx, func(_ *gorm.DB, task *model.Task) error {
		assert.NotZero(t, task.ID)
		return errors.New("forced generation insert failure")
	})

	upstreamCalled := false
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalled = true
			return nil, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "persist_task_failed", taskErr.Code)
	assert.False(t, upstreamCalled)
	assert.Zero(t, result.Task.ID)

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&taskCount).Error)
	assert.Zero(t, taskCount)
	var outboxCount int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
}

func TestSubmitPreparedTaskKeepsDefinitiveApplicationRejectionRetryable(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":"rate_limited"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, channel.NewRejectedTaskResponseError(&dto.TaskError{
				Code:       "rate_limited",
				Message:    "try another channel",
				StatusCode: http.StatusTooManyRequests,
			})
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "rate_limited", taskErr.Code)
	assert.Equal(t, "try another channel", taskErr.Message)
	assert.Equal(t, TaskSubmitRejected, result.Outcome)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateReserved, persisted.PrivateData.BillingState)
	assert.EqualValues(t, model.TaskStatusNotStart, persisted.Status)
}

func TestSubmitPreparedTaskTreatsUncertainApplicationResponseAsUnknown(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"malformed":true}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, channel.NewUncertainTaskResponseError(&dto.TaskError{
				Code:       "invalid_response",
				Message:    "upstream acceptance cannot be determined",
				StatusCode: http.StatusBadGateway,
			})
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.Equal(t, TaskSubmitUnknown, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateAmbiguous, persisted.PrivateData.BillingState)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.True(t, persisted.PrivateData.AccountingRequired)
	assert.Equal(t, model.TaskAccountingStatePending, persisted.PrivateData.AccountingState)
	require.NotNil(t, persisted.PrivateData.TargetQuota)
	assert.Equal(t, info.PriceData.Quota, *persisted.PrivateData.TargetQuota)
}

func TestSubmitPreparedTaskUpgradesRejectedResponseToUnknownWhenClaimResetFails(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_rejected_response_claim_reset", func(tx *gorm.DB) {
		task, ok := tx.Statement.Dest.(*model.Task)
		if ok && task.PrivateData.BillingState == model.TaskBillingStateReserved && task.ID > 0 {
			tx.AddError(errors.New("forced rejected response claim reset failure"))
		}
	}))
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":"rejected"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, channel.NewRejectedTaskResponseError(&dto.TaskError{
				Code:       "rejected",
				Message:    "upstream rejected the request",
				StatusCode: http.StatusBadRequest,
			})
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.Equal(t, TaskSubmitUnknown, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateAmbiguous, persisted.PrivateData.BillingState)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
}

func TestSubmitPreparedTaskMarksTransportFailureUnknownWithoutRetryOrRefund(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return nil, channel.NewTaskRequestError(errors.New("connection reset"), true)
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())
	assert.Equal(t, info.PublicTaskID, taskErr.Data)

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusUnknown), persisted.Status)
	assert.Equal(t, taskcommon.ProgressComplete, persisted.Progress)
}

func TestSubmitPreparedTaskKeepsPreWriteTransportFailureRetryable(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return nil, channel.NewTaskRequestError(errors.New("dial failed before request write"), false)
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("pre-write failure must not reach the adaptor parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "do_request_failed", taskErr.Code)
	assert.Equal(t, TaskSubmitNotSent, result.Outcome)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskStatusNotStart, persisted.Status)
}

func TestSubmitPreparedTaskTreatsFailedPreWriteClaimResetAsUnknown(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_submission_claim_reset", func(tx *gorm.DB) {
		task, ok := tx.Statement.Dest.(*model.Task)
		if ok && task.PrivateData.BillingState == model.TaskBillingStateReserved && task.ID > 0 {
			tx.AddError(errors.New("forced submission claim reset failure"))
		}
	}))
	_, ctx, info := newTaskReliabilityContext(t)

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return nil, channel.NewTaskRequestError(errors.New("dial failed before request write"), false)
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("failed claim reset must not reach the upstream response parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.Equal(t, TaskSubmitUnknown, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateAmbiguous, persisted.PrivateData.BillingState)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
}

func TestSubmitPreparedTaskRecoversAcceptedTaskAfterTransientFinalPersistFailure(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	failSubmittedUpdate := true
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_first_submitted_update", func(tx *gorm.DB) {
		task, ok := tx.Statement.Dest.(*model.Task)
		if !ok || task.Status != model.TaskStatusSubmitted || !failSubmittedUpdate {
			return
		}
		failSubmittedUpdate = false
		tx.AddError(errors.New("forced accepted task update failure"))
	}))

	recorder, ctx, info := newTaskReliabilityContext(t)
	upstreamCalls := 0
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			response, err := channel.NewJSONTaskSubmitResponse(
				"upstream-task",
				[]byte(`{"id":"upstream-task"}`),
				map[string]string{"id": info.PublicTaskID},
			)
			require.NoError(t, err)
			return response, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, TaskSubmitAccepted, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())
	assert.Equal(t, 1, upstreamCalls)
	assert.Empty(t, recorder.Body.String(), "accepted response must remain buffered until persistence recovery completes")

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), persisted.Status)
	assert.Equal(t, taskcommon.ProgressSubmitted, persisted.Progress)
	assert.Zero(t, persisted.FinishTime)
	assert.Equal(t, "upstream-task", persisted.GetUpstreamTaskID())

	unfinished := model.GetAllUnFinishSyncTasks(10)
	require.Len(t, unfinished, 1)
	assert.Equal(t, persisted.ID, unfinished[0].ID)
}

func TestSubmitPreparedTaskSettlesAcceptedFreeTaskImmediately(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	info.PriceData.Quota = 0

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-free-task"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			response, err := channel.NewJSONTaskSubmitResponse(
				"upstream-free-task",
				[]byte(`{"id":"upstream-free-task"}`),
				map[string]string{"id": info.PublicTaskID},
			)
			require.NoError(t, err)
			return response, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, TaskSubmitAccepted, result.Outcome)

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateAccepted, persisted.PrivateData.BillingState)
	assert.Nil(t, persisted.PrivateData.TargetQuota)
	assert.Zero(t, persisted.Quota)
}

func TestSubmitPreparedTaskTreatsAmbiguousHTTPStatusAsUnknown(t *testing.T) {
	for _, statusCode := range []int{http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			setupTaskReliabilityDB(t)
			_, ctx, info := newTaskReliabilityContext(t)
			adaptor := &reliabilityTaskAdaptor{
				doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
					return &http.Response{
						StatusCode: statusCode,
						Body:       io.NopCloser(strings.NewReader(`{"error":"temporary"}`)),
					}, nil
				},
				doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
					t.Fatal("ambiguous HTTP response must not reach the adaptor parser")
					return nil, nil
				},
			}

			result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
			require.NotNil(t, taskErr)
			require.NotNil(t, result)
			assert.Equal(t, "task_submission_unknown", taskErr.Code)
			assert.Equal(t, TaskSubmitUnknown, result.Outcome)
			assert.False(t, result.CanRefund())
			assert.False(t, result.CanRetry())

			var persisted model.Task
			require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
			assert.Equal(t, model.TaskStatus(model.TaskStatusUnknown), persisted.Status)
			assert.Equal(t, taskcommon.ProgressComplete, persisted.Progress)
		})
	}
}

func TestSubmitPreparedTaskAcceptsSuccessfulTwoXXStatuses(t *testing.T) {
	for _, statusCode := range []int{http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			setupTaskReliabilityDB(t)
			_, ctx, info := newTaskReliabilityContext(t)
			adaptor := &reliabilityTaskAdaptor{
				doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
					return &http.Response{
						StatusCode: statusCode,
						Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
					}, nil
				},
				doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
					response, err := channel.NewJSONTaskSubmitResponse(
						"upstream-task",
						[]byte(`{"id":"upstream-task"}`),
						map[string]string{"id": info.PublicTaskID},
					)
					require.NoError(t, err)
					return response, nil
				},
			}

			result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			assert.Equal(t, TaskSubmitAccepted, result.Outcome)
			assert.False(t, result.CanRefund())
			assert.False(t, result.CanRetry())

			var persisted model.Task
			require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
			assert.EqualValues(t, model.TaskStatusSubmitted, persisted.Status)
			assert.Equal(t, "upstream-task", persisted.GetUpstreamTaskID())
		})
	}
}

func TestSubmitPreparedTaskRetriesAcceptedPersistenceWithoutResubmitting(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	failuresRemaining := 2
	callbackName := "test:fail_accepted_persistence_twice"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updatedTask, ok := tx.Statement.Dest.(*model.Task)
		if !ok || failuresRemaining == 0 || updatedTask.PrivateData.UpstreamTaskID != "upstream-task" {
			return
		}
		failuresRemaining--
		tx.AddError(errors.New("forced accepted persistence failure"))
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	upstreamCalls := 0
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			response, err := channel.NewJSONTaskSubmitResponse(
				"upstream-task",
				[]byte(`{"id":"upstream-task"}`),
				map[string]string{"id": info.PublicTaskID},
			)
			require.NoError(t, err)
			return response, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, TaskSubmitAccepted, result.Outcome)
	assert.Equal(t, 1, upstreamCalls)
	assert.Zero(t, failuresRemaining)

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, "upstream-task", persisted.PrivateData.UpstreamTaskID)
	assert.EqualValues(t, model.TaskStatusSubmitted, persisted.Status)
}

func TestSubmitPreparedTaskRecoversAcceptedReceiptAfterTaskPersistenceRetriesExhausted(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	recorder, ctx, info := newTaskReliabilityContext(t)

	persistenceAttempts := 0
	callbackName := "test:fail_all_accepted_persistence"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updatedTask, ok := tx.Statement.Dest.(*model.Task)
		if !ok || updatedTask.PrivateData.UpstreamTaskID != "upstream-task" {
			return
		}
		persistenceAttempts++
		tx.AddError(errors.New("forced persistent accepted task failure"))
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	upstreamCalls := 0
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			response, err := channel.NewJSONTaskSubmitResponse(
				"upstream-task",
				[]byte(`{"id":"upstream-task"}`),
				map[string]string{"id": info.PublicTaskID},
			)
			require.NoError(t, err)
			return response, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.Equal(t, TaskSubmitUnknown, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())
	assert.Equal(t, 1, upstreamCalls)
	assert.Equal(t, 5, persistenceAttempts, "initial acceptance write plus four bounded recovery attempts")
	assert.Empty(t, recorder.Body.String())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, model.TaskBillingStateAmbiguous, persisted.PrivateData.BillingState)
	assert.True(t, persisted.PrivateData.AccountingRequired)
	assert.Equal(t, model.TaskAccountingStatePending, persisted.PrivateData.AccountingState)
	assert.Empty(t, persisted.PrivateData.UpstreamTaskID)
	require.NotNil(t, persisted.PrivateData.TargetQuota)
	assert.Equal(t, info.PriceData.Quota, *persisted.PrivateData.TargetQuota)

	var recoveryEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", service.KKAIOutboxTopicTaskBillingRecovery).First(&recoveryEvent).Error)
	var recoveryPayload service.TaskBillingRecoveryPayload
	require.NoError(t, common.UnmarshalJsonStr(recoveryEvent.Payload, &recoveryPayload))
	require.NotNil(t, recoveryPayload.Acceptance)
	assert.Equal(t, "upstream-task", recoveryPayload.Acceptance.UpstreamTaskID)
	assert.JSONEq(t, `{"id":"upstream-task"}`, string(recoveryPayload.Acceptance.RawResponse))
	assert.Equal(t, info.ChannelId, recoveryPayload.Acceptance.ChannelID)
	assert.Equal(t, info.PriceData.Quota, recoveryPayload.Acceptance.TargetQuota)
	assert.LessOrEqual(t, recoveryEvent.AvailableAt, time.Now().Unix())

	require.NoError(t, db.Callback().Update().Remove(callbackName))
	processor := service.NewKKAIOutboxProcessor(db, "accepted-receipt-recovery")
	require.NoError(t, processor.Register(service.KKAIOutboxTopicTaskBillingRecovery, service.TaskBillingRecoveryHandler{}.Handle))
	batch, err := processor.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, batch)
	assert.Equal(t, 1, batch.Claimed)
	assert.Equal(t, 1, batch.Deferred)

	require.NoError(t, model.DB.First(&persisted, persisted.ID).Error)
	assert.Equal(t, "upstream-task", persisted.PrivateData.UpstreamTaskID)
	assert.EqualValues(t, model.TaskStatusSubmitted, persisted.Status)
	assert.Equal(t, info.ChannelId, persisted.ChannelId)
	assert.JSONEq(t, `{"id":"upstream-task"}`, string(persisted.Data))
}

func TestSubmitPreparedTaskKeepsUnknownResidualWhenTaskAndReceiptWritesFail(t *testing.T) {
	db := setupTaskReliabilityDB(t)
	recorder, ctx, info := newTaskReliabilityContext(t)

	taskPersistenceAttempts := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_task_and_receipt_persistence", func(tx *gorm.DB) {
		if task, ok := tx.Statement.Dest.(*model.Task); ok && task.PrivateData.UpstreamTaskID == "upstream-task" {
			taskPersistenceAttempts++
			tx.AddError(errors.New("forced persistent accepted task failure"))
			return
		}
		if tx.Statement.Table == "kkai_outbox" {
			tx.AddError(errors.New("forced accepted receipt persistence failure"))
		}
	}))

	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			response, err := channel.NewJSONTaskSubmitResponse(
				"upstream-task",
				[]byte(`{"id":"upstream-task"}`),
				map[string]string{"id": info.PublicTaskID},
			)
			require.NoError(t, err)
			return response, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.Equal(t, TaskSubmitUnknown, result.Outcome)
	assert.False(t, result.CanRefund())
	assert.False(t, result.CanRetry())
	assert.Equal(t, 5, taskPersistenceAttempts)
	assert.Empty(t, recorder.Body.String())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, model.TaskBillingStateAmbiguous, persisted.PrivateData.BillingState)
	assert.Empty(t, persisted.PrivateData.UpstreamTaskID)

	var recoveryEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", service.KKAIOutboxTopicTaskBillingRecovery).First(&recoveryEvent).Error)
	var recoveryPayload service.TaskBillingRecoveryPayload
	require.NoError(t, common.UnmarshalJsonStr(recoveryEvent.Payload, &recoveryPayload))
	assert.Nil(t, recoveryPayload.Acceptance)
}

func TestSubmitPreparedTaskKeepsDefinitiveBodylessTwoXXRejectionRetryable(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNoContent,
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			return nil, channel.NewRejectedTaskResponseError(&dto.TaskError{
				Code:       "invalid_empty_body",
				Message:    "empty response",
				StatusCode: http.StatusBadGateway,
			})
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "invalid_empty_body", taskErr.Code)
	assert.Equal(t, TaskSubmitRejected, result.Outcome)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry())

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.Equal(t, model.TaskBillingStateReserved, persisted.PrivateData.BillingState)
	assert.EqualValues(t, model.TaskStatusNotStart, persisted.Status)
}

func TestSubmitPreparedTaskKeepsExplicitHTTPRejectionRetryable(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate_limited"}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("explicit HTTP rejection must not reach the adaptor parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "fail_to_fetch_task", taskErr.Code)
	assert.Equal(t, TaskSubmitRejected, result.Outcome)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry())
}

func TestSubmitPreparedTaskDoesNotOverwriteDispatchingStateFromStaleReplay(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)

	task := buildProvisionalTask(ctx, constant.TaskPlatform("reliability"), info, nil)
	require.NoError(t, model.DB.Create(task).Error)
	staleReservedTask := *task

	claimedTask, claimed, err := model.ClaimTaskSubmission(ctx, task.ID, &model.TaskSubmissionAttempt{
		Platform:          constant.TaskPlatform("first-attempt"),
		ChannelID:         17,
		Action:            constant.TaskActionGenerate,
		OriginModelName:   "first-model",
		UpstreamModelName: "first-upstream-model",
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, model.TaskBillingStateDispatching, claimedTask.PrivateData.BillingState)

	info.ChannelMeta.ChannelId = 29
	info.OriginModelName = "stale-replay-model"
	info.UpstreamModelName = "stale-replay-upstream-model"
	upstreamCalled := false
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			upstreamCalled = true
			return nil, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("stale replay must not reach the upstream response parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("stale-replay"), &staleReservedTask)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "task_submission_unknown", taskErr.Code)
	assert.False(t, upstreamCalled)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskBillingStateDispatching, persisted.PrivateData.BillingState)
	assert.Equal(t, constant.TaskPlatform("first-attempt"), persisted.Platform)
	assert.Equal(t, 17, persisted.ChannelId)
	assert.Equal(t, "first-model", persisted.Properties.OriginModelName)
	assert.Equal(t, "first-upstream-model", persisted.Properties.UpstreamModelName)
}

func TestEnforceTaskMaxQuotaSupportsZeroCeiling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.Nil(t, enforceTaskMaxQuota(ctx, 1))
	SetTaskMaxQuota(ctx, 0)
	assert.Nil(t, enforceTaskMaxQuota(ctx, 0))

	taskErr := enforceTaskMaxQuota(ctx, 1)
	require.NotNil(t, taskErr)
	assert.Equal(t, "quote_stale", taskErr.Code)
	assert.Equal(t, http.StatusConflict, taskErr.StatusCode)
	assert.Equal(t, map[string]any{"current_quota": 1}, taskErr.Data)
}

func setupTaskReliabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.KKAIOutboxEvent{}, &model.Channel{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
	})
	return db
}

func newTaskReliabilityContext(t *testing.T) (*httptest.ResponseRecorder, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"prompt":"test"}`))

	info := &relaycommon.RelayInfo{
		UserId:          42,
		UsingGroup:      "default",
		OriginModelName: "test-video-model",
		PriceData: types.PriceData{
			FreeModel:  true,
			Quota:      123,
			ModelPrice: 1.23,
			ModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         9,
			ChannelType:       constant.ChannelTypeSora,
			UpstreamModelName: "test-video-model",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionGenerate,
			PublicTaskID: "task_public",
		},
	}
	return recorder, ctx, info
}
