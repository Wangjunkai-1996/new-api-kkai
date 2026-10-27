package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskFailureSnapshotKeepsBusinessReasonWithoutProviderSecrets(t *testing.T) {
	const realPerson = "The request failed because the input image 'content[1]' may contain real person."
	for _, test := range []struct {
		name, reason, payload, code, message string
	}{
		{"submission body", `{"error":{"code":"invalid_parameter","message":"` + realPerson + `"}}`, "", "privacy_restriction", realPerson},
		{"polling response", "video generation failed", `{"error":{"message":"` + realPerson + `"},"request":{"api_key":"private"}}`, "privacy_restriction", realPerson},
		{"plain provider reason", realPerson, "", "privacy_restriction", realPerson},
		{"nested gateway error", `{"error":{"message":"{\"error\":{\"message\":\"UnsupportedImageFormat\"}}"}}`, "", "unsupported_media", "UnsupportedImageFormat"},
		{"unknown structured business reason", `{"error":{"code":"vendor_123","message":"The supplied scene cannot be rendered."}}`, "", "generation_failure", "The supplied scene cannot be rendered."},
		{"unknown internal error", "db transaction failed private-token", "", "generation_failure", ""},
		{"transport details", "dial tcp 127.0.0.1:5432: connection refused", "", "generation_failure", ""},
		{"old placeholder", "task submission failed", "", "submission_failure", ""},
		{"urls", "invalid parameter image: https://private.internal/image?Signature=secret", "", "input_invalid", "invalid parameter image: [redacted]"},
		{"bearer and cookies", "invalid parameter. Authorization: Bearer secret-token Cookie: account=secret; session=private", "", "input_invalid", "invalid parameter. [redacted]"},
		{"quoted credentials", `invalid parameter. "api_key": "private-value"`, "", "input_invalid", `invalid parameter. "[redacted]`},
		{"client secret", "invalid parameter. client_secret=private-value", "", "input_invalid", "invalid parameter. [redacted]"},
		{"secret key", "invalid parameter. secret_key: private-value", "", "input_invalid", "invalid parameter. [redacted]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := NewTaskFailureSnapshot("submission", "", test.reason, []byte(test.payload))
			assert.Equal(t, test.code, failure.Code)
			assert.Equal(t, test.message, failure.Message)
			encoded, err := common.Marshal(failure)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "secret")
			assert.NotContains(t, string(encoded), "private")
		})
	}
}

func TestTaskFailurePayloadWithoutCodeKeepsKnownUpstreamCode(t *testing.T) {
	failure := NewTaskFailureSnapshot("submission", "invalid_parameter", "upstream returned error",
		[]byte(`{"error":{"message":"The request could not be processed."}}`))
	assert.Equal(t, "input_invalid", failure.Code)
	assert.Equal(t, "The request could not be processed.", failure.Message)
}

func TestTaskFailureProjectionRequiresTerminalFailure(t *testing.T) {
	task := Task{Status: TaskStatusFailure, FailReason: "task submission failed"}
	assert.Equal(t, "historical_detail_unavailable", task.FailureForDisplay().Code)
	task.PrivateData.Failure = NewTaskFailureSnapshot("submission", "", "may contain real person", nil)
	for _, status := range []TaskStatus{TaskStatusSuccess, TaskStatusUnknown, TaskStatusInProgress} {
		task.Status = status
		assert.Nil(t, task.FailureForDisplay())
	}
	task.Status = TaskStatusFailure
	task.PrivateData.AssetHostedResult = true
	assert.Equal(t, "privacy_restriction", task.FailureForDisplay().Code)
	assert.Equal(t, "may contain real person", task.FailureForDisplay().Message)
	assert.Equal(t, AssetHostedTaskPublicFailureReason, task.PublicFailReason())
}

func TestRecordTaskFailurePreservesRefundAndDoesNotRewriteFinalReason(t *testing.T) {
	truncateTables(t)
	task := Task{
		TaskID: "failure-diagnostics", Status: TaskStatusFailure, FailReason: "task submission failed",
		PrivateData: TaskPrivateData{
			BillingState: TaskBillingStateRefunded, BillingRevision: 3,
			Execution: &TaskExecutionSnapshot{RequestID: "request-proof"},
		},
	}
	insertTask(t, &task)
	require.NoError(t, RecordTaskFailure(context.Background(), task.ID, NewTaskFailureSnapshot("submission", "", "may contain real person", nil)))
	require.NoError(t, RecordTaskFailure(context.Background(), task.ID, NewTaskFailureSnapshot("submission", "", "copyright restrictions", nil)))
	var persisted Task
	require.NoError(t, DB.First(&persisted, task.ID).Error)
	assert.Equal(t, "privacy_restriction", persisted.PrivateData.Failure.Code)
	assert.Equal(t, "request-proof", persisted.PrivateData.Failure.RequestID)
	assert.Equal(t, "task submission failed", persisted.FailReason)
	assert.Equal(t, TaskBillingStateRefunded, persisted.PrivateData.BillingState)
	assert.EqualValues(t, 3, persisted.PrivateData.BillingRevision)
	assert.Zero(t, persisted.Quota)
	var count int64
	require.NoError(t, DB.Model(&Log{}).Where("request_id = ?", "request-proof").Count(&count).Error)
	assert.Zero(t, count)
	for _, status := range []TaskStatus{TaskStatusSuccess, TaskStatusUnknown} {
		other := Task{TaskID: "non-failure-" + string(status), Status: status}
		insertTask(t, &other)
		require.NoError(t, RecordTaskFailure(context.Background(), other.ID, NewTaskFailureSnapshot("submission", "", "may contain real person", nil)))
		require.NoError(t, DB.First(&other, other.ID).Error)
		assert.Nil(t, other.PrivateData.Failure)
	}
}

func TestImmediateTaskFailureCapturesSafeReason(t *testing.T) {
	task := Task{Quota: 123, PrivateData: TaskPrivateData{Execution: &TaskExecutionSnapshot{RequestID: "immediate-request"}}}
	applyTaskSubmissionAcceptance(&task, TaskSubmissionAcceptance{
		Status: TaskStatusFailure, FailReason: "may contain real person", TargetQuota: 0,
		TaskData: []byte(`{"error":{"message":"The input image may contain real person."}}`),
	})
	require.NotNil(t, task.PrivateData.Failure)
	assert.Equal(t, "privacy_restriction", task.PrivateData.Failure.Code)
	assert.Equal(t, "The input image may contain real person.", task.PrivateData.Failure.Message)
	assert.Equal(t, "immediate-request", task.PrivateData.Failure.RequestID)
	assert.Equal(t, "generation", task.PrivateData.Failure.Stage)
	assert.Equal(t, 123, task.Quota)
}
