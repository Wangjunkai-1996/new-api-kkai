package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimTaskSubmissionPersistsRetryGroupOnlyForWinningClaim(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:      "task_retry_group",
		Group:       "group-a",
		ChannelId:   1,
		PrivateData: TaskPrivateData{BillingState: TaskBillingStateDispatching},
	}
	insertTask(t, task)
	ctx := context.Background()
	require.NoError(t, ResetTaskSubmissionClaim(ctx, task.ID))

	claimedTask, claimed, err := ClaimTaskSubmission(ctx, task.ID, &TaskSubmissionAttempt{ChannelID: 2, Group: "group-b"})
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "group-b", claimedTask.Group)
	assert.Equal(t, 2, claimedTask.ChannelId)
	require.NoError(t, DB.First(task, task.ID).Error)
	assert.Equal(t, "group-b", task.Group)
	assert.Equal(t, 2, task.ChannelId)
	assert.Equal(t, TaskBillingStateDispatching, task.PrivateData.BillingState)

	claimedTask, claimed, err = ClaimTaskSubmission(ctx, task.ID, &TaskSubmissionAttempt{ChannelID: 3, Group: "group-c"})
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Equal(t, "group-b", claimedTask.Group)
	require.NoError(t, DB.First(task, task.ID).Error)
	assert.Equal(t, "group-b", task.Group)
	assert.Equal(t, 2, task.ChannelId)
}

func TestClaimTaskSubmissionPreservesGroupWhenAttemptOmitsIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		attempt *TaskSubmissionAttempt
	}{
		{name: "nil_attempt"},
		{name: "empty_group", attempt: &TaskSubmissionAttempt{ChannelID: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			task := &Task{
				TaskID:      "task_existing_group",
				Group:       "group-a",
				PrivateData: TaskPrivateData{BillingState: TaskBillingStateReserved},
			}
			insertTask(t, task)

			claimedTask, claimed, err := ClaimTaskSubmission(context.Background(), task.ID, tc.attempt)
			require.NoError(t, err)
			require.True(t, claimed)
			assert.Equal(t, "group-a", claimedTask.Group)
			require.NoError(t, DB.First(task, task.ID).Error)
			assert.Equal(t, "group-a", task.Group)
		})
	}
}
