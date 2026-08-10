package relay

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubmitPreparedTaskTreatsAuditUnavailableAsDefinitivePolicyRejection(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"policy_audit_unavailable"}}`)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("policy rejection must not reach the adaptor parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, string(types.ErrorCodePolicyAuditUnavailable), taskErr.Code)
	assert.Equal(t, http.StatusServiceUnavailable, taskErr.UpstreamStatusCode)
	assert.Equal(t, TaskSubmitRejected, result.Outcome)
	assert.True(t, result.CanRefund())
	assert.True(t, result.CanRetry(), "controller-level policy code blocks failover after a definitive rejection")

	var persisted model.Task
	require.NoError(t, model.DB.Where("task_id = ?", info.PublicTaskID).First(&persisted).Error)
	assert.EqualValues(t, model.TaskStatusNotStart, persisted.Status)
	assert.Equal(t, model.TaskBillingStateReserved, persisted.PrivateData.BillingState)
}

func TestSubmitPreparedTaskPreservesUnauthorizedUpstreamKeyPolicy(t *testing.T) {
	setupTaskReliabilityDB(t)
	_, ctx, info := newTaskReliabilityContext(t)
	adaptor := &reliabilityTaskAdaptor{
		doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"code":"invalid_api_key","message":"API key has been permanently disabled"}}`,
				)),
			}, nil
		},
		doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
			t.Fatal("policy rejection must not reach the adaptor parser")
			return nil, nil
		},
	}

	result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
	require.NotNil(t, taskErr)
	require.NotNil(t, result)
	classification := service.ClassifyKKAITaskPolicyError(taskErr)
	require.True(t, classification.Detected)
	require.Equal(t, service.KKAIPolicyCausalityUpstreamKey, classification.Causality)
	require.Equal(t, http.StatusUnauthorized, taskErr.UpstreamStatusCode)
	require.Equal(t, TaskSubmitRejected, result.Outcome)
}

func TestSubmitPreparedTaskRejectsEmbedded2xxPolicyErrorsBeforeAdaptor(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		wantCode      string
		wantStatus    int
		wantCausality string
	}{
		{
			name:       "local audit unavailable",
			code:       string(types.ErrorCodePolicyAuditUnavailable),
			wantCode:   string(types.ErrorCodePolicyAuditUnavailable),
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:          "confirmed cyber",
			code:          "cyber_policy",
			wantCode:      "fail_to_fetch_task",
			wantStatus:    http.StatusForbidden,
			wantCausality: service.KKAIPolicyCausalityClientToken,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTaskReliabilityDB(t)
			_, ctx, info := newTaskReliabilityContext(t)
			adaptor := &reliabilityTaskAdaptor{
				doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(
							`{"error":{"code":"` + test.code + `","message":"request rejected"}}`,
						)),
					}, nil
				},
				doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
					t.Fatal("embedded policy rejection must not reach the adaptor parser")
					return nil, nil
				},
			}

			result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)

			require.NotNil(t, taskErr)
			require.NotNil(t, result)
			require.Equal(t, test.wantCode, taskErr.Code)
			require.Equal(t, test.wantStatus, taskErr.StatusCode)
			require.Equal(t, http.StatusOK, taskErr.UpstreamStatusCode)
			require.Equal(t, TaskSubmitRejected, result.Outcome)
			classification := service.ClassifyKKAITaskPolicyError(taskErr)
			require.Equal(t, test.wantCausality != "", classification.Detected)
			require.Equal(t, test.wantCausality, classification.Causality)
		})
	}
}

func TestSubmitPreparedTaskPreservesPolicyErrorsWhenStateRecoveryFails(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		status          int
		wantCode        string
		wantCausality   string
		failUnknownSave bool
	}{
		{
			name:            "local policy reset and unknown save fail",
			body:            `{"error":{"code":"policy_audit_unavailable","message":"audit unavailable"}}`,
			status:          http.StatusServiceUnavailable,
			wantCode:        string(types.ErrorCodePolicyAuditUnavailable),
			failUnknownSave: true,
		},
		{
			name:          "confirmed cyber reset fails",
			body:          `{"error":{"code":"cyber_policy","message":"request rejected"}}`,
			status:        http.StatusForbidden,
			wantCode:      "fail_to_fetch_task",
			wantCausality: service.KKAIPolicyCausalityClientToken,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupTaskReliabilityDB(t)
			callbackName := "test:fail_policy_submission_state_recovery"
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
				updatedTask, ok := tx.Statement.Dest.(*model.Task)
				if !ok || updatedTask.ID <= 0 {
					return
				}
				if updatedTask.PrivateData.BillingState == model.TaskBillingStateReserved ||
					test.failUnknownSave && updatedTask.PrivateData.BillingState == model.TaskBillingStateAmbiguous {
					tx.AddError(errors.New("forced policy state recovery failure"))
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })
			_, ctx, info := newTaskReliabilityContext(t)
			adaptor := &reliabilityTaskAdaptor{
				doRequest: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
					return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
				},
				doResponse: func(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *channel.TaskResponseError) {
					t.Fatal("policy rejection must not reach the adaptor parser")
					return nil, nil
				},
			}

			result, taskErr := submitPreparedTask(ctx, info, adaptor, constant.TaskPlatform("reliability"), nil)
			require.NotNil(t, taskErr)
			require.NotNil(t, result)
			require.Equal(t, test.wantCode, taskErr.Code)
			require.Equal(t, TaskSubmitUnknown, result.Outcome)
			if test.wantCausality == "" {
				require.False(t, service.ClassifyKKAITaskPolicyError(taskErr).Detected)
			} else {
				classification := service.ClassifyKKAITaskPolicyError(taskErr)
				require.True(t, classification.Detected)
				require.Equal(t, test.wantCausality, classification.Causality)
			}
		})
	}
}
